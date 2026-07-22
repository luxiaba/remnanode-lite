#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 6 ]; then
  echo "usage: $0 <release-id> <tag> <commit> <true|false-prerelease> <true|false-latest> <asset-directory>" >&2
  exit 2
fi

release_id=$1
release_tag=$2
source_commit=$3
prerelease=$4
make_latest=$5
asset_directory=$6
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${IMAGE:?IMAGE is required}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"
: "${SOURCE_DIGEST:?SOURCE_DIGEST is required}"
: "${VERSION:?VERSION is required}"

[[ "$SOURCE_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || {
  echo "invalid source image digest: $SOURCE_DIGEST" >&2
  exit 2
}
[ "$release_tag" = "v$VERSION" ] || {
  echo "release tag $release_tag does not match version $VERSION" >&2
  exit 2
}

for value in "$prerelease" "$make_latest"; do
  case "$value" in
    true|false) ;;
    *) echo "expected boolean value, got $value" >&2; exit 2 ;;
  esac
done
[[ "$release_id" =~ ^[1-9][0-9]*$ ]] || {
  echo "invalid GitHub Release ID: $release_id" >&2
  exit 2
}

readonly max_attempts=12
readonly retry_delay_seconds=5
identity="https://github.com/${GITHUB_REPOSITORY}/.github/workflows/container.yml@refs/heads/main"
last_error="$RUNNER_TEMP/release-verification-error.log"

verify_once() {
  gh api "repos/${GITHUB_REPOSITORY}/releases/${release_id}" \
    >"$RUNNER_TEMP/release-published.json"
  go run ./cmd/release-tool verify-release \
    --snapshot "$RUNNER_TEMP/release-published.json" \
    --directory "$asset_directory" \
    --tag "$release_tag" \
    --commit "$source_commit" \
    --draft=false \
    --prerelease="$prerelease" \
    --immutable=true
  exact_digest="$(docker buildx imagetools inspect \
    --format '{{.Manifest.Digest}}' "${IMAGE}:${VERSION}" | tr -d '\r\n')"
  [[ "$exact_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || {
    echo "exact image returned an invalid manifest digest: $exact_digest" >&2
    return 1
  }
  [ "$exact_digest" = "$SOURCE_DIGEST" ] || {
    echo "exact image ${IMAGE}:${VERSION} resolves to $exact_digest, expected $SOURCE_DIGEST" >&2
    return 1
  }
  bash scripts/verify-release-tag.sh "$release_tag" "$source_commit"
  gh release verify "$release_tag" --repo "$GITHUB_REPOSITORY"
  for asset in "$asset_directory"/*; do
    [ -f "$asset" ] || continue
    gh release verify-asset "$release_tag" "$asset" --repo "$GITHUB_REPOSITORY"
  done
  bash scripts/verify-release-latest.sh "$release_tag" "$make_latest"
  gh attestation verify "oci://${IMAGE}@${SOURCE_DIGEST}" \
    --repo "$GITHUB_REPOSITORY" \
    --cert-identity "$identity" \
    --source-digest "$source_commit" \
    --deny-self-hosted-runners
}

for attempt in $(seq 1 "$max_attempts"); do
  if verify_once >"$last_error" 2>&1; then
    cat "$last_error"
    exit 0
  fi
  if [ "$attempt" -lt "$max_attempts" ]; then
    echo "published Release attestation is not ready (attempt ${attempt}/${max_attempts}); retrying" >&2
    sleep "$retry_delay_seconds"
  fi
done

cat "$last_error" >&2
exit 1
