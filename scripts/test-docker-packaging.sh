#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

fail() {
  echo "Docker packaging check: $*" >&2
  exit 1
}

require_file() {
  [ -f "$1" ] || fail "required file is missing: $1"
}

require_text() {
  local file="$1" text="$2"
  grep -Fq -- "$text" "$file" || fail "$file is missing: $text"
}

for file in \
  Dockerfile \
  compose.yaml \
  compose.build.yaml \
  .dockerignore \
  .env.example \
  .github/workflows/container.yml \
  .github/workflows/release.yml \
  release/runtime-assets.lock.json \
  release/bundle/THIRD_PARTY_NOTICES.md \
  release/bundle/SOURCE-OFFER.md \
  scripts/promote-image-tag.sh \
  deploy/compose.single-file.yaml; do
  require_file "$file"
done

literal_dollar='$'

if grep -Eqi '(^|[/:_-])latest([[:space:]/:@_-]|$)' Dockerfile; then
  fail "Dockerfile must not use floating latest assets or base images"
fi
require_text Dockerfile '# syntax=docker/dockerfile:1.7.0@sha256:dbbd5e059e8a07ff7ea6233b213b36aa516b4c53c645f1817a4dd18b83cbea56'
require_text Dockerfile 'ARG GO_IMAGE=golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651'
require_text Dockerfile 'ARG DEBIAN_IMAGE=debian:bookworm-slim@sha256:7b140f374b289a7c2befc338f42ebe6441b7ea838a042bbd5acbfca6ec875818'
require_text Dockerfile 'COPY release/runtime-assets.lock.json /runtime-assets.lock.json'
require_text Dockerfile 'release-tool materialize'
require_text Dockerfile "--arch \"${literal_dollar}TARGETARCH\""
require_text Dockerfile '--out-dir /assets'
require_text Dockerfile 'COPY --from=assets --chmod=0755 /assets/lib/rw-core'
require_text Dockerfile 'COPY --from=assets --chmod=0644 /assets/share/xray/geoip.dat'
require_text Dockerfile 'COPY --from=assets --chmod=0644 /assets/share/xray/geosite.dat'
require_text Dockerfile 'COPY --from=assets --chmod=0644 /assets/share/asn/asn-prefixes.bin'
for license_file in CC-BY-SA-4.0 CC0-1.0 GPL-3.0-only MPL-2.0; do
  require_text Dockerfile "COPY --from=assets --chmod=0644 /assets/licenses/${license_file}.txt /usr/share/doc/remnanode-lite/licenses/${license_file}.txt"
done
require_text Dockerfile 'COPY --chmod=0644 LICENSE /usr/share/doc/remnanode-lite/LICENSE'
require_text Dockerfile 'COPY --chmod=0644 release/bundle/THIRD_PARTY_NOTICES.md /usr/share/doc/remnanode-lite/THIRD_PARTY_NOTICES.md'
require_text Dockerfile 'COPY --chmod=0644 release/bundle/SOURCE-OFFER.md /usr/share/doc/remnanode-lite/SOURCE-OFFER.md'
require_text Dockerfile 'COPY --chmod=0644 release/runtime-assets.lock.json /usr/share/doc/remnanode-lite/runtime-assets.lock.json'
require_text Dockerfile 'ENTRYPOINT ["/usr/local/bin/remnanode-lite"]'
for stale_pin in \
  'ARG XRAY_CORE_VERSION=' \
  'ARG XRAY_AMD64_SHA256=' \
  'ARG XRAY_ARM64_SHA256=' \
  'ARG ASN_SOURCE_URL=' \
  'ARG ASN_SOURCE_SHA256=' \
  'Xray-linux-64.zip' \
  'Xray-linux-arm64-v8a.zip'; do
  if grep -Fq "$stale_pin" Dockerfile; then
    fail "Dockerfile duplicates runtime asset lock data: $stale_pin"
  fi
done

