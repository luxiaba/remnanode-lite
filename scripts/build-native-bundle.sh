#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: build-native-bundle.sh OUTPUT_DIR [amd64|arm64 ...]

Builds deterministic, self-contained Native Linux bundles. If no architecture
is given, both amd64 and arm64 are built.

Environment:
  RNL_ASSET_CACHE_DIR       Content-addressed asset cache
  RNL_NATIVE_INSTALLER      Self-contained install.sh input
  SOURCE_DATE_EPOCH         Override the source commit timestamp
  SOURCE_REVISION           Override the source Git commit
  RNL_OFFLINE_BUILD=1       Forbid network access; require a complete cache
EOF
}

if [ "$#" -lt 1 ] || [ -z "$1" ]; then
  usage >&2
  exit 2
fi

case "$1" in
  /*) output_dir="$1" ;;
  *) output_dir="$(pwd)/$1" ;;
esac
shift

if [ "$#" -eq 0 ]; then
  architectures=(amd64 arm64)
else
  architectures=("$@")
fi

declare -A seen_architectures=()
for architecture in "${architectures[@]}"; do
  case "$architecture" in
    amd64|arm64) ;;
    *) echo "unsupported Native bundle architecture: $architecture" >&2; exit 2 ;;
  esac
  if [ -n "${seen_architectures[$architecture]:-}" ]; then
    echo "duplicate Native bundle architecture: $architecture" >&2
    exit 2
  fi
  seen_architectures[$architecture]=1
done

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

version="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' internal/version/version.go)"
contract_version="$(tr -d ' \n\r' < internal/version/contract.version)"
toolchain="$(sed -n 's/^toolchain[[:space:]][[:space:]]*//p' go.mod)"
[ -n "$version" ] && [ -n "$contract_version" ] && [ -n "$toolchain" ] || {
  echo "Native release metadata is incomplete" >&2
  exit 1
}
actual_toolchain="$(GOTOOLCHAIN=local go env GOVERSION)"
[ "$actual_toolchain" = "$toolchain" ] || {
  echo "Native release build requires $toolchain, found $actual_toolchain" >&2
  exit 1
}

source_revision="${SOURCE_REVISION:-$(git rev-parse HEAD)}"
source_date_epoch="${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct "$source_revision")}"
asset_cache="${RNL_ASSET_CACHE_DIR:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}/remnanode-lite-release-assets}"
installer="${RNL_NATIVE_INSTALLER:-$root_dir/release/native/install.sh}"
offline_args=()
if [ "${RNL_OFFLINE_BUILD:-0}" = 1 ]; then
  offline_args=(--offline)
fi

[ -f "$installer" ] && [ ! -L "$installer" ] && [ -x "$installer" ] || {
  echo "missing executable Native installer: $installer" >&2
  exit 1
}
mkdir -p "$output_dir" "$asset_cache"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/remnanode-native-build.XXXXXX")"
cleanup() {
  rm -rf -- "$work_dir"
}
trap cleanup EXIT INT TERM

support_dir="$work_dir/native-support"
mkdir -p "$support_dir/deploy"
for support_file in \
  node.env.example \
  remnanode-lite-hardening.conf \
  remnanode-lite.openrc \
  remnanode-lite.service; do
  source_file="$root_dir/deploy/$support_file"
  [ -f "$source_file" ] && [ ! -L "$source_file" ] || {
    echo "missing canonical Native support file: $source_file" >&2
    exit 1
  }
  cp "$source_file" "$support_dir/deploy/$support_file"
done
chmod 0644 \
  "$support_dir/deploy/node.env.example" \
  "$support_dir/deploy/remnanode-lite-hardening.conf" \
  "$support_dir/deploy/remnanode-lite.service"
chmod 0755 "$support_dir/deploy/remnanode-lite.openrc"

GOTOOLCHAIN=local GOWORK=off GOFLAGS='' GOEXPERIMENT='' GOFIPS140=off CGO_ENABLED=0 \
  go build -mod=readonly -buildvcs=false -trimpath -ldflags='-s -w' \
  -o "$work_dir/release-tool" ./cmd/release-tool
GOTOOLCHAIN=local GOWORK=off GOFLAGS='' GOEXPERIMENT='' GOFIPS140=off CGO_ENABLED=0 \
  go build -mod=readonly -buildvcs=false -trimpath -ldflags='-s -w' \
  -o "$work_dir/asn-builder" ./cmd/asn-builder

"$work_dir/release-tool" validate --lock release/runtime-assets.lock.json

for architecture in "${architectures[@]}"; do
  arch_environment=()
  case "$architecture" in
    amd64) arch_environment=(GOAMD64=v1) ;;
    arm64) arch_environment=(GOARM64=v8.0) ;;
  esac
  ldflags="-s -w -X github.com/luxiaba/remnanode-lite/internal/version.Version=${version} -X github.com/luxiaba/remnanode-lite/internal/version.ContractVersion=${contract_version}"
  env GOTOOLCHAIN=local GOWORK=off GOFLAGS='' GOEXPERIMENT='' GOFIPS140=off \
    CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" "${arch_environment[@]}" \
    go build -mod=readonly -buildvcs=false -trimpath -ldflags="$ldflags" \
    -o "$work_dir/remnanode-lite-$architecture" ./cmd/remnanode-lite
  env GOTOOLCHAIN=local GOWORK=off GOFLAGS='' GOEXPERIMENT='' GOFIPS140=off \
    CGO_ENABLED=0 GOOS=linux GOARCH="$architecture" "${arch_environment[@]}" \
    go build -mod=readonly -buildvcs=false -trimpath -ldflags="$ldflags" \
    -o "$work_dir/rnlctl-$architecture" ./cmd/rnlctl

  archive="$output_dir/remnanode-lite_${version}_linux_${architecture}.tar.gz"
  "$work_dir/release-tool" build \
    --lock release/runtime-assets.lock.json \
    --arch "$architecture" \
    --version "$version" \
    --contract-version "$contract_version" \
    --source-revision "$source_revision" \
    --source-date-epoch "$source_date_epoch" \
    --project-root "$root_dir" \
    --node "$work_dir/remnanode-lite-$architecture" \
    --rnlctl "$work_dir/rnlctl-$architecture" \
    --asn-builder "$work_dir/asn-builder" \
    --installer "$installer" \
    --support-dir "$support_dir" \
    --cache-dir "$asset_cache" \
    --out "$archive" \
    "${offline_args[@]}"

  "$work_dir/release-tool" verify \
    --lock release/runtime-assets.lock.json \
    --archive "$archive" \
    --arch "$architecture" \
    --version "$version" \
    --contract-version "$contract_version" \
    --source-revision "$source_revision"
done
