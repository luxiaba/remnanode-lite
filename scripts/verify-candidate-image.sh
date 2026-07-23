#!/usr/bin/env bash
set -euo pipefail

allow_missing=false
if [ "${1:-}" = --allow-missing ]; then
  allow_missing=true
  shift
fi
[ "$#" -eq 0 ] || {
  echo "usage: $0 [--allow-missing]" >&2
  exit 2
}

: "${GH_TOKEN:?GH_TOKEN is required}"
: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
: "${GITHUB_SHA:?GITHUB_SHA is required}"
: "${IMAGE:?IMAGE is required}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"

[[ "$GITHUB_SHA" =~ ^[0-9a-f]{40}$ ]] || {
  echo "invalid candidate source commit: $GITHUB_SHA" >&2
  exit 2
}

candidate="${IMAGE}:sha-${GITHUB_SHA}"
inspect_error="$(mktemp)"
trap 'rm -f "$inspect_error"' EXIT
if ! digest="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "$candidate" 2>"$inspect_error" | tr -d '\r\n')"; then
  if [ "$allow_missing" = true ] &&
    grep -Eqi 'manifest unknown|name unknown|not found|HTTP 404' "$inspect_error"; then
    echo "$candidate does not exist yet" >&2
    printf 'state=absent\n'
    exit 0
  fi
  cat "$inspect_error" >&2
  echo "could not inspect candidate image $candidate" >&2
  exit 1
fi
[[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || {
  echo "$candidate returned an invalid manifest digest: $digest" >&2
  exit 1
}
docker buildx imagetools inspect --raw "${IMAGE}@${digest}" \
  >"$RUNNER_TEMP/candidate-index.json"
go run ./cmd/release-tool verify-index \
  --manifest "$RUNNER_TEMP/candidate-index.json" \
  --digest "$digest" >&2
for platform in linux/amd64 linux/arm64; do
  architecture="${platform#linux/}"
  sbom="$RUNNER_TEMP/candidate-sbom-${architecture}.json"
  docker buildx imagetools inspect \
    --format "{{json (index .SBOM \"${platform}\")}}" \
    "${IMAGE}@${digest}" >"$sbom"
  jq -e '
    .SPDX
    | type == "object"
      and (.SPDXID == "SPDXRef-DOCUMENT")
      and (.spdxVersion | type == "string" and startswith("SPDX-"))
      and (.dataLicense | type == "string" and length > 0)
      and (.documentNamespace | type == "string" and length > 0)
      and (.creationInfo.creators | type == "array" and length > 0)
  ' "$sbom" >/dev/null || {
    echo "$candidate has no valid SPDX SBOM for $platform" >&2
    exit 1
  }
done
gh attestation verify "oci://${IMAGE}@${digest}" \
  --repo "$GITHUB_REPOSITORY" \
  --cert-identity "https://github.com/${GITHUB_REPOSITORY}/.github/workflows/container.yml@refs/heads/main" \
  --source-digest "$GITHUB_SHA" \
  --predicate-type https://slsa.dev/provenance/v1 \
  --deny-self-hosted-runners >&2
printf 'state=present\ndigest=%s\n' "$digest"
