#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
source scripts/check-lib.sh

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

forbidden_content_re='383[2]9|/[U]sers/|[Cc][Oo][Dd][Ee][Xx]|[Pp][Aa][Nn][Ee][Ll][[:space:]]+[^[:alnum:][:space:]]*[vV]?[0-9]+\.[0-9]+\.[0-9]+'
content_policy_failed=0
while IFS= read -r -d '' file; do
  [ -f "$file" ] || continue
  if LC_ALL=C grep -HInE -- "$forbidden_content_re" "$file"; then
    content_policy_failed=1
  fi
done < <(git ls-files -co --exclude-standard -z)
if [ "$content_policy_failed" -ne 0 ]; then
  echo 'repository contains personalized or fixed-compatibility content' >&2
  exit 1
fi

run_check_step "Documentation contracts" go run ./cmd/docs-check
native_shell_files=()
while IFS= read -r -d '' file; do
  native_shell_files+=("$file")
done < <(find release/native -type f -name '*.sh' -print0)
[ "${#native_shell_files[@]}" -gt 0 ] || {
  echo "Native release scripts are missing" >&2
  exit 1
}
run_check_step "ShellCheck" \
  shellcheck -x scripts/*.sh deploy/remnanode-lite.openrc "${native_shell_files[@]}"

check_shell_syntax() {
  local script
  for script in scripts/*.sh; do
    bash -n "$script" || return $?
  done
  for script in "${native_shell_files[@]}"; do
    sh -n "$script" || return $?
  done
  sh -n deploy/remnanode-lite.openrc || return $?
}
run_check_step "Shell syntax" check_shell_syntax
run_check_step "GitHub Actions lint" actionlint

repository_behavior_tests=(
  scripts/check-lib-test.sh
  scripts/check-version-test.sh
  scripts/verify-release-tag-test.sh
  scripts/verify-release-latest-test.sh
  scripts/require-channel-owner-test.sh
  scripts/verify-candidate-image-test.sh
  scripts/verify-release-image-test.sh
  scripts/verify-published-release-test.sh
  scripts/promote-image-tag-test.sh
  scripts/find-workflow-run-test.sh
  scripts/release-state-test.sh
  scripts/main-release-guard-test.sh
  scripts/verify-release-asset-attestations-test.sh
  scripts/verify-draft-release-test.sh
  scripts/test-docker-packaging.sh
  scripts/check-supply-chain.sh
)
for test_script in "${repository_behavior_tests[@]}"; do
  run_check_step "${test_script##*/}" bash "$test_script"
done

if command -v govulncheck >/dev/null 2>&1; then
  run_check_step "Reachable vulnerability scan" govulncheck ./...
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
run_check_step "Linux release cross-build" \
  bash scripts/build-release-binaries.sh "$artifact_dir"

echo "repository checks passed"
