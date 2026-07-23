#!/usr/bin/env bash
set -euo pipefail

: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GH_REPO:?GH_REPO is required}"

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

pinned_version="$(tr -d ' \n\r' <internal/version/contract.version)"
latest_version="$(gh api repos/remnawave/node/releases/latest --jq .tag_name)"
[[ "$latest_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  echo "official Node returned an invalid release tag: $latest_version" >&2
  exit 1
}
if [ "$latest_version" = "$pinned_version" ]; then
  echo "official Node remains at $pinned_version"
  exit 0
fi

title="chore: sync official Node ${latest_version}"
issues="$(gh issue list --state open --limit 100 --json title)"
if jq -e --arg title "$title" '.[] | select(.title == $title)' <<<"$issues" >/dev/null; then
  echo "sync issue already exists: $title"
  exit 0
fi

gh issue create \
  --title "$title" \
  --body "Official remnawave/node released ${latest_version}; the current compatibility baseline is ${pinned_version}. Update the pinned source commit, contract evidence, implementation, tests, and acceptance scope before changing ContractVersion. Choose the project Version independently according to docs/versioning.md."
