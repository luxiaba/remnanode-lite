#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

fail() {
  echo "supply-chain check: $*" >&2
  exit 1
}

require_text() {
  local file="$1" text="$2"
  grep -Fq -- "$text" "$file" || fail "$file is missing: $text"
}

for command in go git; do
  command -v "$command" >/dev/null 2>&1 || fail "required command is missing: $command"
done

literal_dollar='$'

version="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' internal/version/version.go)"
contract_version="$(tr -d ' \n\r' < internal/version/contract.version)"
[ -n "$version" ] && [ -n "$contract_version" ] || fail "version metadata is incomplete"
[ "$(go run ./cmd/remnanode-lite version)" = \
  "remnanode-lite ${version} (contract ${contract_version})" ] ||
  fail "compiled version output does not match source metadata"

go run ./cmd/release-tool validate --lock release/runtime-assets.lock.json >/dev/null
go run ./cmd/release-tool help | grep -Eq '^[[:space:]]+materialize[[:space:]]' ||
  fail "release-tool does not expose the materialize command"

stable_metadata="$(bash scripts/release-metadata.sh v2.8.0)"
grep -Fxq 'version=2.8.0' <<<"$stable_metadata"
grep -Fxq 'channel=latest' <<<"$stable_metadata"
grep -Fxq 'prerelease=false' <<<"$stable_metadata"
grep -Fxq 'make_latest=true' <<<"$stable_metadata"
preview_metadata="$(bash scripts/release-metadata.sh v2.8.1-rnl.9)"
grep -Fxq 'version=2.8.1-rnl.9' <<<"$preview_metadata"
grep -Fxq 'channel=preview' <<<"$preview_metadata"
grep -Fxq 'prerelease=true' <<<"$preview_metadata"
grep -Fxq 'make_latest=false' <<<"$preview_metadata"
for invalid_tag in v2.8.0-rnl.0 v02.8.0 v2.8 main '../../etc'; do
  if bash scripts/release-metadata.sh "$invalid_tag" >/dev/null 2>&1; then
    fail "invalid release tag was accepted: $invalid_tag"
  fi
done

if grep -Eq '"url"[[:space:]]*:[[:space:]]*"[^"]*(/latest|refs/heads/|/main([/?"]|$))' \
  release/runtime-assets.lock.json; then
  fail "runtime asset lock contains a floating source URL"
fi
require_text Dockerfile 'COPY release/runtime-assets.lock.json /runtime-assets.lock.json'
require_text Dockerfile 'release-tool materialize'
require_text Dockerfile '--lock /runtime-assets.lock.json'
require_text Dockerfile "--arch \"${literal_dollar}TARGETARCH\""
require_text Dockerfile '--out-dir /assets'
for duplicated_pin in \
  'ARG XRAY_CORE_VERSION=' \
  'ARG XRAY_AMD64_SHA256=' \
  'ARG XRAY_ARM64_SHA256=' \
  'ARG ASN_SOURCE_URL=' \
  'ARG ASN_SOURCE_SHA256=' \
  'github.com/XTLS/Xray-core/releases/download' \
  'github.com/ipverse/as-ip-blocks/archive'; do
  if grep -Fq "$duplicated_pin" Dockerfile; then
    fail "Dockerfile duplicates runtime lock data: $duplicated_pin"
  fi
done

for workflow in \
  .github/workflows/ci.yml \
  .github/workflows/release.yml \
  .github/workflows/security.yml; do
  require_text "$workflow" 'run: bash scripts/install-ci-checks.sh'
done

dependabot_config=.github/dependabot.yml
[ -f "$dependabot_config" ] || fail "Dependabot configuration is missing"
for ecosystem in gomod github-actions docker; do
  grep -Eq "^[[:space:]]*- package-ecosystem: ${ecosystem}$" "$dependabot_config" ||
    fail "Dependabot does not maintain ${ecosystem} dependencies"
done
for expected_count in \
  'target-branch: dev' \
  'interval: weekly' \
  'open-pull-requests-limit: 2'; do
  [ "$(grep -Ec "^[[:space:]]+${expected_count}$" "$dependabot_config")" -eq 3 ] ||
    fail "every Dependabot ecosystem must use ${expected_count}"
done

require_text .github/workflows/ci.yml 'branches: [dev, main]'
for job in go repository native netadmin gate; do
  grep -Eq "^  ${job}:$" .github/workflows/ci.yml || fail "CI workflow is missing job: $job"
done
require_text .github/workflows/ci.yml 'run: bash scripts/check-go.sh'
require_text .github/workflows/ci.yml 'run: bash scripts/check-repository.sh'
require_text .github/workflows/ci.yml 'run: sh release/native/install_test.sh'

for expected in \
  'issues: write' \
  'gh api repos/remnawave/node/releases/latest' \
  'gh issue create'; do
  require_text .github/workflows/contract-sync.yml "$expected"
done
for oracle_caller in \
  .github/workflows/ci.yml \
  .github/workflows/contract-sync.yml \
  scripts/release-check.sh; do
  require_text "$oracle_caller" 'go run ./cmd/contract-source-check'
