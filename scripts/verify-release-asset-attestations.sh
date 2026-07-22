#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <release-asset-directory> <40-character-source-commit>" >&2
  exit 2
fi

directory=$1
source_commit=$2
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

[ -d "$directory" ] || {
  echo "release asset directory does not exist: $directory" >&2
  exit 2
}
[[ "$source_commit" =~ ^[0-9a-f]{40}$ ]] || {
  echo "invalid source commit: $source_commit" >&2
  exit 2
}

identity="https://github.com/${GITHUB_REPOSITORY}/.github/workflows/container.yml@refs/heads/main"
for asset in "$directory"/*; do
  [ -f "$asset" ] || continue
  gh attestation verify "$asset" \
    --repo "$GITHUB_REPOSITORY" \
    --cert-identity "$identity" \
    --source-digest "$source_commit" \
    --deny-self-hosted-runners
done
