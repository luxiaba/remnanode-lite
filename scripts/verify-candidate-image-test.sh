#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT

fail() {
  echo "candidate image verification test: $*" >&2
  exit 1
}

cat >"$test_dir/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  *'imagetools inspect --raw'*)
    printf '{"schemaVersion":2}\n'
    ;;
  *'imagetools inspect --format'*)
    case "${TEST_IMAGE_STATE:-present}" in
      present) printf 'sha256:%064d\n' 0 ;;
      invalid) printf 'not-a-digest\n' ;;
      missing) echo 'ERROR: manifest unknown: not found' >&2; exit 1 ;;
      error) echo 'ERROR: registry unavailable (HTTP 500)' >&2; exit 1 ;;
      *) exit 2 ;;
    esac
    ;;
  *) exit 2 ;;
esac
EOF

cat >"$test_dir/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
echo 'OCI verification log'
EOF

cat >"$test_dir/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
echo 'attestation verification log'
EOF
chmod 0755 "$test_dir/docker" "$test_dir/go" "$test_dir/gh"

run_check() {
  env \
    PATH="$test_dir:$PATH" \
    GH_TOKEN=test-token \
    GITHUB_REPOSITORY=luxiaba/remnanode-lite \
    GITHUB_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
    IMAGE=ghcr.io/luxiaba/remnanode-lite \
    RUNNER_TEMP="$test_dir" \
    bash "$root_dir/scripts/verify-candidate-image.sh" "$@"
}

output="$(TEST_IMAGE_STATE=present run_check 2>"$test_dir/present.log")"
expected=$'state=present\ndigest=sha256:0000000000000000000000000000000000000000000000000000000000000000'
[ "$output" = "$expected" ] || fail "structured output contains logs or wrong values: $output"
grep -Fq 'OCI verification log' "$test_dir/present.log" || fail "OCI log was not sent to stderr"
grep -Fq 'attestation verification log' "$test_dir/present.log" || fail "attestation log was not sent to stderr"

output="$(TEST_IMAGE_STATE=missing run_check --allow-missing 2>/dev/null)"
[ "$output" = 'state=absent' ] || fail "missing candidate did not return state=absent"
if TEST_IMAGE_STATE=missing run_check >/dev/null 2>&1; then
  fail "missing candidate was accepted without --allow-missing"
fi
if TEST_IMAGE_STATE=error run_check --allow-missing >/dev/null 2>&1; then
  fail "registry failure was accepted as a missing candidate"
fi
if TEST_IMAGE_STATE=invalid run_check --allow-missing >/dev/null 2>&1; then
  fail "invalid candidate digest was accepted"
fi

echo "candidate image verification tests passed"
