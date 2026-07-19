#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

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

bash scripts/check-version.sh
go mod verify
go mod tidy -diff
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...

echo "Go checks passed"
