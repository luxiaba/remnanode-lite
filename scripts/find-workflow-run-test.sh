#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary_directory="$(mktemp -d)"
trap 'rm -rf "$temporary_directory"' EXIT

cat >"$temporary_directory/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "$1" = api ]
cat <<JSON
{
  "workflow_runs": [
    {"id": 10, "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "head_branch": "main", "conclusion": "failure", "event": "push", "run_number": 1},
    {"id": 11, "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "head_branch": "dev", "conclusion": "success", "event": "push", "run_number": 2},
    {"id": 12, "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "head_branch": "main", "conclusion": "success", "event": "push", "run_number": 3},
    {"id": 13, "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "head_branch": "main", "conclusion": "success", "event": "workflow_dispatch", "run_number": 4}
  ]
}
JSON
EOF
chmod 0755 "$temporary_directory/gh"

result="$(env \
  PATH="$temporary_directory:$PATH" \
  GITHUB_REPOSITORY=luxiaba/remnanode-lite \
  GH_TOKEN=test \
  "$root_dir/scripts/find-workflow-run.sh" ci.yml \
  aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa)"
[ "$result" = 13 ] || {
  echo "find-workflow-run returned $result, want 13" >&2
  exit 1
}

if env \
  PATH="$temporary_directory:$PATH" \
  GITHUB_REPOSITORY=luxiaba/remnanode-lite \
  GH_TOKEN=test \
  "$root_dir/scripts/find-workflow-run.sh" ci.yml \
  bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb >/dev/null 2>&1; then
  echo "find-workflow-run accepted a commit without a successful run" >&2
  exit 1
fi

echo "workflow run lookup tests passed"
