#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT
mkdir -p "$test_dir/assets" "$test_dir/bin"
touch "$test_dir/assets/one" "$test_dir/assets/two"

cat >"$test_dir/bin/gh" <<'EOF'
#!/bin/bash
set -euo pipefail
[ "$1" = attestation ] && [ "$2" = verify ]
asset=$3
shift 3
[ "$*" = "--repo example/project --cert-identity https://github.com/example/project/.github/workflows/container.yml@refs/heads/main --source-digest aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa --predicate-type https://slsa.dev/provenance/v1 --deny-self-hosted-runners" ]
basename "$asset" >>"$TEST_GH_LOG"
EOF
chmod +x "$test_dir/bin/gh"

PATH="$test_dir/bin:$PATH" \
  GH_TOKEN=test \
  GITHUB_REPOSITORY=example/project \
  TEST_GH_LOG="$test_dir/gh.log" \
  bash "$root_dir/scripts/verify-release-asset-attestations.sh" \
  "$test_dir/assets" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

[ "$(sed -n '1p' "$test_dir/gh.log")" = one ]
[ "$(sed -n '2p' "$test_dir/gh.log")" = two ]
[ "$(wc -l <"$test_dir/gh.log" | tr -d ' ')" = 2 ]

if PATH="$test_dir/bin:$PATH" GH_TOKEN=test GITHUB_REPOSITORY=example/project \
  TEST_GH_LOG="$test_dir/invalid.log" \
  bash "$root_dir/scripts/verify-release-asset-attestations.sh" \
  "$test_dir/assets" invalid >/dev/null 2>&1; then
  echo "asset attestation check accepted an invalid source commit" >&2
  exit 1
fi
[ ! -e "$test_dir/invalid.log" ]

echo "release asset attestation tests passed"
