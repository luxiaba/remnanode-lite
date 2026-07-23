#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <vX.Y.Z[-rnl.N]-tag> <X.Y.Z[-rnl.N]-version> <true|false-prerelease>" >&2
  exit 2
fi

release_tag=$1
version=$2
prerelease=$3
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${IMAGE:?IMAGE is required}"

[ "$release_tag" = "v$version" ] || {
  echo "release tag $release_tag does not match version $version" >&2
  exit 2
}
case "$prerelease" in
  true|false) ;;
  *) echo "expected prerelease must be true or false" >&2; exit 2 ;;
esac

git fetch origin "refs/tags/${release_tag}:refs/tags/${release_tag}"
release_commit="$(git rev-list -n 1 "$release_tag")"
bash scripts/verify-release-tag.sh "$release_tag" "$release_commit" >&2
release_state="$(bash scripts/release-state.sh "$release_tag" "$release_commit" "$prerelease")"
[ "$(sed -n 's/^state=//p' <<<"$release_state")" = published ] || {
  echo "${release_tag} is not an immutable published Release" >&2
  exit 1
}
gh release verify "$release_tag" --repo "$GITHUB_REPOSITORY" >&2

candidate_output="$(
  GITHUB_SHA="$release_commit" \
    bash scripts/verify-candidate-image.sh
)"
candidate_state="$(sed -n 's/^state=//p' <<<"$candidate_output")"
candidate_digest="$(sed -n 's/^digest=//p' <<<"$candidate_output")"
[ "$candidate_state" = present ] && [[ "$candidate_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || {
  echo "release candidate verification returned invalid output" >&2
  exit 1
}

printf 'commit=%s\ndigest=%s\n' "$release_commit" "$candidate_digest"
