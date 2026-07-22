#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 <expected-vX.Y.Z[-rnl.N]-tag> <true|false>" >&2
  exit 2
}

[ "$#" -eq 2 ] || usage
expected_tag="$1"
make_latest="$2"
case "$make_latest" in
  true|false) ;;
  *) usage ;;
esac

: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

api_url="${GITHUB_API_URL:-https://api.github.com}"
api_url="${api_url%/}"
response_file="$(mktemp)"
trap 'rm -f "$response_file"' EXIT

if ! status="$(curl \
  --connect-timeout 10 \
  --max-time 30 \
  --retry 3 \
  --retry-all-errors \
  --silent \
  --show-error \
  --output "$response_file" \
  --write-out '%{http_code}' \
  --header "Accept: application/vnd.github+json" \
  --header "Authorization: Bearer ${GH_TOKEN}" \
  --header "X-GitHub-Api-Version: 2022-11-28" \
  "${api_url}/repos/${GITHUB_REPOSITORY}/releases/latest")"; then
  echo "could not query GitHub latest release" >&2
  exit 1
fi

case "$status" in
  200)
    latest_tag="$(jq -er '.tag_name | strings | select(length > 0)' "$response_file")" || {
      echo "GitHub latest release response has no tag_name" >&2
      exit 1
    }
    ;;
  404)
    # GitHub has no latest release while a repository only has prereleases.
    latest_tag=""
    ;;
  *)
    message="$(jq -r '.message // empty' "$response_file" 2>/dev/null || true)"
    if [ -n "$message" ]; then
      echo "GitHub latest release request returned HTTP $status: $message" >&2
    else
      echo "GitHub latest release request returned HTTP $status" >&2
    fi
    exit 1
    ;;
esac

if [ "$make_latest" = true ]; then
  [ "$latest_tag" = "$expected_tag" ] || {
    echo "GitHub latest is ${latest_tag:-<none>}, expected $expected_tag" >&2
    exit 1
  }
else
  [ "$latest_tag" != "$expected_tag" ] || {
    echo "preview release must not be GitHub latest" >&2
    exit 1
  }
fi
