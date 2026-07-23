#!/usr/bin/env bash
set -euo pipefail

allow_non_owner=false
if [ "${1:-}" = --allow-non-owner ]; then
  allow_non_owner=true
  shift
fi

if [ "$#" -ne 2 ]; then
  echo "usage: $0 [--allow-non-owner] <vX.Y.Z[-rnl.N]-tag> <true|false-prerelease>" >&2
  exit 2
fi

release_tag=$1
prerelease=$2
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

case "$prerelease" in
  true)
    releases="$(gh release list --repo "$GITHUB_REPOSITORY" \
      --limit 100 --order desc --exclude-drafts \
      --json tagName,isPrerelease)"
    newest="$(jq -r '
      [
        .[]
        | select(.isPrerelease)
        | select(.tagName | test("^v[0-9]+\\.[0-9]+\\.[0-9]+-rnl\\.[1-9][0-9]*$"))
      ]
      | first
      | .tagName // empty
    ' <<<"$releases")"
    if [ "$newest" = "$release_tag" ] ||
      { [ "$allow_non_owner" = true ] && [ -z "$newest" ]; }; then
      promote=true
    elif [ "$allow_non_owner" = true ]; then
      promote=false
      echo "${release_tag} is not the newest published preview (${newest:-none}); leaving preview unchanged" >&2
    else
      echo "${release_tag} is not the newest published preview (${newest:-none})" >&2
      exit 1
    fi
    ;;
  false)
    if [ "$allow_non_owner" = true ]; then
      owner="$(bash scripts/verify-release-latest.sh --allow-non-owner "$release_tag" true)"
      case "$owner" in
        owner=true) promote=true ;;
        owner=false)
          promote=false
          echo "${release_tag} is not GitHub Latest; leaving latest unchanged" >&2
          ;;
        *)
          echo "could not classify GitHub Latest ownership for ${release_tag}" >&2
          exit 1
          ;;
      esac
    else
      bash scripts/verify-release-latest.sh "$release_tag" true
      promote=true
    fi
    ;;
  *)
    echo "prerelease must be true or false" >&2
    exit 2
    ;;
esac

printf 'promote=%s\n' "$promote"