version="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' internal/version/version.go)"
[ -n "$version" ] || fail "application version is missing"
production_image="ghcr.io/luxiaba/remnanode-lite:${version}"
require_text compose.yaml "$production_image"
require_text .env.example "REMNANODE_IMAGE=${production_image}"
require_text compose.build.yaml "image: remnanode-lite:${version}"
require_text compose.build.yaml 'build:'

for compose_file in compose.yaml compose.build.yaml deploy/compose.single-file.yaml; do
  require_text "$compose_file" '  remnanode-lite:'
  if grep -Eq '^[[:space:]]{2}remnanode:' "$compose_file"; then
    fail "$compose_file still uses the legacy remnanode service name"
  fi
done
for compose_file in compose.yaml deploy/compose.single-file.yaml; do
  require_text "$compose_file" 'container_name: remnanode-lite'
  require_text "$compose_file" 'hostname: remnanode-lite'
done
if grep -Eq '^[[:space:]]+build:' compose.yaml; then
  fail "production compose.yaml must not require a source build"
fi

for required in \
  'network_mode: host' \
  'init: true' \
  'NET_ADMIN' \
  'NET_BIND_SERVICE' \
  'mem_limit: 448m' \
  'memswap_limit: 448m' \
  'cpus: 1.0' \
  'pids_limit: 256' \
  'read_only: true' \
  '["CMD", "/usr/local/bin/remnanode-lite", "healthcheck"]' \
  '/var/log/remnanode:rw,noexec,nosuid,nodev,size=28m,mode=0750' \
  'max-size: 2m'; do
  require_text compose.yaml "$required"
done

require_text deploy/compose.single-file.yaml \
  "image: \"\${REMNANODE_IMAGE:-ghcr.io/luxiaba/remnanode-lite:latest}\""
require_text deploy/compose.single-file.yaml \
  "SECRET_KEY: \"\${SECRET_KEY:?set SECRET_KEY in .env}\""
for variable in NODE_PORT NODE_BIND_ADDR LOW_MEMORY DISABLE_HASHED_SET_CHECK BODY_LIMIT_MB GOMEMLIMIT; do
  require_text compose.yaml "${variable}: \"\${${variable}"
  require_text deploy/compose.single-file.yaml "${variable}: \"\${${variable}"
done
for required in \
  'network_mode: host' \
  'init: true' \
  'read_only: true' \
  'mem_limit: 448m'; do
  require_text deploy/compose.single-file.yaml "$required"
done
if grep -Eq '^[[:space:]]*-[[:space:]]*SECRET_KEY=' deploy/compose.single-file.yaml; then
  fail "single-file Compose must use a mapping for SECRET_KEY"
fi
if grep -Eq '^[[:space:]]+volumes:' deploy/compose.single-file.yaml; then
  fail "single-file Compose must keep runtime logs ephemeral"
fi
if grep -Fq 'remnanode-logs' compose.yaml; then
  fail "production Compose must keep rw-core logs ephemeral"
fi
if grep -Eq 'ghcr\.io/[^[:space:]]+:latest' compose.yaml .env.example; then
  fail "production container configuration must default to an immutable version"
fi

container_workflow=.github/workflows/container.yml
require_text "$container_workflow" '      - "release/runtime-assets.lock.json"'
require_text "$container_workflow" 'Build linux/amd64 and linux/arm64 images without publishing'
require_text "$container_workflow" 'platforms: linux/amd64,linux/arm64'
require_text "$container_workflow" 'outputs: type=cacheonly'
require_text "$container_workflow" 'push: false'
require_text "$container_workflow" 'push-by-digest=true,name-canonical=true,push=true'
require_text "$container_workflow" 'provenance: mode=max'
require_text "$container_workflow" 'attest-main:'
require_text "$container_workflow" 'push-to-registry: true'
require_text "$container_workflow" 'scripts/promote-image-tag.sh immutable'
require_text "$container_workflow" 'scripts/promote-image-tag.sh mutable'
require_text "$container_workflow" "[ \"\$candidate_digest\" = \"\$SOURCE_DIGEST\" ]"
if grep -Eq 'type=raw[^[:space:]]*latest' "$container_workflow"; then
  fail "candidate workflow must not publish latest"
