#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT

readonly commit=0123456789012345678901234567890123456789

fail() {
  echo "published Release verification test: $*" >&2
  exit 1
}

cat >"$test_dir/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$TEST_GO_LOG"
[[ "$*" == *'verify-release'*'--immutable=any'* ]]
if [ "${TEST_IDENTITY_ERROR:-false}" = true ]; then
  echo 'release-tool: deterministic asset mismatch' >&2
  exit 1
fi
echo 'verified release identity and assets'
EOF

cat >"$test_dir/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "$1:$2" in
  api:repos/*/releases/42)
    count=0
    [ ! -f "$TEST_API_COUNT" ] || count="$(cat "$TEST_API_COUNT")"
    count=$((count + 1))
    printf '%s\n' "$count" >"$TEST_API_COUNT"
    immutable=false
    [ "$count" -lt "${TEST_IMMUTABLE_AFTER:-1}" ] || immutable=true
    printf '{"draft":false,"immutable":%s}\n' "$immutable"
    ;;
  api:repos/*/git/ref/tags/v2.8.0)
    printf '{"object":{"type":"commit","sha":"%s"}}\n' "$TEST_COMMIT"
    ;;
  release:verify)
    count=0
    [ ! -f "$TEST_ATTEST_COUNT" ] || count="$(cat "$TEST_ATTEST_COUNT")"
    count=$((count + 1))
    printf '%s\n' "$count" >"$TEST_ATTEST_COUNT"
    if [ "$count" -le "${TEST_ATTEST_FAILURES:-0}" ]; then
      echo "attestation pending on attempt $count" >&2
      exit 1
    fi
    echo 'verified release attestation'
    ;;
  release:verify-asset) ;;
  *)
    echo "unexpected gh invocation: $*" >&2
    exit 2
    ;;
esac
EOF

cat >"$test_dir/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
response_file=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) response_file=$2; shift 2 ;;
    *) shift ;;
  esac
done
printf '{"tag_name":"v2.8.0"}\n' >"$response_file"
printf '200'
EOF

cat >"$test_dir/sleep" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
echo "$1" >>"$TEST_SLEEP_LOG"
EOF

chmod 0755 "$test_dir/go" "$test_dir/gh" "$test_dir/curl" "$test_dir/sleep"
mkdir "$test_dir/assets"

run_check() {
  local case_name=$1
  shift
  env \
    PATH="$test_dir:$PATH" \
    GH_TOKEN=test-token \
    GITHUB_REPOSITORY=luxiaba/remnanode-lite \
    RUNNER_TEMP="$test_dir" \
    RELEASE_VERIFY_ATTEMPTS=4 \
    RELEASE_VERIFY_DELAY_SECONDS=0 \
    TEST_API_COUNT="$test_dir/${case_name}.api-count" \
    TEST_ATTEST_COUNT="$test_dir/${case_name}.attest-count" \
    TEST_GO_LOG="$test_dir/${case_name}.go.log" \
    TEST_SLEEP_LOG="$test_dir/${case_name}.sleep.log" \
    TEST_COMMIT="$commit" \
    "$@" \
    /bin/bash "$root_dir/scripts/verify-published-release.sh" \
      42 v2.8.0 "$commit" false true "$test_dir/assets"
}

if run_check identity TEST_IDENTITY_ERROR=true >"$test_dir/identity.out" 2>&1; then
  fail "deterministic Release identity failure was accepted"
fi
grep -Fq 'deterministic asset mismatch' "$test_dir/identity.out" ||
  fail "deterministic failure lost its original diagnostic"
[ "$(cat "$test_dir/identity.api-count")" -eq 1 ] ||
  fail "deterministic failure was retried"
[ ! -e "$test_dir/identity.sleep.log" ] ||
  fail "deterministic failure entered an eventual-consistency wait"

run_check eventual \
  TEST_IMMUTABLE_AFTER=3 \
  TEST_ATTEST_FAILURES=1 \
  >"$test_dir/eventual.out" 2>&1 ||
  fail "bounded immutable and attestation propagation did not recover"
[ "$(cat "$test_dir/eventual.api-count")" -eq 3 ] ||
  fail "immutable state used the wrong retry boundary"
[ "$(cat "$test_dir/eventual.attest-count")" -eq 2 ] ||
  fail "attestation state used the wrong retry boundary"
[ "$(wc -l <"$test_dir/eventual.sleep.log" | tr -d ' ')" -eq 2 ] ||
  fail "eventual checks did not wait exactly between failed attempts"
grep -Fq 'attestation pending on attempt 1' "$test_dir/eventual.out" ||
  fail "an eventual attestation error was hidden"

echo "published Release verification tests passed"
