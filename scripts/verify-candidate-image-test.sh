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
    [[ "$*" == *"${IMAGE}@${TEST_EXPECTED_DIGEST}"* ]]
    printf '{"schemaVersion":2}\n'
    ;;
  *'imagetools inspect --format'*'.SBOM'*)
    [[ "$*" == *"${IMAGE}@${TEST_EXPECTED_DIGEST}"* ]]
    case "${TEST_SBOM_STATE:-valid}" in
      valid)
        printf '%s\n' '{"SPDX":{"SPDXID":"SPDXRef-DOCUMENT","spdxVersion":"SPDX-2.3","dataLicense":"CC0-1.0","documentNamespace":"https://example.test/sbom","creationInfo":{"creators":["Tool: test"]}}}'
        ;;
      invalid) printf '{}\n' ;;
      *) exit 2 ;;
    esac
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

[ "$1" = run ]
[ "$2" = ./cmd/release-tool ]
[ "$3" = verify-index ]
shift 3
manifest=""
digest=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --manifest) manifest=$2; shift 2 ;;
    --digest) digest=$2; shift 2 ;;
    *) exit 2 ;;
  esac
done
[ "$manifest" = "$RUNNER_TEMP/candidate-index.json" ]
[ "$digest" = "$TEST_EXPECTED_DIGEST" ]
echo 'OCI verification log'
EOF

cat >"$test_dir/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[ "$1" = attestation ]
[ "$2" = verify ]
[ "$3" = "oci://${IMAGE}@${TEST_EXPECTED_DIGEST}" ]
shift 3
repo=""
identity=""
source_digest=""
predicate_type=""
deny_self_hosted=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo) repo=$2; shift 2 ;;
    --cert-identity) identity=$2; shift 2 ;;
    --source-digest) source_digest=$2; shift 2 ;;
    --predicate-type) predicate_type=$2; shift 2 ;;
    --deny-self-hosted-runners) deny_self_hosted=true; shift ;;
    *) exit 2 ;;
  esac
done
[ "$repo" = "$GITHUB_REPOSITORY" ]
[ "$identity" = "https://github.com/${GITHUB_REPOSITORY}/.github/workflows/container.yml@refs/heads/main" ]
[ "$source_digest" = "$GITHUB_SHA" ]
[ "$predicate_type" = https://slsa.dev/provenance/v1 ]
[ "$deny_self_hosted" = true ]
echo 'attestation verification log'
EOF
chmod 0755 "$test_dir/docker" "$test_dir/go" "$test_dir/gh"

run_check() {
  local expected_digest="sha256:0000000000000000000000000000000000000000000000000000000000000000"
  if [ "${1:-}" = --digest ]; then
    expected_digest=$2
  fi
  env \
    PATH="$test_dir:$PATH" \
    GH_TOKEN=test-token \
    GITHUB_REPOSITORY=luxiaba/remnanode-lite \
    GITHUB_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
    IMAGE=ghcr.io/luxiaba/remnanode-lite \
    RUNNER_TEMP="$test_dir" \
    TEST_EXPECTED_DIGEST="$expected_digest" \
    bash "$root_dir/scripts/verify-candidate-image.sh" "$@"
}

output="$(TEST_IMAGE_STATE=present run_check 2>"$test_dir/present.log")"
expected=$'state=present\ndigest=sha256:0000000000000000000000000000000000000000000000000000000000000000'
[ "$output" = "$expected" ] || fail "structured output contains logs or wrong values: $output"
grep -Fq 'OCI verification log' "$test_dir/present.log" || fail "OCI log was not sent to stderr"
grep -Fq 'attestation verification log' "$test_dir/present.log" || fail "attestation log was not sent to stderr"

output="$(TEST_IMAGE_STATE=missing run_check --digest sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 2>/dev/null)"
expected=$'state=present\ndigest=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
[ "$output" = "$expected" ] || fail "explicit digest verification inspected the mutable candidate tag"
if run_check --allow-missing --digest sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb >/dev/null 2>&1; then
  fail "explicit digest verification accepted --allow-missing"
fi
if run_check --digest not-a-digest >/dev/null 2>&1; then
  fail "explicit digest verification accepted an invalid digest"
fi

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
if TEST_SBOM_STATE=invalid run_check >/dev/null 2>&1; then
  fail "candidate without a valid per-platform SPDX SBOM was accepted"
fi

echo "candidate image verification tests passed"
