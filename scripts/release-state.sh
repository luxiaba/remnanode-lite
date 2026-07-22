#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <tag> <40-character-commit> <true|false-prerelease>" >&2
  exit 2
fi

release_tag=$1
source_commit=$2
expected_prerelease=$3
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

printf '%s\n' "$release_tag" | grep -Eq \
  '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rnl\.[1-9][0-9]*)?$' || {
  echo "invalid release tag: $release_tag" >&2
  exit 2
}
printf '%s\n' "$source_commit" | grep -Eq '^[0-9a-f]{40}$' || {
  echo "invalid source commit: $source_commit" >&2
  exit 2
}
case "$expected_prerelease" in
  true|false) ;;
  *) echo "expected prerelease must be true or false" >&2; exit 2 ;;
esac

# Listing releases avoids treating a normal "not found" response as an API
# error, while still finding an earlier draft created by a stopped workflow.
releases="$(gh api --paginate --slurp \
  "repos/${GITHUB_REPOSITORY}/releases?per_page=100")"
matches="$(jq -ce --arg tag "$release_tag" '
  [ .[] | if type == "array" then .[] else . end | select(.tag_name == $tag) ]
' <<<"$releases")"
match_count="$(jq 'length' <<<"$matches")"
case "$match_count" in
  0)
    printf 'state=absent\n'
    exit 0
    ;;
  1) ;;
  *)
    echo "more than one GitHub Release uses ${release_tag}" >&2
    exit 1
    ;;
esac

release="$(jq -ce '.[0]' <<<"$matches")"
release_id="$(jq -r 'if has("id") then .id else empty end' <<<"$release")"
target_commit="$(jq -r 'if has("target_commitish") then .target_commitish else empty end' <<<"$release")"
draft="$(jq -r 'if has("draft") then .draft else empty end' <<<"$release")"
prerelease="$(jq -r 'if has("prerelease") then .prerelease else empty end' <<<"$release")"
immutable="$(jq -r 'if has("immutable") then .immutable else empty end' <<<"$release")"

[[ "$release_id" =~ ^[1-9][0-9]*$ ]] || {
  echo "GitHub returned an invalid Release ID for ${release_tag}" >&2
  exit 1
}
[ "$target_commit" = "$source_commit" ] || {
  if [ "$draft" = true ]; then
    echo "${release_tag} has an unpublished draft for ${target_commit:-<empty>}; main advanced to a different candidate. Delete only that draft, accept the new candidate, then dispatch the release again." >&2
  else
    echo "${release_tag} targets ${target_commit:-<empty>}, expected ${source_commit}" >&2
  fi
  exit 1
}
[ "$prerelease" = "$expected_prerelease" ] || {
  echo "${release_tag} prerelease=${prerelease:-<empty>}, expected ${expected_prerelease}" >&2
  exit 1
}

case "$draft:$immutable" in
  true:false)
    state=draft
    ;;
  false:true)
    state=published
    ;;
  false:false)
    echo "${release_tag} is published but GitHub Release immutability is disabled" >&2
    exit 1
    ;;
  true:true)
    echo "${release_tag} cannot be both draft and immutable" >&2
    exit 1
    ;;
  *)
    echo "GitHub returned invalid Release state for ${release_tag}" >&2
    exit 1
    ;;
esac

printf 'state=%s\nrelease_id=%s\n' "$state" "$release_id"
