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
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"

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
[ -d "$asset_directory" ] || {
  echo "release asset directory does not exist: $asset_directory" >&2
  exit 2
}

readonly max_attempts="${RELEASE_VERIFY_ATTEMPTS:-12}"
readonly retry_delay_seconds="${RELEASE_VERIFY_DELAY_SECONDS:-5}"
[[ "$max_attempts" =~ ^[1-9][0-9]*$ ]] || {
  echo "RELEASE_VERIFY_ATTEMPTS must be a positive integer" >&2
  exit 2
}
[[ "$retry_delay_seconds" =~ ^[0-9]+$ ]] || {
  echo "RELEASE_VERIFY_DELAY_SECONDS must be a non-negative integer" >&2
  exit 2
}

snapshot="$RUNNER_TEMP/release-published.json"
gh api "repos/${GITHUB_REPOSITORY}/releases/${release_id}" >"$snapshot"
go run ./cmd/release-tool verify-release \
  --snapshot "$snapshot" \
  --directory "$asset_directory" \
  --tag "$release_tag" \
  --commit "$source_commit" \
  --draft=false \
  --prerelease="$prerelease" \
  --immutable=any
# The public Release can appear before its Git ref is visible through the tag
# endpoint. Validate an already visible ref now, then require it inside the
# bounded immutable-state retry below.
bash scripts/verify-release-tag.sh --allow-missing "$release_tag" "$source_commit"

# GitHub returns the public Release and its tag before the repository's
# "Latest" pointer is always visible through the API. The immutable retry
# below rechecks the complete final snapshot, including that pointer.

verify_immutable() {
  gh api "repos/${GITHUB_REPOSITORY}/releases/${release_id}" \
    >"$snapshot" || return 1
  jq -e '.draft == false and .immutable == true' "$snapshot" >/dev/null || {
    echo "GitHub Release $release_tag is public but not immutable yet" >&2
    return 1
  }
  go run ./cmd/release-tool verify-release \
    --snapshot "$snapshot" \
    --directory "$asset_directory" \
    --tag "$release_tag" \
    --commit "$source_commit" \
    --draft=false \
    --prerelease="$prerelease" \
    --immutable=true || return 1
  bash scripts/verify-release-tag.sh "$release_tag" "$source_commit" || return 1
  bash scripts/verify-release-latest.sh "$release_tag" "$make_latest" || return 1
}

verify_attestations() {
  gh release verify "$release_tag" --repo "$GITHUB_REPOSITORY" || return 1
  for asset in "$asset_directory"/*; do
    [ -f "$asset" ] || continue
    gh release verify-asset "$release_tag" "$asset" --repo "$GITHUB_REPOSITORY" || return 1
  done
}

retry_eventual_check() {
  local description=$1
  local error_file=$2
  shift 2
  local attempt
  for ((attempt = 1; attempt <= max_attempts; attempt++)); do
    if "$@" >"$error_file" 2>&1; then
      cat "$error_file"
      return 0
    fi
    cat "$error_file" >&2
    if [ "$attempt" -lt "$max_attempts" ]; then
      echo "${description} is not ready (attempt ${attempt}/${max_attempts}); retrying" >&2
      sleep "$retry_delay_seconds"
    fi
  done
  return 1
}

retry_eventual_check \
  "GitHub Release immutability" \
  "$RUNNER_TEMP/release-immutable-error.log" \
  verify_immutable
retry_eventual_check \
  "GitHub Release attestation" \
  "$RUNNER_TEMP/release-attestation-error.log" \
  verify_attestations

echo "verified immutable GitHub Release $release_tag and every published asset"
