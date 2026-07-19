#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 <immutable|mutable> <image> <sha256:digest> <tag>" >&2
  exit 2
}

[ "$#" -eq 4 ] || usage
mode="$1"
image="$2"
source_digest="$3"
tag="$4"

case "$mode" in
  immutable|mutable) ;;
  *) usage ;;
esac
[[ "$source_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || {
  echo "invalid source manifest digest: $source_digest" >&2
  exit 2
}
[[ "$tag" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] || {
  echo "invalid container tag: $tag" >&2
  exit 2
}

target="${image}:${tag}"
source="${image}@${source_digest}"

if [ "$mode" = immutable ]; then
  inspect_error="$(mktemp)"
  trap 'rm -f "$inspect_error"' EXIT
  if existing="$(docker buildx imagetools inspect \
    --format '{{.Manifest.Digest}}' "$target" 2>"$inspect_error" | tr -d '\r\n')"; then
    if [ "$existing" != "$source_digest" ]; then
      echo "refusing to move immutable tag $target from $existing to $source_digest" >&2
      exit 1
    fi
    echo "$target already resolves to $source_digest"
    exit 0
  fi
  if ! grep -Eqi 'not found|manifest unknown|name unknown|404' "$inspect_error"; then
    cat "$inspect_error" >&2
    echo "could not determine whether immutable tag $target exists" >&2
    exit 1
  fi
fi

docker buildx imagetools create \
  --prefer-index=false \
  --tag "$target" \
  "$source"

promoted="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "$target" | tr -d '\r\n')"
if [ "$promoted" != "$source_digest" ]; then
  echo "$target resolved to $promoted, expected $source_digest" >&2
  exit 1
fi
echo "$target now resolves to $source_digest"
