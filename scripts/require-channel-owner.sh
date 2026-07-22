#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <vX.Y.Z[-rnl.N]-tag> <true|false-prerelease>" >&2
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
    [ "$newest" = "$release_tag" ] || {
      echo "${release_tag} is not the newest published preview (${newest:-none})" >&2
      exit 1
    }
    ;;
  false)
    bash scripts/verify-release-latest.sh "$release_tag" true
    ;;
  *)
    echo "prerelease must be true or false" >&2
    exit 2
    ;;
esac

echo "verified $release_tag owns its moving release channel"
