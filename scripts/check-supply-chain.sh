#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

fail() {
  echo "supply-chain check: $*" >&2
  exit 1
}

literal_dollar='$'

require_text() {
  local file=$1 text=$2
  grep -Fq -- "$text" "$file" || fail "$file is missing: $text"
}

require_order() {
  local file=$1 first=$2 second=$3 first_line second_line
  first_line="$(grep -nF -- "$first" "$file" | head -1 | cut -d: -f1)"
  second_line="$(grep -nF -- "$second" "$file" | head -1 | cut -d: -f1)"
  [ -n "$first_line" ] && [ -n "$second_line" ] && [ "$first_line" -lt "$second_line" ] ||
    fail "$file must place '$first' before '$second'"
}

version="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' internal/version/version.go)"
contract_version="$(tr -d ' \n\r' <internal/version/contract.version)"
[ -n "$version" ] && [ -n "$contract_version" ] || fail "version metadata is incomplete"
[ "$(go run ./cmd/remnanode-lite version)" = \
  "remnanode-lite ${version} (contract ${contract_version})" ] ||
  fail "compiled version output does not match source metadata"

release_tool_help="$(go run ./cmd/release-tool help)"
for command in metadata validate materialize build verify assemble finalize-release verify-package verify-release-index verify-index verify-release; do
  grep -Eq "^[[:space:]]+${command}[[:space:]]" <<<"$release_tool_help" ||
    fail "release-tool does not expose ${command}"
done
go run ./cmd/release-tool validate --lock release/runtime-assets.lock.json >/dev/null

if grep -Eq '"url"[[:space:]]*:[[:space:]]*"[^"]*(/latest|refs/heads/|/main([/?"]|$))' \
  release/runtime-assets.lock.json; then
  fail "runtime asset lock contains a floating source URL"
fi
for required in \
  'COPY release/runtime-assets.lock.json /runtime-assets.lock.json' \
  'release-tool materialize' \
  '--lock /runtime-assets.lock.json' \
  '--out-dir /assets'; do
  require_text Dockerfile "$required"
done
if grep -Eq 'ARG (XRAY_(CORE_VERSION|AMD64_SHA256|ARM64_SHA256)|ASN_SOURCE_(URL|SHA256))=' Dockerfile; then
  fail "Dockerfile duplicates the runtime asset lock"
fi

dependabot=.github/dependabot.yml
for ecosystem in gomod github-actions docker; do
  grep -Eq "^[[:space:]]*- package-ecosystem: ${ecosystem}$" "$dependabot" ||
    fail "Dependabot does not maintain ${ecosystem} dependencies"
done

if grep -R -Fq 'runs-on: ubuntu-latest' .github/workflows; then
  fail "workflows must use an explicit Ubuntu runner release"
fi
if grep -R -Eq '^[[:space:]]+pull_request_target:|^[[:space:]]+workflow_run:' .github/workflows; then
  fail "privileged pull_request_target/workflow_run triggers are not allowed"
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

candidate=.github/workflows/container.yml
for required in \
  'name: candidate' \
  'platforms: linux/amd64,linux/arm64' \
  'push-by-digest=true,name-canonical=true,push=true' \
  'provenance: mode=max' \
  'sbom: generator=docker.io/docker/buildkit-syft-scanner:stable-1@sha256:79e7b013cbec16bbb436f312819a49a4a57752b2270c1a9332ae1a10fcc82a68' \
  'push-to-registry: true' \
  'release-tool assemble' \
  'release-tool finalize-release' \
  'release-tool verify-package' \
  '--require-release-index' \
  'Attest every final Release asset' \
  "native-bundles-${literal_dollar}{{ github.sha }}" \
  'actions/upload-artifact@' \
  "release-assets-${literal_dollar}{{ github.sha }}" \
  'scripts/promote-image-tag.sh immutable' \
  'scripts/promote-candidate-edge.sh' \
  'needs: [image, native]' \
  'needs: [image, release-assets]'; do
  require_text "$candidate" "$required"
done
require_order "$candidate" 'name: Bind the accepted OCI index to the Release package' 'name: Attest every final Release asset'
require_text scripts/verify-candidate-image.sh 'index .SBOM'
require_text scripts/verify-candidate-image.sh '--predicate-type https://slsa.dev/provenance/v1'
if grep -Eq '(^|[^[:alnum:]_-])latest([^[:alnum:]_.-]|$)' "$candidate"; then
  fail "candidate workflow must not publish the stable latest channel"
fi

release=.github/workflows/release.yml
for required in \
  'actions: read' \
  'scripts/find-workflow-run.sh ci.yml' \
  'scripts/find-workflow-run.sh container.yml' \
  'scripts/require-current-main.sh' \
  'scripts/check-release-metadata.sh' \
  'actions/download-artifact@' \
  'release-tool verify-package' \
  'release-tool verify-release-index' \
  '--require-release-index' \
  'release-index.json' \
  'scripts/verify-release-asset-attestations.sh' \
  'scripts/verify-candidate-image.sh' \
  'draft: true' \
  'scripts/verify-draft-release.sh' \
  'scripts/verify-published-release.sh' \
  'scripts/promote-image-tag.sh immutable' \
  'scripts/promote-image-tag.sh mutable'; do
  require_text "$release" "$required"
