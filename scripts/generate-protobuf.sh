#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

mode="${1:---write}"
case "$mode" in
  --write|--check) ;;
  *)
    echo "usage: $0 [--write|--check]" >&2
    exit 2
    ;;
esac

for command in go protoc; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "required protobuf command is missing: $command" >&2
    exit 1
  }
done

readonly PROTOC_VERSION="35.1"
readonly PROTOC_GEN_GO_VERSION="v1.36.11"
readonly PROTO_FILE="internal/xtls/xrpc/wire.proto"
readonly GENERATED_FILE="internal/xtls/xrpc/wire.pb.go"

actual_protoc_version="$(protoc --version)"
if [ "$actual_protoc_version" != "libprotoc ${PROTOC_VERSION}" ]; then
  echo "protoc ${PROTOC_VERSION} is required, found ${actual_protoc_version:-unknown}" >&2
  exit 1
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir -p "$tmp_dir/bin" "$tmp_dir/generated"

GOBIN="$tmp_dir/bin" GOWORK=off GOFLAGS='' \
  go install "google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION}"
actual_plugin_version="$("$tmp_dir/bin/protoc-gen-go" --version)"
if [ "$actual_plugin_version" != "protoc-gen-go ${PROTOC_GEN_GO_VERSION}" ]; then
  echo "protoc-gen-go ${PROTOC_GEN_GO_VERSION} is required, found ${actual_plugin_version:-unknown}" >&2
  exit 1
fi

PATH="$tmp_dir/bin:$PATH" protoc \
  --go_out="$tmp_dir/generated" \
  --go_opt=paths=source_relative \
  "$PROTO_FILE"

generated="$tmp_dir/generated/$GENERATED_FILE"
if [ "$mode" = --check ]; then
  if ! cmp -s "$GENERATED_FILE" "$generated"; then
    diff -u "$GENERATED_FILE" "$generated" || true
    echo "$GENERATED_FILE is stale; run scripts/generate-protobuf.sh" >&2
    exit 1
  fi
  echo "protobuf generation check passed"
  exit 0
fi

install -m 0644 "$generated" "$GENERATED_FILE"
echo "generated $GENERATED_FILE"
