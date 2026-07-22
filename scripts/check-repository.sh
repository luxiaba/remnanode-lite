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
native_shell_files=()
while IFS= read -r -d '' file; do
  native_shell_files+=("$file")
done < <(find release/native -type f -name '*.sh' -print0)
[ "${#native_shell_files[@]}" -gt 0 ] || {
  echo "Native release scripts are missing" >&2
  exit 1
}
shellcheck -x scripts/*.sh deploy/remnanode-lite.openrc "${native_shell_files[@]}"
for script in scripts/*.sh; do
  bash -n "$script"
done
for script in "${native_shell_files[@]}"; do
  sh -n "$script"
done
sh -n deploy/remnanode-lite.openrc
actionlint
bash scripts/check-version-test.sh
bash scripts/verify-release-tag-test.sh
bash scripts/verify-release-latest-test.sh
bash scripts/verify-candidate-image-test.sh
bash scripts/verify-release-image-test.sh
bash scripts/find-workflow-run-test.sh
bash scripts/release-state-test.sh
bash scripts/test-docker-packaging.sh
bash scripts/check-supply-chain.sh

if command -v govulncheck >/dev/null 2>&1; then
  govulncheck ./...
elif [ "${REQUIRE_GOVULNCHECK:-0}" = "1" ]; then
  echo "govulncheck is required but not installed" >&2
  exit 1
fi

if [ -n "${CHECK_ARTIFACT_DIR:-}" ]; then
  artifact_dir="$CHECK_ARTIFACT_DIR"
  mkdir -p "$artifact_dir"
else
  artifact_dir="$(mktemp -d)"
  trap 'rm -rf "$artifact_dir"' EXIT
fi
bash scripts/build-release-binaries.sh "$artifact_dir"

echo "repository checks passed"
