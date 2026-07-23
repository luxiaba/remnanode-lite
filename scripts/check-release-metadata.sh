#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 X.Y.Z[-rnl.N]" >&2
  exit 2
fi

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

release_tag=$1
RELEASE_TAG="$release_tag" bash scripts/check-version.sh

version="$release_tag"
grep -Eq "^## \[${version//./\\.}\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$" CHANGELOG.md || {
  echo "CHANGELOG does not contain a dated ${version} release heading" >&2
  exit 1
}

echo "release metadata is ready for ${release_tag}"
