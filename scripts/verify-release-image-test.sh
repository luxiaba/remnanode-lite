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
    [[ "$*" == *"${IMAGE}@${TEST_RELEASE_INDEX_DIGEST}"* ]]
    printf '%s\n' '{"SPDX":{"SPDXID":"SPDXRef-DOCUMENT","spdxVersion":"SPDX-2.3","dataLicense":"CC0-1.0","documentNamespace":"https://example.test/sbom","creationInfo":{"creators":["Tool: test"]}}}'
    ;;
  *'imagetools inspect --format'*)
    [[ "$*" != *":2.8.0" ]] || {
      echo 'reconciliation inspected the exact tag before restoring it' >&2
      exit 1
    }
    printf '%s\n' "$TEST_DIGEST"
    ;;
  *'imagetools inspect --raw'*)
    [[ "$*" == *"${IMAGE}@${TEST_RELEASE_INDEX_DIGEST}"* ]]
    printf '{"schemaVersion":2}\n'
    ;;
  *) exit 2 ;;
esac
EOF

cat >"$test_dir/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[ "$1" = run ]
[ "$2" = ./cmd/release-tool ]
case "$3" in
  verify-index)
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
    [ "$digest" = "$TEST_RELEASE_INDEX_DIGEST" ]
    ;;
  verify-release-index)
    shift 3
    file=""
    tag=""
    image=""
    source_revision=""
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --file) file=$2; shift 2 ;;
        --tag) tag=$2; shift 2 ;;
        --image) image=$2; shift 2 ;;
        --source-revision) source_revision=$2; shift 2 ;;
        *) exit 2 ;;
      esac
    done
    [ "${TEST_RELEASE_INDEX_STATE:-valid}" = valid ] || exit 1
    [[ "$file" == */release-index.json ]]
    [ "$tag" = v2.8.0 ]
    [ "$image" = "$IMAGE" ]
    [ "$source_revision" = "$TEST_COMMIT" ]
    printf 'source_revision=%s\nindex_digest=%s\n' "$TEST_COMMIT" "$TEST_RELEASE_INDEX_DIGEST"
    ;;
  *) exit 2 ;;
esac
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
    case "$2" in
      verify)
        [ "$3" = v2.8.0 ]
        [ "$4" = --repo ]
        [ "$5" = "$GITHUB_REPOSITORY" ]
        ;;
      verify-asset)
        [ "$3" = v2.8.0 ]
        [[ "$4" == */release-index.json ]]
        [ "$5" = --repo ]
        [ "$6" = "$GITHUB_REPOSITORY" ]
        ;;
      download)
        [ "$3" = v2.8.0 ]
        directory=""
        pattern=""
        while [ "$#" -gt 0 ]; do
          case "$1" in
            --dir) directory=$2; shift 2 ;;
            --pattern) pattern=$2; shift 2 ;;
            *) shift ;;
          esac
        done
        [ -n "$directory" ]
        [ "$pattern" = release-index.json ]
        mkdir -p "$directory"
        printf '%s\n' '{"schema_version":1}' >"$directory/release-index.json"
        ;;
      *) exit 2 ;;
    esac
    ;;
  attestation)
    [ "$2" = verify ]
    subject=$3
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
    [ "$source_digest" = "$TEST_COMMIT" ]
    [ "$predicate_type" = https://slsa.dev/provenance/v1 ]
    [ "$deny_self_hosted" = true ]
    case "$subject" in
      "oci://${IMAGE}@${TEST_RELEASE_INDEX_DIGEST}"|*/release-index.json) ;;
      *) exit 2 ;;
    esac
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
    TEST_RELEASE_INDEX_DIGEST="${TEST_RELEASE_INDEX_DIGEST:-$digest}" \
    bash "$root_dir/scripts/verify-release-image.sh" v2.8.0 2.8.0 false
}

output="$(run_check 2>"$test_dir/published.log")"
expected="commit=$commit
digest=$digest"
[ "$output" = "$expected" ] || fail "published Release output was $output"

release_digest="sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
output="$(TEST_RELEASE_INDEX_DIGEST="$release_digest" run_check 2>"$test_dir/release-index.log")"
expected="commit=$commit
digest=$release_digest"
[ "$output" = "$expected" ] || fail "reconciliation did not use the Release index digest"
if TEST_RELEASE_INDEX_STATE=invalid run_check >"$test_dir/invalid-index.out" 2>&1; then
  fail "invalid Release index was accepted for reconciliation"
fi

if TEST_RELEASE_STATE=draft run_check >"$test_dir/draft.out" 2>&1; then
  fail "draft Release was accepted for channel reconciliation"
fi
grep -Fq 'not an immutable published Release' "$test_dir/draft.out" ||
  fail "draft Release failed without the expected state error"

if TEST_RELEASE_STATE=mutable run_check >"$test_dir/mutable.out" 2>&1; then
  fail "mutable Release was accepted for channel reconciliation"
fi
grep -Fq 'not an immutable published Release' "$test_dir/mutable.out" ||
  fail "mutable Release failed without the expected state error"

echo 'release image verification tests passed'
