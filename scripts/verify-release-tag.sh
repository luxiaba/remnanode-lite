#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: verify-release-tag.sh vX.Y.Z[-rnl.N] COMMIT" >&2
  exit 2
fi

tag="$1"
expected_commit="$2"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${GH_TOKEN:?GH_TOKEN is required}"

printf '%s\n' "$tag" | grep -Eq \
  '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rnl\.[1-9][0-9]*)?$' || {
  echo "invalid release tag: $tag" >&2
  exit 2
}
printf '%s\n' "$expected_commit" | grep -Eq '^[0-9a-f]{40}$' || {
  echo "invalid expected release commit: $expected_commit" >&2
  exit 2
}

ref="$(gh api "repos/${GITHUB_REPOSITORY}/git/ref/tags/${tag}")"
object_type="$(jq -r '.object.type // empty' <<<"$ref")"
object_sha="$(jq -r '.object.sha // empty' <<<"$ref")"
[ "$object_type" = tag ] || {
  echo "$tag is not an annotated Git tag" >&2
  exit 1
}
printf '%s\n' "$object_sha" | grep -Eq '^[0-9a-f]{40}$' || {
  echo "$tag returned an invalid annotated tag object" >&2
  exit 1
}

tag_object="$(gh api "repos/${GITHUB_REPOSITORY}/git/tags/${object_sha}")"
target_type="$(jq -r '.object.type // empty' <<<"$tag_object")"
target_commit="$(jq -r '.object.sha // empty' <<<"$tag_object")"
[ "$target_type" = commit ] || {
  echo "$tag points to a non-commit object ($target_type)" >&2
  exit 1
}
[ "$target_commit" = "$expected_commit" ] || {
  echo "$tag resolves to $target_commit, expected $expected_commit" >&2
  exit 1
}

printf 'verified release tag %s -> %s\n' "$tag" "$target_commit"
