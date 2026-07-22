#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary_directory="$(mktemp -d)"
trap 'rm -rf "$temporary_directory"' EXIT

fail() {
  echo "version policy test: $*" >&2
  exit 1
}

make_fixture() {
  local version=$1 contract=$2 fixture tag
  shift 2
  fixture="$(mktemp -d "$temporary_directory/fixture.XXXXXX")"
  mkdir -p \
    "$fixture/bin" \
    "$fixture/scripts" \
    "$fixture/internal/version" \
    "$fixture/internal/contract"
  cp "$ROOT_DIR/scripts/check-version.sh" "$fixture/scripts/check-version.sh"
  cat >"$fixture/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

[ "$1" = run ]
[ "$2" = ./cmd/release-tool ]
[ "$3" = metadata ]
[ "$4" = --tag ]
tag=$5
if [[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rnl\.[1-9][0-9]*)?$ ]]; then
  :
else
  exit 1
fi
case "$tag" in
  *-rnl.*)
    channel=preview
    prerelease=true
    make_latest=false
    ;;
  *)
    channel=latest
    prerelease=false
    make_latest=true
    ;;
esac
printf 'version=%s\n' "${tag#v}"
printf 'tag=%s\n' "$tag"
printf 'channel=%s\n' "$channel"
printf 'prerelease=%s\n' "$prerelease"
printf 'make_latest=%s\n' "$make_latest"
EOF
  chmod 0755 "$fixture/bin/go"

  cat >"$fixture/internal/version/version.go" <<EOF
package version

var Version = "$version"
var ContractVersion = "$contract"
EOF
  printf '%s\n' "$contract" >"$fixture/internal/version/contract.version"
  cat >"$fixture/internal/contract/probe.go" <<'EOF'
package contract

var userAgent = "remnanode-contract-probe/"+version.Version
EOF
  printf 'services:\n  node:\n    image: ghcr.io/luxiaba/remnanode-lite:%s\n' \
    "$version" >"$fixture/compose.yaml"
  printf 'REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite:%s\n' \
    "$version" >"$fixture/.env.example"
  printf 'services:\n  node:\n    image: remnanode-lite:%s\n' \
    "$version" >"$fixture/compose.build.yaml"
  cat >"$fixture/go.mod" <<'EOF'
module example.com/version-policy-fixture

go 1.26

toolchain go1.26.5
EOF

  git -C "$fixture" init --quiet --initial-branch=main
  git -C "$fixture" config user.name version-policy-test
  git -C "$fixture" config user.email version-policy-test@example.invalid
  git -C "$fixture" config commit.gpgsign false
  git -C "$fixture" config tag.gpgsign false
  git -C "$fixture" add .
  git -C "$fixture" -c core.hooksPath=/dev/null commit --quiet --no-gpg-sign -m fixture
  for tag in "$@"; do
    git -C "$fixture" tag "$tag"
  done
  printf '%s\n' "$fixture"
}

assert_version_passes() {
  local name=$1 version=$2 contract=$3 release_tag=$4 tag_count=$5
  local fixture output
  shift 5
  [ "$#" -eq "$tag_count" ] || fail "invalid test definition: $name"
  fixture="$(make_fixture "$version" "$contract" "$@")"
  if ! output="$(cd "$fixture" && PATH="$fixture/bin:$PATH" RELEASE_TAG="$release_tag" \
    bash scripts/check-version.sh 2>&1)"; then
    fail "$name unexpectedly failed: $output"
  fi
}

assert_version_fails() {
  local name=$1 expected=$2 version=$3 contract=$4 release_tag=$5 tag_count=$6
  local fixture output
  shift 6
  [ "$#" -eq "$tag_count" ] || fail "invalid test definition: $name"
  fixture="$(make_fixture "$version" "$contract" "$@")"
  if output="$(cd "$fixture" && PATH="$fixture/bin:$PATH" RELEASE_TAG="$release_tag" \
    bash scripts/check-version.sh 2>&1)"; then
    fail "$name unexpectedly passed"
  fi
  [[ "$output" == *"$expected"* ]] ||
    fail "$name failed for the wrong reason: $output"
}

# Stable releases compare each component numerically and ignore preview tags.
assert_version_passes \
  'stable minor advancement' 2.10.0 2.10.0 v2.10.0 1 v2.9.999
assert_version_passes \
  'stable major advancement' 3.0.0 3.0.0 v3.0.0 1 v2.999.999
assert_version_passes \
  'stable exact tag rerun' 2.8.0 2.8.0 v2.8.0 1 v2.8.0
assert_version_passes \
  'preview tag does not block stable' 2.8.0 2.8.0 v2.8.0 1 v99.0.0-rnl.9
assert_version_fails \
  'stable minor rollback' 'older than existing tag v2.10.0' \
  2.9.999 2.9.999 v2.9.999 1 v2.10.0
assert_version_fails \
  'stable patch rollback' 'older than existing tag v2.8.10' \
  2.8.9 2.8.9 v2.8.9 1 v2.8.10
assert_version_passes \
  'stable component wider than shell integer' \
  100000000000000000000.0.0 100000000000000000000.0.0 \
  v100000000000000000000.0.0 1 v99999999999999999999.999.999
assert_version_fails \
  'stable rollback beyond shell integer width' \
  'older than existing tag v100000000000000000000.0.0' \
  99999999999999999999.999.999 99999999999999999999.999.999 \
  v99999999999999999999.999.999 1 v100000000000000000000.0.0

# Preview revisions are monotonic only within their X.Y.Z line. Exact reruns
# are allowed, but any other release tag must advance N.
assert_version_passes \
  'preview revision advancement' 2.8.0-rnl.10 2.8.0 v2.8.0-rnl.10 1 \
  v2.8.0-rnl.9
assert_version_passes \
  'preview exact tag rerun' 2.8.0-rnl.9 2.8.0 v2.8.0-rnl.9 1 \
  v2.8.0-rnl.9
assert_version_passes \
  'separate preview line is independent' 2.9.0-rnl.1 2.8.0 v2.9.0-rnl.1 1 \
  v2.8.0-rnl.999
assert_version_fails \
  'preview revision rollback' 'must advance beyond existing v2.8.0-rnl.10' \
  2.8.0-rnl.9 2.8.0 v2.8.0-rnl.9 1 v2.8.0-rnl.10
assert_version_passes \
  'preview revision wider than shell integer' \
  2.8.0-rnl.100000000000000000000 2.8.0 \
  v2.8.0-rnl.100000000000000000000 1 \
  v2.8.0-rnl.99999999999999999999
assert_version_fails \
  'preview rollback beyond shell integer width' \
  'must advance beyond existing v2.8.0-rnl.100000000000000000000' \
  2.8.0-rnl.99999999999999999999 2.8.0 \
  v2.8.0-rnl.99999999999999999999 1 \
  v2.8.0-rnl.100000000000000000000
assert_version_passes \
  'malformed preview tag is ignored' 2.8.0-rnl.2 2.8.0 v2.8.0-rnl.2 1 \
  v2.8.0-rnl.invalid-rnl.999

assert_version_fails \
  'stable must match contract' 'must match contract 2.8.0' \
  2.8.1 2.8.0 v2.8.1 0
assert_version_fails \
  'zero preview revision is invalid' 'must use X.Y.Z or X.Y.Z-rnl.N' \
  2.8.0-rnl.0 2.8.0 v2.8.0-rnl.0 0

echo 'version policy tests passed'
