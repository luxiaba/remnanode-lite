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

version="$(extract_go_var Version)"
contract_version="$(extract_go_var ContractVersion)"
[ -n "$version" ] || fail "internal/version Version is missing"
[ -n "$contract_version" ] || fail "internal/version ContractVersion is missing"

stable_version_pattern='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
version_pattern="${stable_version_pattern%\$}(-rnl\.([1-9][0-9]*))?\$"
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

# Compare non-negative decimal strings without shell arithmetic. Release
# components are intentionally allowed to exceed the native integer width.
decimal_is_greater() {
  local left=$1 right=$2
  local LC_ALL=C
  if [ "${#left}" -ne "${#right}" ]; then
    [ "${#left}" -gt "${#right}" ]
    return
  fi
  [ "$left" != "$right" ] || return 1
  [[ "$left" > "$right" ]]
}

decimal_is_greater_or_equal() {
  local left=$1 right=$2
  [ "$left" = "$right" ] || decimal_is_greater "$left" "$right"
}

# A stable release must never move the public latest channels backwards. The
# comparison is component-wise and string-based after length normalization so
# it remains correct without relying on shell integer width.
stable_version_is_greater() {
  local left=$1 right=$2
  local left_component right_component index
  local -a left_parts right_parts
  local old_ifs=$IFS
  IFS=.
  read -r -a left_parts <<<"$left"
  read -r -a right_parts <<<"$right"
  IFS=$old_ifs
  for index in 0 1 2; do
    left_component=${left_parts[$index]}
    right_component=${right_parts[$index]}
    if decimal_is_greater "$left_component" "$right_component"; then
      return 0
    fi
    if decimal_is_greater "$right_component" "$left_component"; then
      return 1
    fi
  done
  return 1
}

if [[ "$version" != *-rnl.* ]]; then
  while IFS= read -r existing_tag; do
    existing_version=${existing_tag#v}
    [[ "$existing_version" =~ $stable_version_pattern ]] || continue
    [ "$existing_version" = "$version" ] && continue
    if stable_version_is_greater "$existing_version" "$version"; then
      fail "stable release $version is older than existing tag $existing_tag"
    fi
  done < <(git tag --list 'v*')
fi

metadata="$(go run ./cmd/release-tool metadata --tag "v${version}")" ||
  fail "release metadata rejected v${version}"
grep -Fxq "version=${version}" <<<"$metadata" ||
  fail "release metadata did not preserve version ${version}"
if [[ "$version" == *-rnl.* ]]; then
  grep -Fxq 'channel=preview' <<<"$metadata" ||
    fail "preview version ${version} did not select the preview channel"
  grep -Fxq 'make_latest=false' <<<"$metadata" ||
    fail "preview version ${version} would move latest"
else
  grep -Fxq 'channel=latest' <<<"$metadata" ||
    fail "stable version ${version} did not select the latest channel"
  grep -Fxq 'make_latest=true' <<<"$metadata" ||
    fail "stable version ${version} would not move latest"
fi

grep -Fq '"remnanode-contract-probe/"+version.Version' internal/contract/probe.go ||
  fail "contract probe User-Agent is not derived from the project version"
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
    preview_tag_pattern="^v${version_line//./\.}-rnl\.([1-9][0-9]*)$"
    while IFS= read -r existing_tag; do
      [ "$existing_tag" = "$release_tag" ] && continue
      [[ "$existing_tag" =~ $preview_tag_pattern ]] || continue
      existing_revision="${BASH_REMATCH[1]}"
      if decimal_is_greater_or_equal "$existing_revision" "$revision"; then
        fail "$release_tag must advance beyond existing $existing_tag"
      fi
    done < <(git tag --list "v${version_line}-rnl.*")
  fi
fi

echo "version check: $version (contract $contract_version, toolchain $toolchain)"
