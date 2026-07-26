#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
source scripts/check-lib.sh

pass_output="$(run_check_step 'passing fixture' bash -c 'exit 0')"
[[ "$pass_output" == *'passing fixture passed ('* ]] || {
  echo "check timing helper did not report success" >&2
  exit 1
}

set +e
fail_output="$(run_check_step 'failing fixture' bash -c 'exit 23' 2>&1)"
fail_status=$?
set -e
[ "$fail_status" -eq 23 ] || {
  echo "check timing helper changed exit 23 to $fail_status" >&2
  exit 1
}
[[ "$fail_output" == *'failing fixture failed after '*' (exit 23)'* ]] || {
  echo "check timing helper did not report failure" >&2
  exit 1
}

guarded_sequence() {
  bash -c 'exit 29' || return $?
  return 0
}
set +e
sequence_output="$(run_check_step 'guarded sequence' guarded_sequence 2>&1)"
sequence_status=$?
set -e
[ "$sequence_status" -eq 29 ] || {
  echo "check timing helper masked guarded sequence exit 29 as $sequence_status" >&2
  exit 1
}
[[ "$sequence_output" == *'guarded sequence failed after '*' (exit 29)'* ]] || {
  echo "check timing helper did not report guarded sequence failure" >&2
  exit 1
}

echo "check timing helper tests passed"
