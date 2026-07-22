#!/bin/sh

set -eu

repository_root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd -P)
installer=$repository_root/release/native/install.sh
temporary_directory=$(mktemp -d /tmp/remnanode-lite-bootstrap-test.XXXXXX)
temporary_directory=$(CDPATH='' cd -- "$temporary_directory" && pwd -P)
trap 'rm -rf -- "$temporary_directory"' 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

fail() {
	printf 'install_test.sh: %s\n' "$*" >&2
	exit 1
}

assert_contains() {
	case "$1" in
		*"$2"*) ;;
		*) fail "output does not contain: $2" ;;
	esac
}

assert_not_contains() {
	case "$1" in
		*"$2"*) fail "output unexpectedly contains: $2" ;;
		*) ;;
	esac
}

make_command_fixtures() {
	mkdir -p "$temporary_directory/fake-bin"
	cat >"$temporary_directory/fake-bin/id" <<'EOF'
#!/bin/sh
[ "${1:-}" = -u ] || exit 2
printf '0\n'
EOF
	cat >"$temporary_directory/fake-bin/uname" <<'EOF'
#!/bin/sh
case "${1:-}" in
	-s) printf 'Linux\n' ;;
	-m) printf 'x86_64\n' ;;
	*) exit 2 ;;
esac
EOF
	chmod 0755 "$temporary_directory/fake-bin/id" "$temporary_directory/fake-bin/uname"
	cat >"$temporary_directory/fake-bin/stat" <<'EOF'
#!/bin/sh
# The installer test fakes root, so fake root ownership consistently too.
printf '0 700\n'
EOF
	chmod 0755 "$temporary_directory/fake-bin/stat"
	TEST_REAL_CP=$(command -v cp)
	export TEST_REAL_CP
	cat >"$temporary_directory/fake-bin/cp" <<'EOF'
#!/bin/sh
case "${1:-}" in
	--) source_path=${2:-} ;;
	*) source_path=${1:-} ;;
esac
"$TEST_REAL_CP" "$@" || exit $?
if [ -n "${TEST_SWAP_SOURCE:-}" ] && [ "$source_path" = "$TEST_SWAP_SOURCE" ]; then
	"$TEST_REAL_CP" -- "$TEST_SWAP_REPLACEMENT" "$TEST_SWAP_SOURCE"
fi
EOF
	chmod 0755 "$temporary_directory/fake-bin/cp"

	real_tar=$(command -v tar)
	if ! "$real_tar" --version 2>/dev/null | grep -q 'GNU tar'; then
		TEST_REAL_TAR=$real_tar
		export TEST_REAL_TAR
		cat >"$temporary_directory/fake-bin/tar" <<'EOF'
#!/bin/sh
if [ "${1:-}" = --version ]; then
	printf 'tar (GNU tar) test fixture\n'
	exit 0
fi
[ "${1:-}" != --no-same-owner ] || shift
[ "${1:-}" != --no-same-permissions ] || shift
exec "$TEST_REAL_TAR" "$@"
EOF
		chmod 0755 "$temporary_directory/fake-bin/tar"
	fi
}

make_bundle_tree() {
	tree=$1
	bundle_version=${2:-9.8.7-rnl.3}
	mkdir -p "$tree/remnanode-lite/bin"
	printf '{"version":"%s"}\n' "$bundle_version" \
		>"$tree/remnanode-lite/release-manifest.json"
	cat >"$tree/remnanode-lite/bin/rnlctl" <<'EOF'
#!/bin/sh
for argument do
	printf '<%s>\n' "$argument"
done
expected_version=
bundle_archive=
bundle_root=
while [ "$#" -gt 0 ]; do
	case "$1" in
		--bundle)
			bundle_archive=$2
			shift 2
			;;
		--bundle-root)
			bundle_root=$2
			shift 2
			;;
		--expected-version)
			expected_version=$2
			shift 2
			;;
		*) shift ;;
	esac
	done
if [ -n "$bundle_archive" ]; then
	[ ! -d "$(dirname -- "$bundle_archive")/extracted" ] || {
		printf 'bootstrap extraction still exists during rnlctl execution\n' >&2
		exit 43
	}
	manifest=$(tar -xOzf "$bundle_archive" remnanode-lite/release-manifest.json)
else
	manifest=$(cat "$bundle_root/release-manifest.json")
fi
manifest_version=$(printf '%s\n' "$manifest" \
	| sed -n 's/.*"version":"\([^"]*\)".*/\1/p')
if [ -n "$expected_version" ] && [ "$manifest_version" != "$expected_version" ]; then
	printf 'manifest version mismatch: got %s, expected %s\n' \
		"$manifest_version" "$expected_version" >&2
	exit 42
fi
if [ -n "${TEST_HOST_WRITE_MARKER:-}" ]; then
	: >"$TEST_HOST_WRITE_MARKER"
fi
EOF
	chmod 0755 "$tree/remnanode-lite/bin/rnlctl"
}

make_archive() {
	tree=$1
	archive=$2
	tar -czf "$archive" -C "$tree" remnanode-lite
}

