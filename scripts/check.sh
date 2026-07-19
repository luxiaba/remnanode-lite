#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

bash scripts/check-go.sh
bash scripts/check-repository.sh
bash scripts/test-install-ops.sh

if command -v govulncheck >/dev/null 2>&1; then
  govulncheck ./...
elif [ "${REQUIRE_GOVULNCHECK:-0}" = "1" ]; then
  echo "govulncheck is required but not installed" >&2
  exit 1
fi

echo "all repository checks passed"
