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
[[ "$*" == *'verify-release'* ]]
[[ "$*" == *'--immutable=any'* || "$*" == *'--immutable=true'* ]]
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
    count=0
    [ ! -f "$TEST_TAG_COUNT" ] || count="$(cat "$TEST_TAG_COUNT")"
    count=$((count + 1))
    printf '%s\n' "$count" >"$TEST_TAG_COUNT"
    if [ "$count" -lt "${TEST_TAG_AFTER:-1}" ]; then
      echo 'HTTP 404: tag not visible yet' >&2
      exit 1
    fi
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
count=0
[ ! -f "$TEST_LATEST_COUNT" ] || count="$(cat "$TEST_LATEST_COUNT")"
count=$((count + 1))
printf '%s\n' "$count" >"$TEST_LATEST_COUNT"
tag=v2.8.0
if [ "$count" -lt "${TEST_LATEST_AFTER:-1}" ]; then
  tag=v0.0.1
fi
printf '{"tag_name":"%s"}\n' "$tag" >"$response_file"
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
    TEST_LATEST_COUNT="$test_dir/${case_name}.latest-count" \
    TEST_TAG_COUNT="$test_dir/${case_name}.tag-count" \
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
[ "$(grep -c -- '--immutable=true' "$test_dir/eventual.go.log")" -eq 1 ] ||
  fail "the final immutable snapshot was not fully reverified"
[ "$(wc -l <"$test_dir/eventual.sleep.log" | tr -d ' ')" -eq 2 ] ||
  fail "eventual checks did not wait exactly between failed attempts"
grep -Fq 'attestation pending on attempt 1' "$test_dir/eventual.out" ||
  fail "an eventual attestation error was hidden"

run_check latest \
  TEST_IMMUTABLE_AFTER=1 \
  TEST_LATEST_AFTER=3 \
  >"$test_dir/latest.out" 2>&1 ||
  fail "bounded GitHub Latest propagation did not recover"
[ "$(cat "$test_dir/latest.latest-count")" -eq 3 ] ||
  fail "GitHub Latest used the wrong retry boundary"
[ "$(grep -c -- '--immutable=true' "$test_dir/latest.go.log")" -eq 3 ] ||
  fail "the final Release snapshot was not reverified with each Latest retry"
[ "$(wc -l <"$test_dir/latest.sleep.log" | tr -d ' ')" -eq 2 ] ||
  fail "GitHub Latest propagation did not wait exactly between failed attempts"
grep -Fq 'GitHub latest is v0.0.1' "$test_dir/latest.out" ||
  fail "an eventual GitHub Latest error was hidden"

run_check tag \
  TEST_IMMUTABLE_AFTER=1 \
  TEST_TAG_AFTER=3 \
  >"$test_dir/tag.out" 2>&1 ||
  fail "bounded Git tag propagation did not recover"
[ "$(cat "$test_dir/tag.tag-count")" -eq 3 ] ||
  fail "Git tag propagation used the wrong retry boundary"
[ "$(wc -l <"$test_dir/tag.sleep.log" | tr -d ' ')" -eq 1 ] ||
  fail "Git tag propagation did not wait between attempts"
grep -Fq 'HTTP 404: tag not visible yet' "$test_dir/tag.out" ||
  fail "an eventual Git tag error was hidden"

echo "published Release verification tests passed"
