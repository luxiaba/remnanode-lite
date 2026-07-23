#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary_directory="$(mktemp -d)"
trap 'rm -rf "$temporary_directory"' EXIT

fail() {
  echo "release tag verification test: $*" >&2
  exit 1
}

cat >"$temporary_directory/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "$1" = api ]
case "$2" in
  */git/ref/tags/*)
    if [ "${TEST_REF_ERROR:-}" = true ]; then
      echo 'gh: internal server error (HTTP 500)' >&2
      exit 1
    fi
    if [ "${TEST_REF_MISSING:-}" = true ]; then
      echo 'gh: Not Found (HTTP 404)' >&2
      exit 1
    fi
    printf '{"object":{"type":"%s","sha":"%s"}}\n' \
      "${TEST_REF_TYPE:-tag}" "${TEST_TAG_OBJECT:-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}"
    ;;
  */git/tags/*)
    printf '{"object":{"type":"%s","sha":"%s"}}\n' \
      "${TEST_TARGET_TYPE:-commit}" "${TEST_TARGET_COMMIT:-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb}"
    ;;
  *) exit 2 ;;
esac
EOF
chmod 0755 "$temporary_directory/gh"

run_check() {
  env PATH="$temporary_directory:$PATH" \
    GITHUB_REPOSITORY=luxiaba/remnanode-lite \
    GH_TOKEN=test-token \
    "$ROOT_DIR/scripts/verify-release-tag.sh" "$@"
}

expected=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
run_check 2.8.0-rnl.1 "$expected" >/dev/null || fail "valid annotated tag was rejected"

TEST_REF_TYPE=commit TEST_TAG_OBJECT="$expected" \
  run_check 2.8.0 "$expected" >/dev/null || fail "valid lightweight tag was rejected"
TEST_REF_MISSING=true run_check --allow-missing 2.8.0 "$expected" >/dev/null ||
  fail "an absent pre-publication tag was rejected"
TEST_REF_MISSING=true run_check --require-missing 2.8.0 "$expected" >/dev/null ||
  fail "an absent required-missing tag was rejected"
if TEST_REF_MISSING=true run_check 2.8.0 "$expected" >/dev/null 2>&1; then
  fail "an absent published tag was accepted"
fi
if run_check --require-missing 2.8.0 "$expected" >/dev/null 2>&1; then
  fail "an existing pre-publication tag was accepted"
fi
if TEST_REF_ERROR=true run_check --allow-missing 2.8.0 "$expected" >/dev/null 2>&1; then
  fail "a non-404 tag lookup failure was accepted as absent"
fi
if TEST_TARGET_TYPE=tree run_check 2.8.0 "$expected" >/dev/null 2>&1; then
  fail "tag pointing to a non-commit object was accepted"
fi
if TEST_REF_TYPE=blob run_check 2.8.0 "$expected" >/dev/null 2>&1; then
  fail "unsupported ref object was accepted"
fi
if TEST_TARGET_COMMIT=cccccccccccccccccccccccccccccccccccccccc \
  run_check 2.8.0 "$expected" >/dev/null 2>&1; then
  fail "moved tag was accepted"
fi
if run_check 02.8.0 "$expected" >/dev/null 2>&1; then
  fail "invalid version tag was accepted"
fi

echo 'release tag verification tests passed'
