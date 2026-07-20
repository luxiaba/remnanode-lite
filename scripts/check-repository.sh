#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

for command in go git shellcheck actionlint; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "required repository check command is missing: $command" >&2
    exit 1
  }
done

required_shellcheck_version=0.11.0
shellcheck_version="$(shellcheck --version | sed -n 's/^version: //p')"
if [ "$shellcheck_version" != "$required_shellcheck_version" ]; then
  echo "shellcheck $required_shellcheck_version is required, found ${shellcheck_version:-unknown}" >&2
  exit 1
fi

git diff --check
git diff --cached --check
go run ./cmd/docs-check
shellcheck -x scripts/*.sh deploy/remnawave-node.openrc
for script in scripts/*.sh; do
  bash -n "$script"
done
sh -n deploy/remnawave-node.openrc
actionlint
bash scripts/test-docker-packaging.sh
bash scripts/check-supply-chain.sh

if [ -n "${CHECK_ARTIFACT_DIR:-}" ]; then
  artifact_dir="$CHECK_ARTIFACT_DIR"
  mkdir -p "$artifact_dir"
else
  artifact_dir="$(mktemp -d)"
  trap 'rm -rf "$artifact_dir"' EXIT
fi
bash scripts/build-release-binaries.sh "$artifact_dir"

echo "repository checks passed"
