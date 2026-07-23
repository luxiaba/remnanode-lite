#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary_directory="$(mktemp -d)"
trap 'rm -rf "$temporary_directory"' EXIT

fail() {
  echo "release latest verification test: $*" >&2
  exit 1
}

cat >"$temporary_directory/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

response_file=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output|-o)
      response_file="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

[ -n "$response_file" ]
body="${TEST_LATEST_BODY:-}"
[ -n "$body" ] || body='{}'
printf '%s' "$body" >"$response_file"
printf '%s' "${TEST_LATEST_STATUS:-200}"
exit "${TEST_LATEST_CURL_EXIT:-0}"
EOF
chmod 0755 "$temporary_directory/curl"

run_check() {
  local status=$1 body=$2 curl_exit=$3 expected_tag=$4 make_latest=$5
  shift 5
  env \
    PATH="$temporary_directory:$PATH" \
    GITHUB_API_URL=https://api.github.test \
    GITHUB_REPOSITORY=luxiaba/remnanode-lite \
    GH_TOKEN=test-token \
    TEST_LATEST_STATUS="$status" \
    TEST_LATEST_BODY="$body" \
    TEST_LATEST_CURL_EXIT="$curl_exit" \
    "$ROOT_DIR/scripts/verify-release-latest.sh" "$@" "$expected_tag" "$make_latest"
}

assert_success() {
  local name=$1 status=$2 body=$3 curl_exit=$4 expected_tag=$5 make_latest=$6 output
  shift 6
  if ! output="$(run_check "$status" "$body" "$curl_exit" "$expected_tag" "$make_latest" "$@" 2>&1)"; then
    fail "$name unexpectedly failed: $output"
  fi
}

assert_failure() {
  local name=$1 needle=$2 status=$3 body=$4 curl_exit=$5 expected_tag=$6 make_latest=$7 output
  if output="$(run_check "$status" "$body" "$curl_exit" "$expected_tag" "$make_latest" 2>&1)"; then
    fail "$name unexpectedly passed"
  fi
  [[ "$output" == *"$needle"* ]] ||
    fail "$name failed for the wrong reason: $output"
}

assert_success \
  'stable release is GitHub latest' 200 '{"tag_name":"2.8.0"}' 0 2.8.0 true
output="$(run_check 404 '{"message":"Not Found"}' 0 2.8.0 true --allow-non-owner)" ||
  fail 'first stable release did not claim an empty latest pointer in recovery mode'
[ "$output" = 'owner=true' ] ||
  fail "empty latest recovery owner output was $output"
assert_success \
  'preview remains below an existing stable latest' 200 '{"tag_name":"2.8.0"}' 0 2.8.1-rnl.1 false
assert_success \
  'first preview has no stable latest yet' 404 '{"message":"Not Found"}' 0 2.8.0-rnl.1 false
assert_failure \
  'stable release cannot accept no latest' 'expected 2.8.0' \
  404 '{"message":"Not Found"}' 0 2.8.0 true
assert_failure \
  'preview cannot become GitHub latest' 'preview release must not be GitHub latest' \
  200 '{"tag_name":"2.8.0-rnl.1"}' 0 2.8.0-rnl.1 false
assert_failure \
  'authorization errors fail closed' 'HTTP 401' \
  401 '{"message":"Bad credentials"}' 0 2.8.0 true
assert_failure \
  'transport errors fail closed' 'could not query GitHub latest release' \
  000 '{}' 7 2.8.0 true
assert_failure \
  'malformed successful response fails closed' 'response has no tag_name' \
  200 '{}' 0 2.8.0 true

echo 'release latest verification tests passed'
