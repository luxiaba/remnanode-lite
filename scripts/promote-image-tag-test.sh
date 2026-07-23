#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT

readonly digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
readonly other_digest=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb

fail() {
  echo "image tag promotion test: $*" >&2
  exit 1
}

cat >"$test_dir/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >>"$TEST_DOCKER_LOG"
case "$*" in
  *'imagetools inspect --format'*)
    if [ -e "$TEST_PROMOTED_MARKER" ]; then
      printf '%s\n' "$TEST_SOURCE_DIGEST"
      exit 0
    fi
    case "$TEST_TARGET_STATE" in
      missing)
        echo 'ERROR: manifest unknown: not found' >&2
        exit 1
        ;;
      same) printf '%s\n' "$TEST_SOURCE_DIGEST" ;;
      conflict) printf '%s\n' "$TEST_OTHER_DIGEST" ;;
      error)
        echo 'ERROR: registry unavailable (HTTP 500)' >&2
        exit 1
        ;;
      *) exit 2 ;;
    esac
    ;;
  *'imagetools create'*)
    touch "$TEST_PROMOTED_MARKER"
    ;;
  *) exit 2 ;;
esac
EOF
chmod 0755 "$test_dir/docker"

run_check() {
  local mode=$1 state=$2 tag=$3 case_name=$4
  local marker="$test_dir/${case_name}.promoted"
  local log="$test_dir/${case_name}.log"
  env \
    PATH="$test_dir:$PATH" \
    TEST_DOCKER_LOG="$log" \
    TEST_PROMOTED_MARKER="$marker" \
    TEST_SOURCE_DIGEST="$digest" \
    TEST_OTHER_DIGEST="$other_digest" \
    TEST_TARGET_STATE="$state" \
    bash "$root_dir/scripts/promote-image-tag.sh" \
      "$mode" ghcr.io/luxiaba/remnanode-lite "$digest" "$tag"
}

run_check immutable missing 2.8.0 missing >/dev/null ||
  fail "missing immutable tag was not created"
[ "$(grep -c 'imagetools create' "$test_dir/missing.log")" -eq 1 ] ||
  fail "missing immutable tag did not perform exactly one create"

run_check immutable same 2.8.0 same >/dev/null ||
  fail "matching immutable tag was not accepted"
if grep -q 'imagetools create' "$test_dir/same.log"; then
  fail "matching immutable tag was rewritten"
fi

if run_check immutable conflict 2.8.0 conflict >"$test_dir/conflict.out" 2>&1; then
  fail "conflicting immutable tag was overwritten"
fi
grep -Fq 'refusing to move immutable tag' "$test_dir/conflict.out" ||
  fail "immutable conflict did not explain the refusal"
if grep -q 'imagetools create' "$test_dir/conflict.log"; then
  fail "immutable conflict attempted a create"
fi

if run_check immutable error 2.8.0 error >"$test_dir/error.out" 2>&1; then
  fail "registry failure was treated as a missing immutable tag"
fi
grep -Fq 'could not determine whether immutable tag' "$test_dir/error.out" ||
  fail "registry failure did not retain its diagnostic"

run_check mutable conflict latest mutable >/dev/null ||
  fail "moving channel was not updated"
[ "$(grep -c 'imagetools create' "$test_dir/mutable.log")" -eq 1 ] ||
  fail "moving channel did not perform exactly one create"

echo "image tag promotion tests passed"
