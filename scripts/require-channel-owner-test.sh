#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT

fail() {
  echo "channel ownership test: $*" >&2
  exit 1
}

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

[ -n "$response_file" ]
body="${TEST_LATEST_BODY:-}"
[ -n "$body" ] || body='{}'
printf '%s' "$body" >"$response_file"
printf '%s' "${TEST_LATEST_STATUS:-200}"
exit "${TEST_CURL_EXIT:-0}"
EOF

cat >"$test_dir/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [ "${TEST_GH_EXIT:-0}" -ne 0 ]; then
  echo 'gh: upstream API failure' >&2
  exit "$TEST_GH_EXIT"
fi

case "$1:$2" in
  release:list) printf '%s\n' "${TEST_PREVIEW_RELEASES:-[]}" ;;
  *)
    echo "unexpected gh invocation: $*" >&2
    exit 2
    ;;
esac
EOF
chmod 0755 "$test_dir/curl" "$test_dir/gh"

run_stable() {
  local body=$1 status=$2 curl_exit=$3
  shift 3
  env \
    PATH="$test_dir:$PATH" \
    GH_TOKEN=test-token \
    GITHUB_REPOSITORY=luxiaba/remnanode-lite \
    GITHUB_API_URL=https://api.github.test \
    TEST_LATEST_BODY="$body" \
    TEST_LATEST_STATUS="$status" \
    TEST_CURL_EXIT="$curl_exit" \
    "$root_dir/scripts/require-channel-owner.sh" "$@"
}

run_preview() {
  local releases=$1 gh_exit=$2
  shift 2
  env \
    PATH="$test_dir:$PATH" \
    GH_TOKEN=test-token \
    GITHUB_REPOSITORY=luxiaba/remnanode-lite \
    GITHUB_API_URL=https://api.github.test \
    TEST_PREVIEW_RELEASES="$releases" \
    TEST_GH_EXIT="$gh_exit" \
    "$root_dir/scripts/require-channel-owner.sh" "$@"
}

output="$(run_stable '{"tag_name":"v2.8.1"}' 200 0 \
  --allow-non-owner v2.8.1 false)" ||
  fail "current stable Release was rejected"
[ "$output" = 'promote=true' ] || fail "current stable Release returned $output"

output="$(run_stable '{"message":"Not Found"}' 404 0 \
  --allow-non-owner v2.8.1 false)" ||
  fail "first stable Release did not claim an empty latest channel"
[ "$output" = 'promote=true' ] || fail "empty latest channel returned $output"

if ! output="$(run_stable '{"tag_name":"v2.8.1"}' 200 0 \
  --allow-non-owner v2.8.0 false 2>"$test_dir/stable-old.err")"; then
  fail "older stable Release did not finish as a no-op"
fi
[ "$output" = 'promote=false' ] || fail "older stable Release returned $output"
grep -Fq 'leaving latest unchanged' "$test_dir/stable-old.err" ||
  fail "older stable Release did not explain the no-op"

if run_stable '{"tag_name":"v2.8.1"}' 200 0 \
  v2.8.0 false >"$test_dir/stable-required.out" 2>&1; then
  fail "older stable Release was accepted without no-op mode"
fi

if run_stable '{"message":"Bad credentials"}' 401 0 \
  --allow-non-owner v2.8.1 false >"$test_dir/stable-error.out" 2>&1; then
  fail "stable API failure was treated as a no-op"
fi
grep -Fq 'HTTP 401' "$test_dir/stable-error.out" ||
  fail "stable API failure lost its diagnostic"

preview_current='[{"tagName":"v2.8.1-rnl.2","isPrerelease":true}]'
output="$(run_preview "$preview_current" 0 \
  --allow-non-owner v2.8.1-rnl.2 true)" ||
  fail "current preview Release was rejected"
[ "$output" = 'promote=true' ] || fail "current preview Release returned $output"

output="$(run_preview '[]' 0 \
  --allow-non-owner v2.8.1-rnl.1 true)" ||
  fail "first preview Release did not claim an empty preview channel"
[ "$output" = 'promote=true' ] || fail "empty preview channel returned $output"

preview_newer='[{"tagName":"v2.8.1-rnl.3","isPrerelease":true}]'
if ! output="$(run_preview "$preview_newer" 0 \
  --allow-non-owner v2.8.1-rnl.2 true 2>"$test_dir/preview-old.err")"; then
  fail "older preview Release did not finish as a no-op"
fi
[ "$output" = 'promote=false' ] || fail "older preview Release returned $output"
grep -Fq 'leaving preview unchanged' "$test_dir/preview-old.err" ||
  fail "older preview Release did not explain the no-op"

if run_preview 'not-json' 0 \
  --allow-non-owner v2.8.1-rnl.2 true >"$test_dir/preview-json.out" 2>&1; then
  fail "malformed preview metadata was treated as a no-op"
fi

if run_preview '[]' 1 \
  --allow-non-owner v2.8.1-rnl.2 true >"$test_dir/preview-api.out" 2>&1; then
  fail "preview API failure was treated as a no-op"
fi
grep -Fq 'upstream API failure' "$test_dir/preview-api.out" ||
  fail "preview API failure lost its diagnostic"

echo 'channel ownership tests passed'
