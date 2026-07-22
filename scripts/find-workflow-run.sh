#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: find-workflow-run.sh WORKFLOW_FILE COMMIT" >&2
  exit 2
fi

workflow=$1
commit=$2
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${GH_TOKEN:?GH_TOKEN is required}"

[[ "$workflow" =~ ^[A-Za-z0-9._-]+\.ya?ml$ ]] || {
  echo "invalid workflow file: $workflow" >&2
  exit 2
}
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || {
  echo "invalid workflow commit: $commit" >&2
  exit 2
}

runs="$(gh api --method GET \
  "repos/${GITHUB_REPOSITORY}/actions/workflows/${workflow}/runs" \
  -f head_sha="$commit" \
  -f status=completed \
  -f per_page=100)"
run_id="$(jq -r --arg commit "$commit" '
  [
    .workflow_runs[]?
    | select(.head_sha == $commit)
    | select(.head_branch == "main")
    | select(.conclusion == "success")
    | select(.event == "push" or .event == "workflow_dispatch")
  ]
  | sort_by(.run_number)
  | last
  | .id // empty
' <<<"$runs")"
[[ "$run_id" =~ ^[1-9][0-9]*$ ]] || {
  echo "no successful ${workflow} run exists for main commit ${commit}" >&2
  exit 1
}

printf '%s\n' "$run_id"