make_command_fixtures
mkdir -p "$temporary_directory/good-tree"
make_bundle_tree "$temporary_directory/good-tree"
good_archive=$temporary_directory/remnanode-lite_9.8.7-rnl.3_linux_amd64.tar.gz
make_archive "$temporary_directory/good-tree" "$good_archive"
archive_checksum=$(sha256sum "$good_archive")
archive_checksum=${archive_checksum%% *}
printf '%s  %s\n' "$archive_checksum" "$(basename -- "$good_archive")" \
	>"$temporary_directory/SHA256SUMS"
secret_file=$temporary_directory/input-secret.key
printf 'fixture-secret-that-must-not-be-logged\n' >"$secret_file"
chmod 0600 "$secret_file"

fixture_path=$temporary_directory/fake-bin:$PATH
installer_tmp=$temporary_directory/installer-tmp
mkdir -m 0700 "$installer_tmp"
export TMPDIR="$installer_tmp"
offline_output=$(TMPDIR=$installer_tmp PATH=$fixture_path sh "$installer" \
	--bundle "$good_archive" \
	--port 38329 \
	--secret-file "$secret_file" \
	--prepare-only \
	--yes)
assert_contains "$offline_output" '<install>'
assert_contains "$offline_output" '<--bundle>'
assert_contains "$offline_output" '<--sha256>'
assert_contains "$offline_output" "<$archive_checksum>"
assert_not_contains "$offline_output" "<$good_archive>"
assert_not_contains "$offline_output" '<--expected-version>'
assert_contains "$offline_output" '<--port>'
assert_contains "$offline_output" '<38329>'
assert_contains "$offline_output" '<--secret-file>'
assert_contains "$offline_output" "<$secret_file>"
assert_contains "$offline_output" '<--prepare-only>'
assert_not_contains "$offline_output" '<--yes>'
assert_not_contains "$offline_output" 'fixture-secret-that-must-not-be-logged'
[ -z "$(find "$installer_tmp" -mindepth 1 -print -quit)" ] \
	|| fail "installer did not clean its TMPDIR workspace"

prepare_output=$(PATH=$fixture_path sh "$installer" \
	--bundle "$good_archive" --prepare-only --yes </dev/null)
assert_contains "$prepare_output" '<install>'
assert_contains "$prepare_output" '<--prepare-only>'
assert_not_contains "$prepare_output" '<--secret-file>'
assert_not_contains "$prepare_output" 'Panel Secret:'

cp "$installer" "$temporary_directory/good-tree/remnanode-lite/install.sh"
chmod 0755 "$temporary_directory/good-tree/remnanode-lite/install.sh"
embedded_output=$(PATH=$fixture_path sh "$temporary_directory/good-tree/remnanode-lite/install.sh" \
	--secret-file "$secret_file" --yes)
assert_contains "$embedded_output" '<install>'
assert_contains "$embedded_output" "<$temporary_directory/good-tree/remnanode-lite>"
assert_not_contains "$embedded_output" '<--expected-version>'

if invalid_output=$(sh "$installer" --version latest 2>&1); then
	fail "invalid version unexpectedly succeeded"
fi
assert_contains "$invalid_output" "invalid version 'latest'"

if leading_zero_output=$(sh "$installer" --version 01.2.3 2>&1); then
	fail "version containing a leading zero unexpectedly succeeded"
fi
assert_contains "$leading_zero_output" "invalid version '01.2.3'"

if conflict_output=$(sh "$installer" --version 9.8.7 --bundle "$good_archive" 2>&1); then
	fail "conflicting source options unexpectedly succeeded"
fi
assert_contains "$conflict_output" '--version and --bundle are mutually exclusive'

if duplicate_prepare_output=$(sh "$installer" --prepare-only --prepare-only 2>&1); then
	fail "duplicate --prepare-only unexpectedly succeeded"
fi
assert_contains "$duplicate_prepare_output" '--prepare-only may be specified only once'

if duplicate_yes_output=$(sh "$installer" --yes --yes 2>&1); then
	fail "duplicate --yes unexpectedly succeeded"
fi
assert_contains "$duplicate_yes_output" '--yes may be specified only once'

mkdir -p "$temporary_directory/no-checksum"
no_checksum_archive=$temporary_directory/no-checksum/$(basename -- "$good_archive")
cp "$good_archive" "$no_checksum_archive"
if no_checksum_output=$(PATH=$fixture_path sh "$installer" \
	--bundle "$no_checksum_archive" --prepare-only --yes 2>&1); then
	fail "local bundle without an external checksum unexpectedly succeeded"
fi
assert_contains "$no_checksum_output" 'requires --sha256 HEX or a regular SHA256SUMS'

explicit_checksum_output=$(PATH=$fixture_path sh "$installer" \
	--bundle "$no_checksum_archive" --sha256 "$archive_checksum" \
	--prepare-only --yes)
assert_contains "$explicit_checksum_output" '<--bundle>'
assert_contains "$explicit_checksum_output" '<--sha256>'
assert_contains "$explicit_checksum_output" "<$archive_checksum>"

