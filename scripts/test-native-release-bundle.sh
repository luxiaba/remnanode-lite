#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: test-native-release-bundle.sh ARCHIVE [amd64|arm64]

Runs the real rnlctl lifecycle against a generated Native release bundle in a
temporary test root. This does not write /etc, /usr/local, /var/lib, or start a
service; the host/service-manager boundary is replaced by the rnlctl test fake.
EOF
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi
if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  usage >&2
  exit 2
fi

archive=$1
architecture=${2:-amd64}
case "$architecture" in
  amd64|arm64) ;;
  *) echo "unsupported Native bundle architecture: $architecture" >&2; exit 2 ;;
esac

case "$archive" in
  /*) ;;
  *) archive="$(pwd)/$archive" ;;
esac
[ -f "$archive" ] && [ ! -L "$archive" ] || {
  echo "archive must be a regular non-symlink file: $archive" >&2
  exit 2
}

GOTOOLCHAIN=local GOWORK=off GOFLAGS='' \
  REMNANODE_NATIVE_BUNDLE_SMOKE="$archive" \
  REMNANODE_NATIVE_BUNDLE_ARCH="$architecture" \
  go test -count=1 ./internal/rnlctl -run '^TestExternalNativeBundleInstallSmoke$' -v