fi

release_workflow=.github/workflows/release.yml
require_text "$release_workflow" 'Verify main candidate image'
require_text "$release_workflow" \
  "candidate_tag=\"${literal_dollar}{REGISTRY}/${literal_dollar}{IMAGE_NAME}:sha-${literal_dollar}{candidate_commit}\""
require_text "$release_workflow" 'scripts/promote-image-tag.sh immutable'
require_text "$release_workflow" 'scripts/promote-image-tag.sh mutable'
require_text "$release_workflow" 'scripts/verify-release-tag.sh'
require_text "$release_workflow" 'Reconfirm the published Release and exact image'
require_text "$release_workflow" 'Promote the published release channel without rebuilding'
if grep -Fq 'docker/build-push-action@' "$release_workflow"; then
  fail "release workflow must promote the accepted candidate digest instead of rebuilding"
fi

release_single_file="$(sed \
  "s|ghcr.io/luxiaba/remnanode-lite:latest|ghcr.io/luxiaba/remnanode-lite:${version}|" \
  deploy/compose.single-file.yaml)"
grep -Fq "$production_image" <<<"$release_single_file"
if grep -Fq 'ghcr.io/luxiaba/remnanode-lite:latest' <<<"$release_single_file"; then
  fail "release single-file Compose still contains latest"
fi

packaging_tmp_dir="$(mktemp -d)"
trap 'rm -rf "$packaging_tmp_dir"' EXIT

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  command -v jq >/dev/null 2>&1 || fail "jq is required for Compose contract comparison"
  compose_env="${packaging_tmp_dir}/compose.env"
  printf '%s\n' \
    "REMNANODE_IMAGE=${production_image}" \
    'NODE_PORT=38329' \
    'NODE_BIND_ADDR=127.0.0.1' \
    'SECRET_KEY=packaging-check' \
    'LOW_MEMORY=1' \
    'DISABLE_HASHED_SET_CHECK=false' \
    'BODY_LIMIT_MB=12' \
    'GOMEMLIMIT=160MiB' \
    >"$compose_env"

  validate_compose() {
    local services
    services="$(docker compose --env-file "$compose_env" "$@" config --services)"
    [ "$services" = remnanode-lite ] || fail "Compose service set is $services"
    docker compose --env-file "$compose_env" "$@" config --quiet
  }
  validate_compose -f compose.yaml
  validate_compose -f compose.yaml -f compose.build.yaml
  validate_compose -f deploy/compose.single-file.yaml

  if env -u SECRET_KEY docker compose --env-file /dev/null \
    -f deploy/compose.single-file.yaml config --quiet >/dev/null 2>&1; then
    fail "single-file Compose accepted a missing SECRET_KEY"
  fi

  root_service="$(docker compose --env-file "$compose_env" -f compose.yaml \
    config --format json | jq -S '.services["remnanode-lite"]')"
  single_service="$(docker compose --env-file "$compose_env" -f deploy/compose.single-file.yaml \
    config --format json | jq -S '.services["remnanode-lite"]')"
  if [ "$root_service" != "$single_service" ]; then
    echo "production Compose templates do not resolve to the same service" >&2
    diff -u <(printf '%s\n' "$root_service") <(printf '%s\n' "$single_service") >&2 || true
    exit 1
  fi

  release_compose="${packaging_tmp_dir}/release-compose.yaml"
  release_env="${packaging_tmp_dir}/release.env"
  printf '%s\n' "$release_single_file" >"$release_compose"
  printf '%s\n' 'SECRET_KEY=packaging-check' >"$release_env"
  release_image="$(env -u REMNANODE_IMAGE docker compose \
    --env-file "$release_env" -f "$release_compose" config --format json \
    | jq -r '.services["remnanode-lite"].image')"
  [ "$release_image" = "$production_image" ] ||
    fail "release Compose default image is $release_image, want $production_image"
else
  echo "docker compose is unavailable; skipped Compose schema validation" >&2
fi

echo "Docker packaging checks passed"
