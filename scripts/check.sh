#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
source scripts/check-lib.sh

run_check_step "Go gate" bash scripts/check-go.sh
run_check_step "Repository gate" bash scripts/check-repository.sh
run_check_step "Offline Native bootstrap tests" sh release/native/install_test.sh

echo "all repository checks passed"
