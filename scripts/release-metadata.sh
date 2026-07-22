#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: release-metadata.sh vX.Y.Z[-rnl.N]" >&2
  exit 2
fi

tag="$1"
stable_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
preview_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-rnl\.([1-9][0-9]*)$'

case "$tag" in
  *-rnl.*)
    [[ "$tag" =~ $preview_pattern ]] || {
      echo "invalid preview release tag: $tag" >&2
      exit 2
    }
    channel=preview
    prerelease=true
    make_latest=false
    ;;
  *)
    [[ "$tag" =~ $stable_pattern ]] || {
      echo "invalid stable release tag: $tag" >&2
      exit 2
    }
    channel=latest
    prerelease=false
    make_latest=true
    ;;
esac

printf 'version=%s\n' "${tag#v}"
printf 'channel=%s\n' "$channel"
printf 'prerelease=%s\n' "$prerelease"
printf 'make_latest=%s\n' "$make_latest"
