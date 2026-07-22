#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT

fail() {
  echo "release state test: $*" >&2
  exit 1
}

cat >"$test_dir/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
cat "$RELEASE_FIXTURE"
EOF
chmod 0755 "$test_dir/gh"

write_fixture() {
  local state=$1
  case "$state" in
    absent)
      printf '[]\n' >"$test_dir/release.json"
      ;;
    draft)
      cat >"$test_dir/release.json" <<'EOF'
[{"id":42,"tag_name":"v2.8.0","target_commitish":"0123456789012345678901234567890123456789","draft":true,"prerelease":false,"immutable":false}]
EOF
      ;;
    stale-draft)
      cat >"$test_dir/release.json" <<'EOF'
[{"id":42,"tag_name":"v2.8.0","target_commitish":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","draft":true,"prerelease":false,"immutable":false}]
EOF
      ;;
    published)
      cat >"$test_dir/release.json" <<'EOF'
[{"id":42,"tag_name":"v2.8.0","target_commitish":"0123456789012345678901234567890123456789","draft":false,"prerelease":false,"immutable":true}]
EOF
      ;;
    mutable)
      cat >"$test_dir/release.json" <<'EOF'
[{"id":42,"tag_name":"v2.8.0","target_commitish":"0123456789012345678901234567890123456789","draft":false,"prerelease":false,"immutable":false}]
EOF
      ;;
  esac
}

run_state() {
  PATH="$test_dir:$PATH" \
    GH_TOKEN=test-token \
    GITHUB_REPOSITORY=luxiaba/remnanode-lite \
    RELEASE_FIXTURE="$test_dir/release.json" \
    bash "$root_dir/scripts/release-state.sh" \
      v2.8.0 0123456789012345678901234567890123456789 false
}

write_fixture absent
[ "$(run_state)" = 'state=absent' ] || fail "absent Release was not recognized"

write_fixture draft
expected=$'state=draft\nrelease_id=42'
[ "$(run_state)" = "$expected" ] || fail "draft Release was not recognized"

write_fixture published
expected=$'state=published\nrelease_id=42'
[ "$(run_state)" = "$expected" ] || fail "published immutable Release was not recognized"

write_fixture mutable
if run_state >/dev/null 2>&1; then
  fail "published mutable Release was accepted"
fi

write_fixture stale-draft
if run_state >"$test_dir/stale-draft.out" 2>&1; then
  fail "a draft from an older main commit was accepted"
fi
grep -Fq 'Delete only that draft' "$test_dir/stale-draft.out" ||
  fail "stale draft recovery guidance was not emitted"

echo "release state tests passed"
