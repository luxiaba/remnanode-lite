#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <40-character-main-commit>" >&2
  exit 2
fi

expected_commit=$1
[[ "$expected_commit" =~ ^[0-9a-f]{40}$ ]] || {
  echo "invalid expected main commit: $expected_commit" >&2
  exit 2
}

git fetch --no-tags origin '+refs/heads/main:refs/remotes/origin/main'
main_commit="$(git rev-parse origin/main)"
[ "$expected_commit" = "$main_commit" ] || {
  echo "release dispatch is stale: main is $main_commit, not $expected_commit" >&2
  exit 1
}
