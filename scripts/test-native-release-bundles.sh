#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <release-asset-directory> <version>" >&2
  exit 2
fi

directory=$1
version=$2
for architecture in amd64 arm64; do
  bash scripts/test-native-release-bundle.sh \
    "$directory/remnanode-lite_${version}_linux_${architecture}.tar.gz" \
    "$architecture"
done
