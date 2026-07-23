#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <X.Y.Z[-rnl.N]-tag> <X.Y.Z[-rnl.N]-version> <true|false-prerelease>" >&2
  exit 2
fi

release_tag=$1
version=$2
prerelease=$3
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${IMAGE:?IMAGE is required}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"

[ "$release_tag" = "$version" ] || {
  echo "release tag $release_tag does not match version $version" >&2
  exit 2
}
case "$prerelease" in
  true|false) ;;
  *) echo "expected prerelease must be true or false" >&2; exit 2 ;;
esac

git fetch origin "refs/tags/${release_tag}:refs/tags/${release_tag}"
release_commit="$(git rev-list -n 1 "$release_tag")"
bash scripts/verify-release-tag.sh "$release_tag" "$release_commit" >&2
release_state="$(bash scripts/release-state.sh "$release_tag" "$release_commit" "$prerelease")"
[ "$(sed -n 's/^state=//p' <<<"$release_state")" = published ] || {
  echo "${release_tag} is not an immutable published Release" >&2
  exit 1
}
gh release verify "$release_tag" --repo "$GITHUB_REPOSITORY" >&2

index_directory="$(mktemp -d "${RUNNER_TEMP%/}/release-index.XXXXXX")"
trap 'rm -rf "$index_directory"' EXIT
release_index="$index_directory/release-index.json"
gh release download "$release_tag" \
  --repo "$GITHUB_REPOSITORY" \
  --pattern release-index.json \
  --dir "$index_directory" \
  --clobber >&2
[ -f "$release_index" ] || {
  echo "${release_tag} did not provide release-index.json" >&2
  exit 1
}
gh release verify-asset "$release_tag" "$release_index" \
  --repo "$GITHUB_REPOSITORY" >&2

identity="https://github.com/${GITHUB_REPOSITORY}/.github/workflows/container.yml@refs/heads/main"
gh attestation verify "$release_index" \
  --repo "$GITHUB_REPOSITORY" \
  --cert-identity "$identity" \
  --source-digest "$release_commit" \
  --predicate-type https://slsa.dev/provenance/v1 \
  --deny-self-hosted-runners >&2
index_output="$(go run ./cmd/release-tool verify-release-index \
  --file "$release_index" \
  --tag "$release_tag" \
  --image "$IMAGE" \
  --source-revision "$release_commit")"
index_digest="$(sed -n 's/^index_digest=//p' <<<"$index_output")"
[[ "$index_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || {
  echo "release index verification returned an invalid digest" >&2
  exit 1
}

candidate_output="$(
  GITHUB_SHA="$release_commit" \
    bash scripts/verify-candidate-image.sh --digest "$index_digest"
)"
candidate_state="$(sed -n 's/^state=//p' <<<"$candidate_output")"
candidate_digest="$(sed -n 's/^digest=//p' <<<"$candidate_output")"
[ "$candidate_state" = present ] && [ "$candidate_digest" = "$index_digest" ] || {
  echo "release candidate verification returned invalid output" >&2
  exit 1
}

printf 'commit=%s\ndigest=%s\n' "$release_commit" "$candidate_digest"
