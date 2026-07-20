#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

require_file() {
  [ -f "$1" ] || {
    echo "required Docker packaging file is missing: $1" >&2
    exit 1
  }
}

require_text() {
  local file="$1" text="$2"
  grep -Fq -- "$text" "$file" || {
    echo "$file is missing required Docker packaging text: $text" >&2
    exit 1
  }
}

for file in \
  Dockerfile \
  compose.yaml \
  compose.build.yaml \
  .dockerignore \
  .env.example \
  .github/workflows/container.yml \
  .github/workflows/release.yml \
  scripts/check-release-risk-disclosure.sh \
  scripts/promote-image-tag.sh \
  docs/deployment-docker.md \
  deploy/compose.single-file.yaml; do
  require_file "$file"
done

if grep -Eqi '(^|[/:_-])latest([[:space:]/:@_-]|$)' Dockerfile; then
  echo "Dockerfile must not use floating latest assets or base images" >&2
  exit 1
fi

require_text Dockerfile '# syntax=docker/dockerfile:1.7.0@sha256:dbbd5e059e8a07ff7ea6233b213b36aa516b4c53c645f1817a4dd18b83cbea56'
require_text Dockerfile 'ARG GO_IMAGE=golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651'
require_text Dockerfile 'ARG DEBIAN_IMAGE=debian:bookworm-slim@sha256:7b140f374b289a7c2befc338f42ebe6441b7ea838a042bbd5acbfca6ec875818'
require_text Dockerfile 'ARG XRAY_CORE_VERSION=v26.6.27'
require_text Dockerfile 'Xray-linux-64.zip'
require_text Dockerfile 'Xray-linux-arm64-v8a.zip'
require_text Dockerfile 'b3e5902d06d6282fe53cfa2fc426058b9aeaa429b2c812e20887cd47f26d08bf'
require_text Dockerfile '13a251379bea366c2cf10363ad71e75734193d401f26f518bf0c25e5c8f8c931'
require_text Dockerfile 'https://github.com/ipverse/as-ip-blocks/archive/56d021c7536afb15317155e45b57e7b5c87a4700.tar.gz'
require_text Dockerfile 'fc8be15bfbef3134f603276a26364935dbd2543d099dbaafa978a33b674a58ec'
require_text Dockerfile 'asn-builder -format ipverse-tar-gz'
require_text Dockerfile 'ENV XRAY_CORE_VERSION=v26.6.27'
require_text Dockerfile 'ENTRYPOINT ["/usr/local/bin/remnanode-lite"]'

version="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' internal/version/version.go)"
[ -n "$version" ] || {
  echo "application version is missing" >&2
  exit 1
}
production_image="ghcr.io/luxiaba/remnanode-lite:${version}"
require_text compose.yaml "$production_image"
require_text .env.example "REMNANODE_IMAGE=${production_image}"
require_text compose.build.yaml "image: remnanode-lite:${version}"
require_text compose.build.yaml 'build:'

if grep -Eq '^[[:space:]]+build:' compose.yaml; then
  echo "production compose.yaml must not require a source build" >&2
  exit 1
fi

require_text compose.yaml 'network_mode: host'
require_text compose.yaml 'init: true'
require_text compose.yaml 'NET_ADMIN'
require_text compose.yaml 'NET_BIND_SERVICE'
require_text compose.yaml 'mem_limit: 448m'
require_text compose.yaml 'memswap_limit: 448m'
require_text compose.yaml 'cpus: 1.0'
require_text compose.yaml 'pids_limit: 256'
require_text compose.yaml 'read_only: true'
require_text compose.yaml '["CMD", "/usr/local/bin/remnanode-lite", "healthcheck"]'

require_text deploy/compose.single-file.yaml 'image: ghcr.io/luxiaba/remnanode-lite:latest'
require_text deploy/compose.single-file.yaml 'SECRET_KEY: "REPLACE_WITH_THE_COMPLETE_PANEL_SECRET_KEY"'
require_text deploy/compose.single-file.yaml 'network_mode: host'
require_text deploy/compose.single-file.yaml 'init: true'
require_text deploy/compose.single-file.yaml 'read_only: true'
require_text deploy/compose.single-file.yaml 'mem_limit: 448m'
release_single_file="$(sed \
  "s|ghcr.io/luxiaba/remnanode-lite:latest|ghcr.io/luxiaba/remnanode-lite:${version}|" \
  deploy/compose.single-file.yaml)"
grep -Fq "image: ghcr.io/luxiaba/remnanode-lite:${version}" <<<"$release_single_file"
if grep -Fq 'ghcr.io/luxiaba/remnanode-lite:latest' <<<"$release_single_file"; then
  echo "release single-file Compose still contains latest" >&2
  exit 1
