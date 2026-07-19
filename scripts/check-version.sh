#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

fail() {
  echo "version check: $*" >&2
  exit 1
}

extract_go_var() {
  local name="$1"
  sed -n "s/^var ${name} = \"\([^\"]*\)\"$/\1/p" internal/version/version.go
}

extract_script_version() {
  local path="$1"
  sed -n 's/^VERSION="\([^"]*\)"$/\1/p' "$path"
}

version="$(extract_go_var Version)"
contract_version="$(extract_go_var ContractVersion)"
[ -n "$version" ] || fail "internal/version Version is missing"
[ -n "$contract_version" ] || fail "internal/version ContractVersion is missing"

version_pattern='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rnl\.([1-9][0-9]*))?$'
[[ "$version" =~ $version_pattern ]] ||
  fail "release version $version must use X.Y.Z or X.Y.Z-rnl.N"
[[ "$contract_version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
  fail "contract version $contract_version must use X.Y.Z"

file_contract_version="$(tr -d '[:space:]' < internal/version/contract.version)"
[ "$contract_version" = "$file_contract_version" ] ||
  fail "contract.version=${file_contract_version}, Go ContractVersion=${contract_version}"
if [[ "$version" != *-rnl.* ]] && [ "$version" != "$contract_version" ]; then
  fail "official-aligned release $version must match contract $contract_version"
fi

for script in \
  scripts/install-node.sh \
  scripts/install-node-alpine.sh \
  scripts/upgrade.sh \
  scripts/uninstall.sh; do
  script_version="$(extract_script_version "$script")"
  [ "$script_version" = "$version" ] ||
    fail "$script VERSION=${script_version:-missing}, want $version"
done

expected_release_tag="$(printf "RNL_TAG=\"\${RNL_TAG:-v%s}\"" "$version")"
grep -Fq "$expected_release_tag" scripts/install-xray.sh ||
  fail "scripts/install-xray.sh default RNL_TAG is not v$version"
grep -Fq "remnanode-contract-probe/${version}" internal/contract/probe.go ||
  fail "contract probe User-Agent is not $version"
expected_ghcr_image="ghcr.io/luxiaba/remnanode-lite:${version}"
grep -Fq "$expected_ghcr_image" compose.yaml ||
  fail "compose.yaml image is not $expected_ghcr_image"
grep -Fq "REMNANODE_IMAGE=${expected_ghcr_image}" .env.example ||
  fail ".env.example image is not $expected_ghcr_image"
grep -Fq "image: remnanode-lite:${version}" compose.build.yaml ||
  fail "compose.build.yaml local image is not remnanode-lite:$version"
toolchain="$(sed -n 's/^toolchain[[:space:]][[:space:]]*//p' go.mod)"
[ "$toolchain" = "go1.26.5" ] || fail "go.mod toolchain=${toolchain:-missing}, want go1.26.5"

release_tag="${RELEASE_TAG:-}"
if [ -n "$release_tag" ]; then
  [[ "${release_tag#v}" =~ $version_pattern ]] && [[ "$release_tag" == v* ]] ||
    fail "release tag $release_tag must use vX.Y.Z or vX.Y.Z-rnl.N"
  [ "$release_tag" = "v$version" ] || fail "release tag $release_tag does not match v$version"

  if [[ "$version" == *-rnl.* ]]; then
    version_line="${version%%-rnl.*}"
    revision="${version##*-rnl.}"
    while IFS= read -r existing_tag; do
      [ "$existing_tag" = "$release_tag" ] && continue
      existing_revision="${existing_tag##*-rnl.}"
      [[ "$existing_revision" =~ ^[1-9][0-9]*$ ]] || continue
      if [ "$existing_revision" -ge "$revision" ]; then
        fail "$release_tag must advance beyond existing $existing_tag"
      fi
    done < <(git tag --list "v${version_line}-rnl.*")
  fi
fi

echo "version check: $version (contract $contract_version, toolchain $toolchain)"
