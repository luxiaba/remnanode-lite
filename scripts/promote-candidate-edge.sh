#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <image> <source-manifest-digest> <40-character-main-commit>" >&2
  exit 2
fi

image=$1
source_digest=$2
source_commit=$3
[[ "$source_commit" =~ ^[0-9a-f]{40}$ ]] || {
  echo "invalid source commit: $source_commit" >&2
  exit 2
}

git fetch --no-tags origin '+refs/heads/main:refs/remotes/origin/main'
if [ "$source_commit" != "$(git rev-parse origin/main)" ]; then
  echo "main advanced; leaving edge unchanged"
  exit 0
fi

bash scripts/promote-image-tag.sh mutable "$image" "$source_digest" edge
