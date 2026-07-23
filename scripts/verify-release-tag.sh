#!/usr/bin/env bash
set -euo pipefail

mode=must-exist
case "${1:-}" in
  --allow-missing)
    mode=allow-missing
    shift
    ;;
  --require-missing)
    mode=require-missing
    shift
    ;;
esac

if [ "$#" -ne 2 ]; then
  echo "usage: verify-release-tag.sh [--allow-missing|--require-missing] X.Y.Z[-rnl.N] COMMIT" >&2
  exit 2
fi

tag="$1"
expected_commit="$2"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${GH_TOKEN:?GH_TOKEN is required}"

printf '%s\n' "$tag" | grep -Eq \
  '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rnl\.[1-9][0-9]*)?$' || {
  echo "invalid release tag: $tag" >&2
  exit 2
}
printf '%s\n' "$expected_commit" | grep -Eq '^[0-9a-f]{40}$' || {
  echo "invalid expected release commit: $expected_commit" >&2
  exit 2
}

error_file="$(mktemp)"
trap 'rm -f "$error_file"' EXIT
if ! ref="$(gh api "repos/${GITHUB_REPOSITORY}/git/ref/tags/${tag}" 2>"$error_file")"; then
  if [ "$mode" != must-exist ] && grep -Fqi 'HTTP 404' "$error_file"; then
    case "$mode" in
      allow-missing)
        printf 'verified release tag %s is not created yet\n' "$tag"
        ;;
      require-missing)
        printf 'verified release tag %s is absent before publication\n' "$tag"
        ;;
    esac
    exit 0
  fi
  cat "$error_file" >&2
  echo "could not inspect release tag $tag" >&2
  exit 1
fi

if [ "$mode" = require-missing ]; then
  echo "release tag ${tag} already exists before publication" >&2
  exit 1
fi

object_type="$(jq -r '.object.type // empty' <<<"$ref")"
object_sha="$(jq -r '.object.sha // empty' <<<"$ref")"
case "$object_type" in
  commit)
    target_commit=$object_sha
    ;;
  tag)
    tag_object="$(gh api "repos/${GITHUB_REPOSITORY}/git/tags/${object_sha}")"
    target_type="$(jq -r '.object.type // empty' <<<"$tag_object")"
    target_commit="$(jq -r '.object.sha // empty' <<<"$tag_object")"
    [ "$target_type" = commit ] || {
      echo "$tag points to a non-commit object ($target_type)" >&2
      exit 1
    }
    ;;
  *)
    echo "$tag points to an unsupported object type ($object_type)" >&2
    exit 1
    ;;
esac
printf '%s\n' "$target_commit" | grep -Eq '^[0-9a-f]{40}$' || {
  echo "$tag returned an invalid target commit" >&2
  exit 1
}
[ "$target_commit" = "$expected_commit" ] || {
  echo "$tag resolves to $target_commit, expected $expected_commit" >&2
  exit 1
}

printf 'verified release tag %s -> %s\n' "$tag" "$target_commit"
