#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

status="$(git status --porcelain --untracked-files=all)"
if [ -n "$status" ]; then
  echo "release requires a clean worktree:" >&2
  echo "$status" >&2
  exit 1
fi
if git ls-files --error-unmatch cmd/m8-runtime-probe >/dev/null 2>&1; then
  echo "temporary M8 runtime probe must not be tracked" >&2
  exit 1
fi

official_source="${REMNANODE_OFFICIAL_SOURCE:-}"
[ -n "$official_source" ] || {
  echo "REMNANODE_OFFICIAL_SOURCE must point to a Git repository containing the pinned official commit" >&2
  exit 1
}
go run ./cmd/contract-source-check -source "$official_source"

version="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' internal/version/version.go)"
release_tag="${RELEASE_TAG:-${version}}"
RELEASE_TAG="$release_tag" bash scripts/check-version.sh

grep -Eq "^## \[${version//./\\.}\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$" CHANGELOG.md || {
  echo "CHANGELOG does not contain a dated ${version} release heading" >&2
  exit 1
}

REMNANODE_OFFICIAL_SOURCE='' bash scripts/check-go.sh
REMNANODE_DOCS_STRICT_TRANSLATIONS=1 \
  REMNANODE_OFFICIAL_SOURCE='' \
  bash scripts/check-repository.sh
sh release/native/install_test.sh

echo "release gate passed for $release_tag"
