#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "$test_dir"' EXIT

readonly commit=0123456789012345678901234567890123456789
readonly digest="sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

fail() {
  echo "release image verification test: $*" >&2
  exit 1
}

cat >"$test_dir/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "$1" in
  fetch) ;;
  rev-list) printf '%s\n' "$TEST_COMMIT" ;;
  *) exit 2 ;;
esac
EOF

cat >"$test_dir/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "$*" in
  *'imagetools inspect --format'*'.SBOM'*)
    printf '%s\n' '{"SPDX":{"SPDXID":"SPDXRef-DOCUMENT","spdxVersion":"SPDX-2.3","dataLicense":"CC0-1.0","documentNamespace":"https://example.test/sbom","creationInfo":{"creators":["Tool: test"]}}}'
    ;;
  *'imagetools inspect --format'*)
    [[ "$*" != *":2.8.0" ]] || {
      echo 'reconciliation inspected the exact tag before restoring it' >&2
      exit 1
    }
    printf '%s\n' "$TEST_DIGEST"
    ;;
  *'imagetools inspect --raw'*) printf '{"schemaVersion":2}\n' ;;
  *) exit 2 ;;
esac
EOF

cat >"$test_dir/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[ "$1" = run ]
[ "$2" = ./cmd/release-tool ]
[ "$3" = verify-index ]
EOF

cat >"$test_dir/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "$1" in
  api)
    endpoint="${!#}"
    case "$endpoint" in
      */releases\?per_page=100)
        case "${TEST_RELEASE_STATE:-published}" in
          published)
            printf '[{"id":42,"tag_name":"v2.8.0","target_commitish":"%s","draft":false,"prerelease":false,"immutable":true}]\n' "$TEST_COMMIT"
            ;;
          draft)
            printf '[{"id":42,"tag_name":"v2.8.0","target_commitish":"%s","draft":true,"prerelease":false,"immutable":false}]\n' "$TEST_COMMIT"
            ;;
          mutable)
            printf '[{"id":42,"tag_name":"v2.8.0","target_commitish":"%s","draft":false,"prerelease":false,"immutable":false}]\n' "$TEST_COMMIT"
            ;;
          *) exit 2 ;;
        esac
        ;;
      */git/ref/tags/v2.8.0)
        printf '{"object":{"type":"commit","sha":"%s"}}\n' "$TEST_COMMIT"
        ;;
      *) exit 2 ;;
    esac
    ;;
  release)
    [ "$2" = verify ]
    ;;
  attestation)
    [ "$2" = verify ]
    ;;
  *) exit 2 ;;
esac
EOF
chmod 0755 "$test_dir/git" "$test_dir/docker" "$test_dir/go" "$test_dir/gh"

run_check() {
  env \
    PATH="$test_dir:$PATH" \
    GH_TOKEN=test-token \
    GITHUB_REPOSITORY=luxiaba/remnanode-lite \
    IMAGE=ghcr.io/luxiaba/remnanode-lite \
    RUNNER_TEMP="$test_dir" \
    TEST_COMMIT="$commit" \
    TEST_DIGEST="$digest" \
    bash "$root_dir/scripts/verify-release-image.sh" v2.8.0 2.8.0 false
}

output="$(run_check 2>"$test_dir/published.log")"
expected="commit=$commit
digest=$digest"
[ "$output" = "$expected" ] || fail "published Release output was $output"

if TEST_RELEASE_STATE=draft run_check >"$test_dir/draft.out" 2>&1; then
  fail "draft Release was accepted for channel reconciliation"
fi
grep -Fq 'not an immutable published Release' "$test_dir/draft.out" ||
  fail "draft Release failed without the expected state error"

if TEST_RELEASE_STATE=mutable run_check >"$test_dir/mutable.out" 2>&1; then
  fail "mutable Release was accepted for channel reconciliation"
fi
grep -Fq 'immutability is disabled' "$test_dir/mutable.out" ||
  fail "mutable Release failed without the expected state error"

echo 'release image verification tests passed'