fi
if grep -Eq '^[[:space:]]*-[[:space:]]*SECRET_KEY=' deploy/compose.single-file.yaml; then
  echo "single-file Compose must use a mapping for SECRET_KEY" >&2
  exit 1
fi
if grep -Eq '^[[:space:]]+volumes:' deploy/compose.single-file.yaml; then
  echo "single-file Compose must keep runtime logs ephemeral" >&2
  exit 1
fi

require_text .github/workflows/release.yml 'packages: write'
require_text .github/workflows/release.yml 'attestations: read'
github_expression_prefix='$'
require_text .github/workflows/release.yml \
  "if [ \"${github_expression_prefix}GITHUB_SHA\" != \"${github_expression_prefix}main_head\" ]; then"
require_text .github/workflows/release.yml 'needs: release'
require_text .github/workflows/release.yml 'needs: publish-container'
require_text .github/workflows/release.yml 'Verify accepted candidate image'
require_text .github/workflows/release.yml 'candidateImageDigest // empty'
require_text .github/workflows/release.yml '--cert-identity'
require_text .github/workflows/release.yml '/.github/workflows/container.yml@refs/heads/main'
require_text .github/workflows/release.yml '--source-digest'
require_text .github/workflows/release.yml '--deny-self-hosted-runners'
require_text .github/workflows/release.yml 'docker buildx imagetools inspect'
require_text .github/workflows/release.yml 'docker buildx imagetools inspect --raw'
require_text .github/workflows/release.yml 'vnd.docker.reference.type'
require_text .github/workflows/release.yml 'vnd.docker.reference.digest'
jq_index="\$index"
jq_runnable="\$runnable"
jq_attestations="\$attestations"
require_text .github/workflows/release.yml "(${jq_index}.manifests | length) == 4"
require_text .github/workflows/release.yml "(${jq_runnable} | length) == 2"
require_text .github/workflows/release.yml "(${jq_attestations} | length) == 2"
require_text .github/workflows/release.yml '["linux/amd64", "linux/arm64"]'
if grep -Fq -- '--signer-workflow' .github/workflows/release.yml ||
  grep -Fq -- '--source-ref' .github/workflows/release.yml; then
  echo "release workflow must use an exact attestation certificate identity" >&2
  exit 1
fi
require_text scripts/release-check.sh 'scripts/check-release-risk-disclosure.sh'
require_text scripts/check-release-risk-disclosure.sh '## Known Risks'
require_text scripts/check-release-risk-disclosure.sh 'operator-attested'
require_text scripts/check-release-risk-disclosure.sh 'not an unforgeable proof'
for deferred in \
  arm64-production-runtime \
  native-systemd-install \
  native-openrc-install \
  50000-user-load \
  24h-soak \
  fault-and-rollback-injection; do
  require_text scripts/check-release-risk-disclosure.sh "$deferred"
done

risk_fixture_dir="$(mktemp -d)"
trap 'rm -rf "$risk_fixture_dir"' EXIT
valid_risks="${risk_fixture_dir}/valid.md"
invalid_deferred="${risk_fixture_dir}/invalid-deferred.md"
invalid_operator="${risk_fixture_dir}/invalid-operator.md"
{
  printf '%s\n\n' '# v2.8.0' '## Known Risks'
  for deferred in \
    arm64-production-runtime \
    native-systemd-install \
    native-openrc-install \
    50000-user-load \
    24h-soak \
    fault-and-rollback-injection; do
    printf -- "- \`%s\`: deferred; not validated by \`docker-production-smoke-v1\`.\n" "$deferred"
  done
  printf '\n%s\n\n%s\n' \
    'Runtime evidence is operator-attested and is not an unforgeable proof.' \
    '## Installation and Upgrade'
} >"$valid_risks"
bash scripts/check-release-risk-disclosure.sh "$valid_risks"
sed 's/: deferred;/: not deferred;/' "$valid_risks" >"$invalid_deferred"
if bash scripts/check-release-risk-disclosure.sh "$invalid_deferred" >/dev/null 2>&1; then
  echo "release risk checker accepted a negated deferred disclosure" >&2
  exit 1
fi
sed 's/Runtime evidence is operator-attested/Runtime evidence is not operator-attested/' \
  "$valid_risks" >"$invalid_operator"
if bash scripts/check-release-risk-disclosure.sh "$invalid_operator" >/dev/null 2>&1; then
  echo "release risk checker accepted a negated operator evidence statement" >&2
  exit 1
fi
require_text .github/workflows/release.yml \
  "SOURCE_DIGEST: ${github_expression_prefix}{{ needs.release.outputs.candidate_digest }}"
