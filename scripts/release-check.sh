#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

status="$(git status --porcelain --untracked-files=all)"
if [ -n "$status" ]; then
  echo "release requires a clean worktree:" >&2
  echo "$status" >&2
  exit 1
fi
if git ls-files --error-unmatch cmd/m8-runtime-probe >/dev/null 2>&1; then
  echo "temporary M8 runtime probe must not be tracked" >&2
  exit 1
fi

official_source="${REMNANODE_OFFICIAL_SOURCE:-}"
[ -n "$official_source" ] || {
  echo "REMNANODE_OFFICIAL_SOURCE must point to a Git repository containing the pinned official commit" >&2
  exit 1
}
go run ./cmd/contract-source-check -source "$official_source"

version="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' internal/version/version.go)"
release_tag="${RELEASE_TAG:-v${version}}"
RELEASE_TAG="$release_tag" bash scripts/check-version.sh

grep -Fq 'Unreleased' CHANGELOG.md && {
  echo "CHANGELOG still contains Unreleased" >&2
  exit 1
}
grep -Eq "^## \[${version//./\\.}\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$" CHANGELOG.md || {
  echo "CHANGELOG does not contain a dated ${version} release heading" >&2
  exit 1
}

release_note="docs/releases/v${version}.md"
[ -s "$release_note" ] || {
  echo "missing or empty $release_note" >&2
  exit 1
}
git ls-files --error-unmatch "$release_note" >/dev/null 2>&1 || {
  echo "$release_note must be tracked" >&2
  exit 1
}
[ "$(head -n 1 "$release_note")" = "# v${version}" ] || {
  echo "$release_note must start with '# v${version}'" >&2
  exit 1
}
for heading in \
  '## Compatibility' \
  '## Acceptance Results' \
  '## Known Risks' \
  '## Installation and Upgrade'; do
  grep -Fxq "$heading" "$release_note" || {
    echo "$release_note is missing heading: $heading" >&2
    exit 1
  }
done
manifest="docs/development/acceptance/v${version}/manifest.json"
grep -Fq "../development/acceptance/v${version}/manifest.json" "$release_note" || {
  echo "$release_note does not link $manifest" >&2
  exit 1
}
if grep -Eiq 'TODO|TBD|TO BE COMPLETED|Unreleased|IN PROGRESS' "$release_note"; then
  echo "$release_note still contains placeholder text" >&2
  exit 1
fi

evidence_summary="$(go run ./cmd/release-evidence-check -manifest "$manifest" -tag "$release_tag")"
if [[ "$evidence_summary" =~ \(candidate\ ([0-9a-f]{40}),\ image\ (sha256:[0-9a-f]{64})\)$ ]]; then
  candidate_commit="${BASH_REMATCH[1]}"
  candidate_digest="${BASH_REMATCH[2]}"
else
  echo "could not parse the validated candidate identity" >&2
  exit 1
fi
grep -Fq "$candidate_commit" "$release_note" || {
  echo "$release_note does not record candidate commit $candidate_commit" >&2
  exit 1
}
grep -Fq "$candidate_digest" "$release_note" || {
  echo "$release_note does not record candidate image digest $candidate_digest" >&2
  exit 1
}
printf '%s\n' "$evidence_summary"

artifact_dir="$(mktemp -d)"
trap 'rm -rf "$artifact_dir"' EXIT
REMNANODE_OFFICIAL_SOURCE='' CHECK_ARTIFACT_DIR="$artifact_dir" bash scripts/check.sh
go run ./cmd/release-evidence-check \
  -manifest "$manifest" \
  -tag "$release_tag" \
  -artifacts "$artifact_dir"

if [ "${REQUIRE_TAG_AT_HEAD:-0}" = "1" ]; then
  [ "$(git cat-file -t "refs/tags/${release_tag}" 2>/dev/null || true)" = tag ] || {
    echo "$release_tag must be an annotated tag" >&2
    exit 1
  }
  [ "$(git rev-list -n 1 "$release_tag")" = "$(git rev-parse HEAD)" ] || {
    echo "$release_tag does not point at HEAD" >&2
    exit 1
  }
fi
echo "release gate passed for $release_tag"