mkdir -p "$temporary_directory/unsafe-tree"
make_bundle_tree "$temporary_directory/unsafe-tree"
ln -s /etc/passwd "$temporary_directory/unsafe-tree/remnanode-lite/forbidden-link"
unsafe_archive=$temporary_directory/unsafe.tar.gz
make_archive "$temporary_directory/unsafe-tree" "$unsafe_archive"
unsafe_checksum=$(sha256sum "$unsafe_archive")
unsafe_checksum=${unsafe_checksum%% *}
if unsafe_output=$(PATH=$fixture_path sh "$installer" \
	--bundle "$unsafe_archive" --sha256 "$unsafe_checksum" \
	--secret-file "$secret_file" 2>&1); then
	fail "archive containing a symlink unexpectedly succeeded"
fi
assert_contains "$unsafe_output" 'forbidden entry type'

mkdir -p "$temporary_directory/race"
race_archive=$temporary_directory/race/$(basename -- "$good_archive")
cp "$good_archive" "$race_archive"
printf '%s  %s\n' "$archive_checksum" "$(basename -- "$race_archive")" \
	>"$temporary_directory/race/SHA256SUMS"
race_output=$(TEST_SWAP_SOURCE=$race_archive TEST_SWAP_REPLACEMENT=$unsafe_archive \
	PATH=$fixture_path sh "$installer" \
	--bundle "$race_archive" --prepare-only --yes)
assert_contains "$race_output" '<install>'
assert_contains "$race_output" '<--bundle>'
assert_contains "$race_output" "<$archive_checksum>"
race_replacement_checksum=$(sha256sum "$race_archive")
race_replacement_checksum=${race_replacement_checksum%% *}
[ "$race_replacement_checksum" = "$unsafe_checksum" ] \
	|| fail "TOCTOU fixture did not replace the caller-owned bundle path"

mkdir -p "$temporary_directory/downloads"
cp "$good_archive" "$temporary_directory/downloads/$(basename -- "$good_archive")"
printf '%s  %s\n' "$archive_checksum" "$(basename -- "$good_archive")" \
	>"$temporary_directory/downloads/SHA256SUMS"
cat >"$temporary_directory/fake-bin/curl" <<'EOF'
#!/bin/sh
destination=
url=
while [ "$#" -gt 0 ]; do
	case "$1" in
		--output)
			destination=$2
			shift 2
			;;
		--connect-timeout|--max-time|--retry)
			shift 2
			;;
		--*) shift ;;
		*) url=$1; shift ;;
	esac
done
[ -n "$destination" ] && [ -n "$url" ] || exit 2
cp "$TEST_DOWNLOAD_DIRECTORY/${url##*/}" "$destination"
EOF
chmod 0755 "$temporary_directory/fake-bin/curl"

online_output=$(TEST_DOWNLOAD_DIRECTORY=$temporary_directory/downloads \
	PATH=$fixture_path sh "$installer" \
	--version 9.8.7-rnl.3 --secret-file "$secret_file" --yes)
assert_contains "$online_output" 'Downloading Remnanode Lite 9.8.7-rnl.3 for linux/amd64'
assert_contains "$online_output" '<install>'
assert_contains "$online_output" '<--expected-version>'
assert_contains "$online_output" '<9.8.7-rnl.3>'
assert_not_contains "$online_output" '<--yes>'

mkdir -p "$temporary_directory/mismatched-tree"
make_bundle_tree "$temporary_directory/mismatched-tree" 9.8.7-rnl.2
mismatched_archive=$temporary_directory/downloads/$(basename -- "$good_archive")
make_archive "$temporary_directory/mismatched-tree" "$mismatched_archive"
mismatched_checksum=$(sha256sum "$mismatched_archive")
mismatched_checksum=${mismatched_checksum%% *}
printf '%s  %s\n' "$mismatched_checksum" "$(basename -- "$good_archive")" \
	>"$temporary_directory/downloads/SHA256SUMS"
host_write_marker=$temporary_directory/host-write-marker
if mismatch_output=$(TEST_DOWNLOAD_DIRECTORY=$temporary_directory/downloads \
	TEST_HOST_WRITE_MARKER=$host_write_marker PATH=$fixture_path sh "$installer" \
	--version 9.8.7-rnl.3 --prepare-only --yes 2>&1); then
	fail "online bundle with a mismatched manifest version unexpectedly succeeded"
fi
assert_contains "$mismatch_output" 'manifest version mismatch: got 9.8.7-rnl.2, expected 9.8.7-rnl.3'
[ ! -e "$host_write_marker" ] \
	|| fail "mismatched online bundle reached the simulated host-write boundary"

printf '%064d  %s\n' 0 "$(basename -- "$good_archive")" \
	>"$temporary_directory/downloads/SHA256SUMS"
if checksum_output=$(TEST_DOWNLOAD_DIRECTORY=$temporary_directory/downloads \
	PATH=$fixture_path sh "$installer" \
	--version 9.8.7-rnl.3 --secret-file "$secret_file" 2>&1); then
	fail "bundle with the wrong published checksum unexpectedly succeeded"
fi
assert_contains "$checksum_output" 'does not match the trusted expected digest'

printf 'Native bootstrap fixture tests passed\n'
