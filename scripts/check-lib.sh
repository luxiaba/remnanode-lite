#!/usr/bin/env bash

run_check_step() {
  if [ "$#" -lt 2 ]; then
    echo "run_check_step requires a label and a command" >&2
    return 64
  fi

  local label=$1
  local started=$SECONDS
  local status
  shift

  printf '\n==> %s\n' "$label"
  if "$@"; then
    printf '<== %s passed (%ss)\n' "$label" "$((SECONDS - started))"
    return 0
  else
    status=$?
    printf '<== %s failed after %ss (exit %s)\n' \
      "$label" "$((SECONDS - started))" "$status" >&2
    return "$status"
  fi
}
