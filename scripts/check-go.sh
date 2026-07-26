#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
source scripts/check-lib.sh

for command in go git gofmt; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "required Go check command is missing: $command" >&2
    exit 1
  }
done

git diff --check
git diff --cached --check
unformatted="$(
  while IFS= read -r -d '' file; do
    gofmt -l -- "$file"
  done < <(git ls-files -co --exclude-standard -z -- '*.go')
)"
if [ -n "$unformatted" ]; then
  echo "gofmt is required for:" >&2
  echo "$unformatted" >&2
  exit 1
fi

run_check_step "Version policy" bash scripts/check-version.sh
run_check_step "Go module verification" go mod verify
run_check_step "Go module tidiness" go mod tidy -diff
run_check_step "Go test suite" go test -count=1 ./...
run_check_step "Runtime race suite" \
  go test -race -count=1 ./internal/... ./cmd/remnanode-lite
run_check_step "Go vet" go vet ./...

echo "Go checks passed"