done

if grep -R -Fq 'runs-on: ubuntu-latest' .github/workflows; then
  fail "GitHub workflows must use an explicit Ubuntu runner release"
fi
while IFS= read -r action_ref; do
  case "$action_ref" in
    ./*) continue ;;
  esac
  revision="${action_ref##*@}"
  [[ "$revision" =~ ^[0-9a-f]{40}$ ]] ||
    fail "GitHub Action is not pinned to a full commit: $action_ref"
done < <(
  sed -En \
    's/^[[:space:]]*(-[[:space:]]*)?uses:[[:space:]]*([^[:space:]#]+).*$/\2/p' \
    .github/workflows/*.yml .github/workflows/*.yaml 2>/dev/null
)

require_text .github/workflows/container.yml '      - "release/runtime-assets.lock.json"'
require_text .github/workflows/container.yml 'platforms: linux/amd64,linux/arm64'
require_text .github/workflows/container.yml 'push-by-digest=true,name-canonical=true,push=true'
require_text .github/workflows/container.yml 'provenance: mode=max'
require_text .github/workflows/container.yml 'attest-main:'
require_text .github/workflows/container.yml 'push-to-registry: true'
require_text .github/workflows/container.yml 'artifact-metadata: write'
require_text .github/workflows/container.yml 'actions/attest@'

release_workflow=.github/workflows/release.yml
require_text "$release_workflow" \
  "bash scripts/release-metadata.sh \"${literal_dollar}GITHUB_REF_NAME\""
require_text "$release_workflow" 'bash scripts/build-native-bundle.sh dist/native amd64 arm64'
require_text "$release_workflow" 'release-tool verify'
require_text "$release_workflow" 'dist/install.sh'
require_text "$release_workflow" \
  "remnanode-lite_${literal_dollar}{RELEASE_VERSION}_linux_${literal_dollar}{arch}.tar.gz"
require_text "$release_workflow" 'sha256sum --check --strict SHA256SUMS'
require_text "$release_workflow" 'Attest every release asset'
require_text "$release_workflow" 'actions/attest@'
require_text "$release_workflow" 'subject-path: dist/*'
require_text "$release_workflow" 'Verify release asset provenance'
require_text "$release_workflow" 'for asset in dist/*; do'
require_text "$release_workflow" 'draft: true'
require_text "$release_workflow" 'Create draft release and upload every asset'
require_text "$release_workflow" 'Verify the complete draft'
require_text "$release_workflow" '.[] | [.name, .digest, (.size | tostring)]'
require_text "$release_workflow" 'Publish the verified draft'
require_text "$release_workflow" '-F draft=false'
require_text "$release_workflow" "-F prerelease=\"${literal_dollar}PRERELEASE\""
require_text "$release_workflow" "-f make_latest=\"${literal_dollar}MAKE_LATEST\""
require_text "$release_workflow" 'Publish the exact release image tag'
require_text "$release_workflow" 'Promote the published release channel without rebuilding'
require_text "$release_workflow" 'needs: [prepare-release, publish-container, finalize-release]'
require_text "$release_workflow" 'workflow_dispatch:'
require_text "$release_workflow" 'reconcile-channel:'
require_text "$release_workflow" "if: github.event_name == 'workflow_dispatch'"
require_text "$release_workflow" 'Reconcile the published release channel'
require_text "$release_workflow" 'ref: refs/heads/main'
require_text "$release_workflow" 'waiting for attested main candidate'
require_text "$release_workflow" 'scripts/promote-image-tag.sh immutable'
require_text "$release_workflow" 'scripts/promote-image-tag.sh mutable'
require_text "$release_workflow" \
  "candidate_tag=\"${literal_dollar}{REGISTRY}/${literal_dollar}{IMAGE_NAME}:sha-${literal_dollar}{candidate_commit}\""
require_text "$release_workflow" '--cert-identity'
require_text "$release_workflow" '/.github/workflows/container.yml@refs/heads/main'
if grep -Fq 'docker/build-push-action@' "$release_workflow"; then
  fail "release workflow must promote the accepted candidate digest instead of rebuilding"
fi
if grep -Eq 'remnawave-node\.(service|openrc)|scripts/(install-node(-alpine)?|install-xray|upgrade|uninstall)\.sh' \
  Dockerfile .github/workflows/*.yml scripts/check-version.sh; then
  fail "current release contracts still reference the legacy Native installer"
fi
if grep -Eq 'ASN_SOURCE_(URL|SHA256)|XRAY_(AMD64|ARM64)_SHA256' "$release_workflow"; then
  fail "release workflow duplicates runtime asset lock data"
fi

require_text scripts/install-ci-checks.sh 'readonly SHELLCHECK_VERSION=0.11.0'
require_text scripts/install-ci-checks.sh \
  'readonly SHELLCHECK_LINUX_X86_64_SHA256=8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198'
require_text scripts/install-ci-checks.sh \
  'readonly SHELLCHECK_LINUX_AARCH64_SHA256=12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588'

echo "supply-chain checks passed"
