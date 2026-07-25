#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT
mkdir -p "$test_dir/assets" "$test_dir/bin" "$test_dir/runner"

cat >"$test_dir/bin/gh" <<'EOF'
#!/bin/bash
set -euo pipefail
[ "$*" = "api repos/example/project/releases/42" ]
printf '%s\n' '{"id":42,"draft":true}'
EOF
chmod +x "$test_dir/bin/gh"

cat >"$test_dir/bin/go" <<'EOF'
#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >"$TEST_GO_LOG"
EOF
chmod +x "$test_dir/bin/go"

cat >"$test_dir/bin/bash" <<'EOF'
#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >"$TEST_BASH_LOG"
EOF
chmod +x "$test_dir/bin/bash"

tag=2.8.0-rnl.1
commit=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
PATH="$test_dir/bin:$PATH" \
  GH_TOKEN=test \
  GITHUB_REPOSITORY=example/project \
  RUNNER_TEMP="$test_dir/runner" \
  TEST_GO_LOG="$test_dir/go.log" \
  TEST_BASH_LOG="$test_dir/bash.log" \
  /bin/bash "$root_dir/scripts/verify-draft-release.sh" \
  42 "$tag" "$commit" true "$test_dir/assets"

[ "$(cat "$test_dir/runner/release-draft.json")" = '{"id":42,"draft":true}' ]
go_call="$(cat "$test_dir/go.log")"
for expected in \
  'run ./cmd/release-tool verify-release' \
  "--snapshot $test_dir/runner/release-draft.json" \
  "--directory $test_dir/assets" \
  "--tag $tag" \
  "--commit $commit" \
  '--draft=true' \
  '--prerelease=true' \
  '--immutable=false'; do
  [[ "$go_call" == *"$expected"* ]] || {
    echo "draft verification omitted: $expected" >&2
    exit 1
  }
done
[ "$(cat "$test_dir/bash.log")" = \
  "scripts/verify-release-tag.sh --require-missing $tag $commit" ]

if PATH="$test_dir/bin:$PATH" GH_TOKEN=test GITHUB_REPOSITORY=example/project \
  RUNNER_TEMP="$test_dir/runner" TEST_GO_LOG="$test_dir/invalid-go.log" \
  TEST_BASH_LOG="$test_dir/invalid-bash.log" \
  /bin/bash "$root_dir/scripts/verify-draft-release.sh" \
  invalid "$tag" "$commit" true "$test_dir/assets" >/dev/null 2>&1; then
  echo "draft verification accepted an invalid Release ID" >&2
  exit 1
fi
[ ! -e "$test_dir/invalid-go.log" ] && [ ! -e "$test_dir/invalid-bash.log" ]

echo "draft Release verification tests passed"
