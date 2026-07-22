#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 5 ]; then
  echo "usage: $0 <release-id> <tag> <commit> <true|false-prerelease> <asset-directory>" >&2
  exit 2
fi

release_id=$1
release_tag=$2
source_commit=$3
prerelease=$4
asset_directory=$5
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"

[[ "$release_id" =~ ^[1-9][0-9]*$ ]] || {
  echo "invalid GitHub Release ID: $release_id" >&2
  exit 2
}

gh api "repos/${GITHUB_REPOSITORY}/releases/${release_id}" \
  >"$RUNNER_TEMP/release-draft.json"
go run ./cmd/release-tool verify-release \
  --snapshot "$RUNNER_TEMP/release-draft.json" \
  --directory "$asset_directory" \
  --tag "$release_tag" \
  --commit "$source_commit" \
  --draft=true \
  --prerelease="$prerelease" \
  --immutable=false
# GitHub creates the tag only when this verified draft is made public. Do not
# let a draft adopt a tag that was created outside the release workflow.
bash scripts/verify-release-tag.sh --require-missing "$release_tag" "$source_commit"