done
require_order "$release" 'name: Publish the draft Release' 'name: Verify the immutable Release and every asset'
require_order "$release" 'name: Promote the exact image tag before publication' 'name: Publish the draft Release'
require_order "$release" 'name: Verify the immutable Release and every asset' 'name: Confirm the exact image tag after Release verification'
require_order "$release" 'name: Confirm the exact image tag after Release verification' 'name: Promote the published channel without rebuilding'
publish_line="$(grep -nF 'name: Publish the draft Release' "$release" | cut -d: -f1)"
if tail -n "+$publish_line" "$release" | grep -Fq 'scripts/require-current-main.sh'; then
  fail "release workflow must not rebind a published Release to a newer main commit"
fi
if grep -Fq 'docker/build-push-action@' "$release" ||
  grep -Fq 'scripts/build-native-bundle.sh' "$release"; then
  fail "release workflow must promote the accepted candidate without rebuilding"
fi
if grep -Eq '/git/(tags|refs)|Create annotated release tag|^[[:space:]]+tags:' "$release"; then
  fail "release tags must be created when GitHub publishes the draft, not by Git refs or tag pushes"
fi
for script in \
  scripts/require-current-main.sh \
  scripts/verify-release-asset-attestations.sh \
  scripts/verify-candidate-image.sh \
  scripts/verify-release-image.sh \
  scripts/verify-release-image-test.sh \
  scripts/verify-published-release-test.sh \
  scripts/promote-image-tag-test.sh \
  scripts/require-channel-owner-test.sh \
  scripts/require-channel-owner.sh \
  scripts/release-state.sh \
  scripts/verify-draft-release.sh \
  scripts/verify-published-release.sh \
  scripts/promote-candidate-edge.sh \
  scripts/test-native-release-bundles.sh; do
  [ -x "$script" ] || fail "release helper is not executable: $script"
done
require_text scripts/verify-candidate-image.sh 'release-tool verify-index'
require_text scripts/verify-candidate-image.sh 'gh attestation verify'
require_text scripts/verify-candidate-image.sh 'state=absent'
require_text scripts/verify-candidate-image.sh 'state=present'
require_text scripts/release-state.sh 'published-pending-immutability'
require_text scripts/verify-draft-release.sh 'release-tool verify-release'
require_text scripts/verify-draft-release.sh 'verify-release-tag.sh --require-missing'
require_text scripts/verify-published-release.sh '--immutable=any'
require_text scripts/verify-published-release.sh '.immutable == true'
require_text scripts/verify-published-release.sh 'scripts/verify-release-tag.sh'
require_text scripts/verify-published-release.sh 'gh release verify'
require_text scripts/verify-published-release.sh 'gh release verify-asset'
require_text scripts/verify-published-release.sh 'scripts/verify-release-latest.sh'
require_text scripts/verify-release-image.sh 'scripts/verify-candidate-image.sh'
require_text scripts/verify-release-image.sh 'scripts/release-state.sh'
require_text scripts/verify-release-image.sh 'gh release download'
require_text scripts/verify-release-image.sh 'gh release verify-asset'
require_text scripts/verify-release-image.sh 'release-index.json'
require_text scripts/verify-release-image.sh 'release-tool verify-release-index'
require_text scripts/verify-release-image.sh "--digest \"${literal_dollar}index_digest\""
require_text scripts/require-channel-owner.sh 'scripts/verify-release-latest.sh'
require_text scripts/require-channel-owner.sh 'promote=false'

reconcile=.github/workflows/reconcile.yml
for required in \
  'name: reconcile-release' \
  'scripts/verify-release-image.sh' \
  'scripts/promote-image-tag.sh immutable' \
  'scripts/require-channel-owner.sh' \
  'scripts/promote-image-tag.sh mutable'; do
  require_text "$reconcile" "$required"
done
require_order "$reconcile" 'name: Verify the immutable Release and its candidate' 'name: Restore the immutable exact image tag'
require_order "$reconcile" 'name: Restore the immutable exact image tag' 'name: Restore the moving channel'
require_text "$reconcile" "if: steps.channel-owner.outputs.promote == 'true'"
require_text "$reconcile" "if: steps.channel-owner.outputs.promote == 'false'"

require_text scripts/install-ci-checks.sh 'readonly SHELLCHECK_VERSION=0.11.0'
require_text scripts/install-ci-checks.sh \
  'readonly SHELLCHECK_LINUX_X86_64_SHA256=8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198'
require_text scripts/install-ci-checks.sh \
  'readonly SHELLCHECK_LINUX_AARCH64_SHA256=12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588'

echo "supply-chain checks passed"