require_text .github/workflows/release.yml 'make_latest: false'
require_text .github/workflows/release.yml 'overwrite_files: false'
require_text .github/workflows/release.yml 'prerelease: false'
require_text .github/workflows/release.yml 'promote-latest:'
require_text .github/workflows/release.yml 'Publish accepted candidate as the exact release version'
require_text .github/workflows/release.yml 'Promote attested image to GHCR latest'
require_text .github/workflows/release.yml 'Mark GitHub release as latest'
require_text .github/workflows/release.yml 'Revalidate main head before latest promotion'
require_text .github/workflows/release.yml 'scripts/promote-image-tag.sh immutable'
require_text .github/workflows/release.yml 'scripts/promote-image-tag.sh mutable'
require_text .github/workflows/release.yml '-f make_latest=true'
awk '
  /^  promote-latest:$/ { in_job = 1; next }
  in_job && /^  [A-Za-z0-9_-]+:$/ { exit }
  in_job && /^      contents: write$/ { found = 1 }
  END { exit(found ? 0 : 1) }
' .github/workflows/release.yml || {
  echo "release latest promotion requires contents: write" >&2
  exit 1
}
require_text .github/workflows/release.yml 'image=moby/buildkit:v0.31.1@sha256:6b59b7df63a8cb9902736f9ddf7fcff8261613d3e7449b8ea8b7537fc399c03a'
require_text .github/workflows/release.yml 'dist/compose.yaml'
require_text .github/workflows/release.yml 'dist/docker-compose.single-file.yaml'
require_text .github/workflows/release.yml \
  "release_version=\"${github_expression_prefix}{GITHUB_REF_NAME#v}\""
require_text .github/workflows/release.yml 'dist/remnanode.env.example'
require_text .github/workflows/container.yml 'outputs: type=cacheonly'
require_text .github/workflows/container.yml 'push: false'
require_text .github/workflows/container.yml 'workflow_dispatch:'
require_text .github/workflows/container.yml 'branches: [dev, main]'
require_text .github/workflows/container.yml '      - ".env.example"'
require_text .github/workflows/container.yml '      - "compose.yaml"'
if [ "$(grep -Fc '      - "deploy/compose.single-file.yaml"' .github/workflows/container.yml)" -ne 2 ]; then
  echo "container workflow must track the production single-file Compose on push and pull requests" >&2
  exit 1
fi
if [ "$(grep -Fc '      - "scripts/promote-image-tag.sh"' .github/workflows/container.yml)" -ne 2 ]; then
  echo "container workflow must track its image promotion helper on push and pull requests" >&2
  exit 1
fi
require_text .github/workflows/container.yml "cancel-in-progress: \${{ github.event_name != 'workflow_dispatch' }}"
require_text .github/workflows/container.yml "if: github.event_name == 'pull_request' || (github.event_name == 'push' && github.ref == 'refs/heads/dev')"
require_text .github/workflows/container.yml "if: github.ref == 'refs/heads/main' && (github.event_name == 'push' || github.event_name == 'workflow_dispatch')"
require_text .github/workflows/container.yml "if: github.event_name == 'workflow_dispatch' && github.ref != 'refs/heads/main'"
require_text .github/workflows/container.yml 'Reject non-main candidate dispatch'
require_text .github/workflows/container.yml 'candidate publishing is only allowed from refs/heads/main'
require_text .github/workflows/container.yml 'Build and publish untagged main image'
require_text .github/workflows/container.yml 'Read project version'
require_text .github/workflows/container.yml \
  "org.opencontainers.image.version=${github_expression_prefix}{{ steps.project-version.outputs.version }}"
if grep -Fq "org.opencontainers.image.version=sha-${github_expression_prefix}{{ github.sha }}" \
  .github/workflows/container.yml; then
  echo "candidate OCI version must use the project version, not a commit tag alias" >&2
  exit 1
fi
require_text .github/workflows/container.yml 'push-by-digest=true,name-canonical=true,push=true'
require_text .github/workflows/container.yml 'promote-main:'
require_text .github/workflows/container.yml 'needs: [publish-main, attest-main]'
require_text .github/workflows/container.yml 'scripts/promote-image-tag.sh immutable'
require_text .github/workflows/container.yml 'scripts/promote-image-tag.sh mutable'
require_text .github/workflows/container.yml \
  "tag=\"candidate-sha-${github_expression_prefix}{GITHUB_SHA}\""
require_text .github/workflows/container.yml \
  "tag=\"sha-${github_expression_prefix}{GITHUB_SHA}\""
