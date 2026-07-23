#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

bash scripts/check-go.sh
bash scripts/check-repository.sh
sh release/native/install_test.sh

echo "all repository checks passed"
