#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT
mkdir -p "$test_dir/bin"

commit_a=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
commit_b=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb

cat >"$test_dir/bin/git" <<'EOF'
#!/bin/bash
set -euo pipefail
case "$1" in
  fetch)
    [ "$*" = "fetch --no-tags origin +refs/heads/main:refs/remotes/origin/main" ]
    ;;
  rev-parse)
    [ "$*" = "rev-parse origin/main" ]
    printf '%s\n' "$TEST_MAIN_COMMIT"
    ;;
  *)
    exit 64
    ;;
esac
EOF
chmod +x "$test_dir/bin/git"

PATH="$test_dir/bin:$PATH" TEST_MAIN_COMMIT="$commit_a" \
  bash "$root_dir/scripts/require-current-main.sh" "$commit_a"
if PATH="$test_dir/bin:$PATH" TEST_MAIN_COMMIT="$commit_b" \
  bash "$root_dir/scripts/require-current-main.sh" "$commit_a" >/dev/null 2>&1; then
  echo "require-current-main accepted a stale commit" >&2
  exit 1
fi

cat >"$test_dir/bin/bash" <<'EOF'
#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >>"$TEST_BASH_LOG"
EOF
chmod +x "$test_dir/bin/bash"

image=ghcr.io/example/remnanode-lite
digest=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
PATH="$test_dir/bin:$PATH" TEST_MAIN_COMMIT="$commit_a" TEST_BASH_LOG="$test_dir/promote.log" \
  /bin/bash "$root_dir/scripts/promote-candidate-edge.sh" "$image" "$digest" "$commit_a"
[ "$(cat "$test_dir/promote.log")" = \
  "scripts/promote-image-tag.sh mutable $image $digest edge" ] || {
  echo "edge promotion did not forward the accepted candidate" >&2
  exit 1
}

rm -f "$test_dir/promote.log"
output="$(PATH="$test_dir/bin:$PATH" TEST_MAIN_COMMIT="$commit_b" TEST_BASH_LOG="$test_dir/promote.log" \
  /bin/bash "$root_dir/scripts/promote-candidate-edge.sh" "$image" "$digest" "$commit_a")"
[ "$output" = "main advanced; leaving edge unchanged" ]
[ ! -e "$test_dir/promote.log" ] || {
  echo "stale candidate attempted to move edge" >&2
  exit 1
}

echo "main release guard tests passed"