require_text .github/workflows/container.yml "current main is ${github_expression_prefix}main_head"
sbom_generator='generator=docker.io/docker/buildkit-syft-scanner:stable-1@sha256:79e7b013cbec16bbb436f312819a49a4a57752b2270c1a9332ae1a10fcc82a68'
require_text .github/workflows/container.yml "sbom: ${sbom_generator}"
require_text .github/workflows/container.yml 'packages: write'
require_text .github/workflows/container.yml 'attestations: write'
require_text .github/workflows/container.yml 'provenance: mode=max'
require_text .github/workflows/container.yml 'push-to-registry: true'
require_text .github/workflows/container.yml 'attest-main:'
require_text .github/workflows/container.yml 'needs: publish-main'
require_text .github/workflows/container.yml \
  "digest: ${github_expression_prefix}{{ steps.image.outputs.digest }}"
require_text .github/workflows/container.yml \
  "SOURCE_DIGEST: ${github_expression_prefix}{{ needs.publish-main.outputs.digest }}"
require_text .github/workflows/container.yml "subject-digest: ${github_expression_prefix}{{ steps.digest.outputs.digest }}"
require_text .github/workflows/container.yml 'image: docker.io/tonistiigi/binfmt:qemu-v10.2.3@sha256:400a4873b838d1b89194d982c45e5fb3cda4593fbfd7e08a02e76b03b21166f0'
require_text .github/workflows/container.yml 'image=moby/buildkit:v0.31.1@sha256:6b59b7df63a8cb9902736f9ddf7fcff8261613d3e7449b8ea8b7537fc399c03a'
for workflow in .github/workflows/container.yml .github/workflows/release.yml; do
  require_text "$workflow" 'group: registry-publish'
  require_text "$workflow" 'cancel-in-progress: false'
done
require_text scripts/promote-image-tag.sh 'refusing to move immutable tag'
require_text scripts/promote-image-tag.sh 'could not determine whether immutable tag'
require_text scripts/promote-image-tag.sh '--prefer-index=false'
require_text scripts/promote-image-tag.sh \
  "resolved to ${github_expression_prefix}promoted, expected ${github_expression_prefix}source_digest"
require_text compose.yaml '/var/log/remnanode:rw,noexec,nosuid,nodev,size=28m,mode=0750'
require_text compose.yaml 'max-size: 2m'
require_text internal/xray/logrotate.go 'maxLogSize       = 4 << 20'

if grep -Fq 'remnanode-logs' compose.yaml; then
  echo "production compose must keep rw-core logs ephemeral" >&2
  exit 1
fi

if grep -Eq 'ghcr\.io/[^[:space:]]+:latest' compose.yaml .env.example; then
  echo "production container configuration must default to an immutable version" >&2
  exit 1
fi

if grep -Eq 'type=raw[^[:space:]]*latest' .github/workflows/container.yml; then
  echo "candidate workflows must not publish latest" >&2
  exit 1
fi

if grep -Fq 'docker/build-push-action@' .github/workflows/release.yml; then
  echo "release workflow must promote the accepted candidate digest instead of rebuilding it" >&2
  exit 1
fi

require_text .github/workflows/release.yml \
  "candidate_ref=\"${github_expression_prefix}{image}@${github_expression_prefix}{candidate_digest}\""
if grep -Fq \
  "candidate_ref=\"${github_expression_prefix}{image}:sha-${github_expression_prefix}{candidate_commit}\"" \
  .github/workflows/release.yml; then
  echo "release workflow must not require one candidate tag alias" >&2
  exit 1
fi

if grep -Eq '^[[:space:]]+needs:[[:space:]]+build[[:space:]]*$' .github/workflows/container.yml; then
  echo "main image publishing must not repeat the CI build job" >&2
  exit 1
fi

while IFS= read -r action_ref; do
  case "$action_ref" in
    ./*) continue ;;
  esac
  revision="${action_ref##*@}"
  if ! [[ "$revision" =~ ^[0-9a-f]{40}$ ]]; then
    echo "GitHub Action is not pinned to a full commit: $action_ref" >&2
    exit 1
  fi
done < <(
  sed -En \
    's/^[[:space:]]*(-[[:space:]]*)?uses:[[:space:]]*([^[:space:]#]+).*$/\2/p' \
    .github/workflows/*.yml .github/workflows/*.yaml 2>/dev/null
)

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  SECRET_KEY=packaging-check REMNANODE_IMAGE="$production_image" \
    docker compose -f compose.yaml config --quiet
  SECRET_KEY=packaging-check REMNANODE_IMAGE="$production_image" \
    docker compose -f compose.yaml -f compose.build.yaml config --quiet
  docker compose -f deploy/compose.single-file.yaml config --quiet
else
  echo "docker compose is unavailable; skipped Compose schema validation" >&2
fi

echo "Docker packaging checks passed"
