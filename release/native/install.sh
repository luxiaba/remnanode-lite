#!/bin/sh

set -eu

PROGRAM=install.sh
REPOSITORY_URL=https://github.com/luxiaba/remnanode-lite
MAX_CHECKSUM_BYTES=1048576
MAX_BUNDLE_BYTES=536870912

version=
bundle_path=
sha256_value=
port=
secret_file=
prepare_only=0
assume_yes=0
temporary_directory=
temporary_secret=
temporary_root=
control_binary=
tty_echo_disabled=0

usage() {
	cat <<'EOF'
Usage:
  sudo sh install.sh --version VERSION [options]
  sudo sh install.sh --bundle PATH [options]
  sudo ./install.sh [options]                 # from an extracted bundle

Install a specific Remnanode Lite Native Linux release. Online installs never
follow latest implicitly; VERSION must be an exact X.Y.Z or X.Y.Z-rnl.N value.

Options:
  --version VERSION      Download the exact vVERSION GitHub Release bundle
  --bundle PATH          Install from a local Native bundle archive
  --sha256 HEX           Expected SHA-256 for --bundle. When omitted, read the
                         unique matching entry from SHA256SUMS beside PATH
  --port PORT            Set the Panel-to-Node listening port (1-65535)
  --secret-file PATH     Read the Panel Secret from a regular file
  --prepare-only         Install and configure without enabling or starting
                         the service
  --yes                  Skip the non-secret installation confirmation
  -h, --help             Show this help

When no Secret file exists and --secret-file is omitted, the installer reads
the Secret from /dev/tty with terminal echo disabled. The Secret is never put
in a command argument or environment variable.
EOF
}

fail() {
	printf '%s: %s\n' "$PROGRAM" "$*" >&2
	exit 1
}

usage_error() {
	printf '%s: %s\n\n' "$PROGRAM" "$*" >&2
	usage >&2
	exit 2
}

restore_tty() {
	if [ "$tty_echo_disabled" -eq 1 ]; then
		stty echo </dev/tty 2>/dev/null || :
		tty_echo_disabled=0
	fi
}

cleanup() {
	restore_tty
	if [ -n "$temporary_directory" ] && [ -d "$temporary_directory" ]; then
		rm -rf -- "$temporary_directory"
	fi
}

trap cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

option_seen() {
	case "$1" in
		version) [ -n "$version" ] ;;
		bundle) [ -n "$bundle_path" ] ;;
		sha256) [ -n "$sha256_value" ] ;;
		port) [ -n "$port" ] ;;
		secret) [ -n "$secret_file" ] ;;
		*) return 1 ;;
	esac
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--version)
			[ "$#" -ge 2 ] || usage_error "--version requires a value"
			option_seen version && usage_error "--version may be specified only once"
			version=$2
			shift 2
			;;
		--version=*)
			option_seen version && usage_error "--version may be specified only once"
			version=${1#*=}
			[ -n "$version" ] || usage_error "--version requires a value"
			shift
			;;
		--bundle)
			[ "$#" -ge 2 ] || usage_error "--bundle requires a path"
			option_seen bundle && usage_error "--bundle may be specified only once"
			bundle_path=$2
			shift 2
			;;
		--bundle=*)
			option_seen bundle && usage_error "--bundle may be specified only once"
			bundle_path=${1#*=}
			[ -n "$bundle_path" ] || usage_error "--bundle requires a path"
			shift
			;;
		--sha256)
			[ "$#" -ge 2 ] || usage_error "--sha256 requires a digest"
			option_seen sha256 && usage_error "--sha256 may be specified only once"
			sha256_value=$2
			shift 2
			;;
		--sha256=*)
			option_seen sha256 && usage_error "--sha256 may be specified only once"
			sha256_value=${1#*=}
			[ -n "$sha256_value" ] || usage_error "--sha256 requires a digest"
			shift
			;;
		--port)
			[ "$#" -ge 2 ] || usage_error "--port requires a value"
			option_seen port && usage_error "--port may be specified only once"
			port=$2
			shift 2
			;;
		--port=*)
			option_seen port && usage_error "--port may be specified only once"
			port=${1#*=}
			[ -n "$port" ] || usage_error "--port requires a value"
			shift
			;;
		--secret-file)
			[ "$#" -ge 2 ] || usage_error "--secret-file requires a path"
			option_seen secret && usage_error "--secret-file may be specified only once"
			secret_file=$2
			shift 2
			;;
		--secret-file=*)
			option_seen secret && usage_error "--secret-file may be specified only once"
			secret_file=${1#*=}
			[ -n "$secret_file" ] || usage_error "--secret-file requires a path"
			shift
			;;
		--prepare-only)
			[ "$prepare_only" -eq 0 ] \
				|| usage_error "--prepare-only may be specified only once"
			prepare_only=1
			shift
			;;
		--yes)
			[ "$assume_yes" -eq 0 ] \
				|| usage_error "--yes may be specified only once"
			assume_yes=1
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		--)
			shift
			[ "$#" -eq 0 ] || usage_error "positional arguments are not supported"
			;;
		-*) usage_error "unknown option: $1" ;;
		*) usage_error "unexpected positional argument: $1" ;;
	esac
done

if [ -n "$version" ] && [ -n "$bundle_path" ]; then
	usage_error "--version and --bundle are mutually exclusive"
fi
if [ -n "$sha256_value" ] && [ -z "$bundle_path" ]; then
	usage_error "--sha256 is valid only with --bundle"
fi

if [ -n "$version" ]; then
	printf '%s\n' "$version" | grep -Eq \
		'^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rnl\.[1-9][0-9]*)?$' \
		|| usage_error "invalid version '$version'; expected X.Y.Z or X.Y.Z-rnl.N"
fi
if [ -n "$sha256_value" ]; then
	printf '%s\n' "$sha256_value" | grep -Eq '^[0-9A-Fa-f]{64}$' \
		|| usage_error "invalid --sha256; expected exactly 64 hexadecimal characters"
fi

if [ -n "$port" ]; then
	case "$port" in
		*[!0-9]*|'') usage_error "invalid port '$port'; expected 1-65535" ;;
	esac
	[ "$port" -ge 1 ] 2>/dev/null && [ "$port" -le 65535 ] 2>/dev/null \
		|| usage_error "invalid port '$port'; expected 1-65535"
fi

command -v id >/dev/null 2>&1 || fail "required command not found: id"
[ "$(id -u)" -eq 0 ] || fail "run this installer as root (for example, with sudo)"

[ "$(uname -s)" = Linux ] || fail "Native installation is supported only on Linux"
case "$(uname -m)" in
	x86_64|amd64) architecture=amd64 ;;
	aarch64|arm64) architecture=arm64 ;;
	*) fail "unsupported CPU architecture: $(uname -m) (expected x86_64 or arm64)" ;;
esac

for required_command in tar sha256sum mktemp awk grep wc cp chmod tr mkdir stat dirname; do
	command -v "$required_command" >/dev/null 2>&1 \
		|| fail "required command not found: $required_command"
done
if [ -n "$sha256_value" ]; then
	sha256_value=$(printf '%s' "$sha256_value" | tr 'A-F' 'a-f')
fi
tar_version=$(tar --version 2>/dev/null | awk 'NR == 1 { print; exit }') || :
case "$tar_version" in
	*'GNU tar'*) ;;
	*) fail "GNU tar is required for ownership-safe Native bundle extraction" ;;
esac

umask 077
safe_temporary_root() {
	requested_root=$1
	case "$requested_root" in
		/*) ;;
		*) return 1 ;;
	esac
	[ -d "$requested_root" ] && [ -w "$requested_root" ] && [ -x "$requested_root" ] \
		|| return 1
	resolved_root=$(CDPATH='' cd -- "$requested_root" 2>/dev/null && pwd -P) \
		|| return 1
	[ -d "$resolved_root" ] || return 1
	case "$resolved_root" in
		/|/tmp|/var/tmp) ;;
		*) [ ! -L "$requested_root" ] || return 1 ;;
	esac

	# The installer runs as root. Every directory in the resolved path must be
	# root-owned; a shared sticky directory such as /var/tmp is safe for the
	# private 0700 child created below.
	checked_root=$resolved_root
	while :; do
		root_metadata=$(stat -Lc '%u %a' -- "$checked_root" 2>/dev/null) \
			|| return 1
		root_uid=${root_metadata%% *}
		root_mode=${root_metadata#* }
		[ "$root_uid" = "0" ] || return 1
		case "$root_mode" in
			*[!0-9]*) return 1 ;;
		esac
		root_permissions=$((root_mode % 1000))
		root_special=$((root_mode / 1000))
		root_group=$(((root_permissions / 10) % 10))
		root_other=$((root_permissions % 10))
		if { [ $((root_group & 2)) -ne 0 ] || [ $((root_other & 2)) -ne 0 ]; } \
			&& [ $((root_special & 1)) -eq 0 ]; then
			return 1
		fi
		[ "$checked_root" = / ] && break
		checked_root=$(dirname -- "$checked_root")
	done
	printf '%s\n' "$resolved_root"
}

if [ -n "${TMPDIR:-}" ]; then
	temporary_root=$(safe_temporary_root "$TMPDIR") || temporary_root=
fi
if [ -z "$temporary_root" ]; then
	temporary_root=/var/lib/remnanode-lite-installer/tmp
	if mkdir -p -- "$temporary_root" 2>/dev/null \
		&& chmod 0700 "$temporary_root" 2>/dev/null; then
		temporary_root=$(safe_temporary_root "$temporary_root") || temporary_root=
	else
		temporary_root=
	fi
fi
if [ -z "$temporary_root" ]; then
	temporary_root=$(safe_temporary_root /var/tmp) || temporary_root=
fi
[ -n "$temporary_root" ] \
	|| fail "no safe disk-backed Native temporary root is available"
temporary_directory=$(mktemp -d "$temporary_root/remnanode-lite-install.XXXXXX") \
	|| fail "cannot create a secure temporary directory below $temporary_root"
[ -d "$temporary_directory" ] && [ ! -L "$temporary_directory" ] \
	|| fail "temporary workspace is not a real directory"
chmod 0700 "$temporary_directory" \
	|| fail "cannot restrict the temporary workspace"
TMPDIR=$temporary_root
export TMPDIR

regular_file() {
	[ -f "$1" ] && [ ! -L "$1" ]
}

absolute_file_path() {
	file_directory=$(dirname -- "$1")
	file_name=$(basename -- "$1")
	absolute_directory=$(CDPATH='' cd -- "$file_directory" 2>/dev/null && pwd -P) \
		|| return 1
	printf '%s/%s\n' "$absolute_directory" "$file_name"
}

file_size() {
	wc -c <"$1" | awk '{ print $1 }'
}

check_file_size() {
	checked_size=$(file_size "$1")
	case "$checked_size" in
		''|*[!0-9]*) fail "cannot determine downloaded file size" ;;
	esac
	[ "$checked_size" -gt 0 ] && [ "$checked_size" -le "$2" ] \
		|| fail "$3 size $checked_size is outside 1..$2 bytes"
}

download_file() {
	download_url=$1
	download_destination=$2
	case "$download_url" in
		https://github.com/luxiaba/remnanode-lite/releases/download/*) ;;
		*) fail "refusing unexpected download URL" ;;
	esac

	if command -v curl >/dev/null 2>&1; then
		curl --disable --fail --location --silent --show-error --retry 3 \
			--proto '=https' --proto-redir '=https' --tlsv1.2 \
			--connect-timeout 15 --max-time 900 \
			--output "$download_destination" "$download_url" \
			|| fail "download failed: $download_url"
	elif command -v wget >/dev/null 2>&1; then
		wget --quiet --https-only --tries=3 --timeout=30 \
			--output-document="$download_destination" "$download_url" \
			|| fail "download failed: $download_url"
	else
		fail "online installation requires curl or wget"
	fi
}

checksum_for_asset() {
	awk -v name="$2" '
		($2 == name || $2 == "*" name) && length($1) == 64 && $1 ~ /^[0-9A-Fa-f]+$/ {
			if (found) exit 2
			print tolower($1)
			found = 1
		}
		END { if (!found) exit 1 }
	' "$1"
}

verify_bundle_checksum() {
	archive_file=$1
	expected_checksum=$2
	actual_checksum=$(sha256sum "$archive_file") \
		|| fail "cannot calculate the bundle SHA-256"
	actual_checksum=${actual_checksum%% *}
	[ "$actual_checksum" = "$expected_checksum" ] \
		|| fail "bundle SHA-256 does not match the trusted expected digest"
}

inspect_bundle_archive() {
	archive_file=$1
	name_listing=$temporary_directory/archive-names
	type_listing=$temporary_directory/archive-types
	seen_listing=$temporary_directory/archive-seen

	LC_ALL=C tar -tzf "$archive_file" >"$name_listing" \
		|| fail "cannot read the Native bundle archive"
	LC_ALL=C tar -tvzf "$archive_file" >"$type_listing" \
		|| fail "cannot inspect Native bundle entry types"
	: >"$seen_listing"
	entry_count=0
	while IFS= read -r archive_entry || [ -n "$archive_entry" ]; do
		entry_count=$((entry_count + 1))
		[ "$entry_count" -le 512 ] || fail "Native bundle has too many archive entries"
		case "$archive_entry" in
			*[!A-Za-z0-9._/+:-]*) fail "Native bundle contains an invalid archive path" ;;
		esac
		trimmed_entry=${archive_entry%/}
		[ -n "$trimmed_entry" ] || fail "Native bundle contains an empty archive path"
		case "$trimmed_entry" in
			/*|..|../*|*/..|*/../*|*//*|./*|*/./*|*/.)
				fail "Native bundle contains an unsafe archive path"
				;;
		esac
		case "$trimmed_entry" in
			remnanode-lite|remnanode-lite/*) ;;
			*) fail "Native bundle entries must remain below remnanode-lite/" ;;
		esac
		if grep -Fqx -e "$trimmed_entry" "$seen_listing"; then
			fail "Native bundle contains duplicate archive entries"
		fi
		printf '%s\n' "$trimmed_entry" >>"$seen_listing"
	done <"$name_listing"
	[ "$entry_count" -gt 0 ] || fail "Native bundle archive is empty"

	type_count=0
	while IFS= read -r verbose_entry || [ -n "$verbose_entry" ]; do
		type_count=$((type_count + 1))
		entry_type=${verbose_entry%"${verbose_entry#?}"}
		case "$entry_type" in
			-|d) ;;
			*) fail "Native bundle contains a link, device, or other forbidden entry type" ;;
		esac
	done <"$type_listing"
	[ "$type_count" -eq "$entry_count" ] \
		|| fail "Native bundle archive listings are inconsistent"
}

extract_bundle() {
	archive_file=$1
	extract_directory=$temporary_directory/extracted
	mkdir -m 0700 "$extract_directory"
	# A 022 extraction umask preserves the bundle's required 0755/0644 modes
	# while --no-same-permissions strips special or unexpectedly broad bits.
	# Only the independently executable controller is needed by bootstrap. The
	# controller validates and extracts the complete archive in its own private
	# workspace after the outer checksum has been checked.
	(umask 022; LC_ALL=C tar --no-same-owner --no-same-permissions \
		-xzf "$archive_file" -C "$extract_directory" \
		remnanode-lite/release-manifest.json remnanode-lite/bin/rnlctl) \
		|| fail "cannot extract the Native bundle"
	bundle_root=$extract_directory/remnanode-lite
	regular_file "$bundle_root/release-manifest.json" \
		|| fail "Native bundle is missing a regular release-manifest.json"
	regular_file "$bundle_root/bin/rnlctl" \
		|| fail "Native bundle is missing a regular bin/rnlctl"
	[ -x "$bundle_root/bin/rnlctl" ] \
		|| fail "Native bundle bin/rnlctl is not executable"
}

script_directory=
case "$0" in
	*/*)
		script_directory=$(CDPATH='' cd -- "$(dirname -- "$0")" 2>/dev/null && pwd -P) || :
		;;
esac

bundle_root=
archive_install=0
expected_sha256=
expected_version=
if [ -n "$bundle_path" ]; then
	regular_file "$bundle_path" || fail "--bundle must name a regular non-symlink file"
	bundle_path=$(absolute_file_path "$bundle_path") \
		|| fail "cannot resolve the local bundle path"
	local_bundle_name=$(basename -- "$bundle_path")
	local_bundle_directory=$(dirname -- "$bundle_path")
	bundle_snapshot=$temporary_directory/local-bundle.tar.gz
	cp -- "$bundle_path" "$bundle_snapshot" \
		|| fail "cannot copy the local bundle into the private installer workspace"
	chmod 0600 "$bundle_snapshot" \
		|| fail "cannot restrict the private bundle snapshot"
	regular_file "$bundle_snapshot" || fail "private bundle snapshot is not a regular file"
	check_file_size "$bundle_snapshot" "$MAX_BUNDLE_BYTES" "Native bundle"

	if [ -n "$sha256_value" ]; then
		expected_sha256=$sha256_value
	else
		local_checksum_file=$local_bundle_directory/SHA256SUMS
		if ! regular_file "$local_checksum_file" || [ ! -r "$local_checksum_file" ]; then
			fail "local bundle requires --sha256 HEX or a regular SHA256SUMS beside it; download SHA256SUMS from the same exact GitHub Release"
		fi
		checksum_snapshot=$temporary_directory/local-SHA256SUMS
		cp -- "$local_checksum_file" "$checksum_snapshot" \
			|| fail "cannot copy SHA256SUMS into the private installer workspace"
		chmod 0600 "$checksum_snapshot" \
			|| fail "cannot restrict the private SHA256SUMS snapshot"
		regular_file "$checksum_snapshot" || fail "private SHA256SUMS snapshot is not a regular file"
		check_file_size "$checksum_snapshot" "$MAX_CHECKSUM_BYTES" "SHA256SUMS"
		expected_sha256=$(checksum_for_asset "$checksum_snapshot" "$local_bundle_name") \
			|| fail "SHA256SUMS has no unique valid entry for $local_bundle_name"
	fi
	verify_bundle_checksum "$bundle_snapshot" "$expected_sha256"
	inspect_bundle_archive "$bundle_snapshot"
	extract_bundle "$bundle_snapshot"
	bundle_path=$bundle_snapshot
	archive_install=1
elif [ -n "$script_directory" ] \
	&& regular_file "$script_directory/release-manifest.json" \
	&& regular_file "$script_directory/bin/rnlctl" \
	&& [ -x "$script_directory/bin/rnlctl" ]; then
	[ -z "$version" ] \
		|| usage_error "--version cannot be used from inside an extracted bundle"
	bundle_root=$script_directory
else
	[ -n "$version" ] \
		|| usage_error "choose an exact --version or provide --bundle PATH"
	asset_name=remnanode-lite_${version}_linux_${architecture}.tar.gz
	release_url=$REPOSITORY_URL/releases/download/${version}
	checksum_path=$temporary_directory/SHA256SUMS
	bundle_path=$temporary_directory/$asset_name
	printf 'Downloading Remnanode Lite %s for linux/%s...\n' "$version" "$architecture"
	download_file "$release_url/SHA256SUMS" "$checksum_path"
	check_file_size "$checksum_path" "$MAX_CHECKSUM_BYTES" "SHA256SUMS"
	download_file "$release_url/$asset_name" "$bundle_path"
	check_file_size "$bundle_path" "$MAX_BUNDLE_BYTES" "Native bundle"
	expected_sha256=$(checksum_for_asset "$checksum_path" "$asset_name") \
		|| fail "SHA256SUMS has no unique valid entry for $asset_name"
	verify_bundle_checksum "$bundle_path" "$expected_sha256"
	inspect_bundle_archive "$bundle_path"
	extract_bundle "$bundle_path"
	archive_install=1
	expected_version=$version
fi

# Preserve the verified archive as the trust anchor passed to rnlctl, but do
# not retain the bootstrap extraction while rnlctl performs its own verified
# extraction. The standalone Go controller does not depend on adjacent files.
if [ "$archive_install" -eq 1 ]; then
	control_binary=$temporary_directory/rnlctl
	cp -- "$bundle_root/bin/rnlctl" "$control_binary" \
		|| fail "cannot stage the Native lifecycle controller"
	chmod 0755 "$control_binary" \
		|| fail "cannot make the Native lifecycle controller executable"
	regular_file "$control_binary" && [ -x "$control_binary" ] \
		|| fail "staged Native lifecycle controller is invalid"
	rm -rf -- "${bundle_root%/remnanode-lite}" \
		|| fail "cannot remove the bootstrap extraction workspace"
else
	control_binary=$bundle_root/bin/rnlctl
fi

# rnlctl always validates the manifest and every bundled payload before it
# writes host state. This also protects direct execution from an extracted
# bundle, where no outer SHA256SUMS file is available.

if [ "$assume_yes" -eq 0 ]; then
	[ -r /dev/tty ] && [ -w /dev/tty ] \
		|| fail "installation confirmation requires an interactive terminal; rerun with --yes for unattended installation"
	printf 'Continue with Remnanode Lite Native installation? [y/N] ' >/dev/tty
	confirmation=
	IFS= read -r confirmation </dev/tty \
		|| fail "cannot read the installation confirmation"
	case "$confirmation" in
		y|Y|yes|YES|Yes) ;;
		*) fail "installation cancelled" ;;
	esac
fi

if [ -n "$secret_file" ]; then
	regular_file "$secret_file" || fail "--secret-file must name a regular non-symlink file"
	[ -r "$secret_file" ] || fail "--secret-file is not readable"
	secret_file=$(absolute_file_path "$secret_file") \
		|| fail "cannot resolve --secret-file"
elif [ "$prepare_only" -eq 0 ] \
	&& [ ! -e /etc/remnanode-lite/secret.key ] \
	&& [ ! -L /etc/remnanode-lite/secret.key ]; then
	[ -r /dev/tty ] && [ -w /dev/tty ] \
		|| fail "no installed Secret exists; use --secret-file or run from an interactive terminal"
	command -v stty >/dev/null 2>&1 || fail "required command not found for interactive Secret input: stty"
	printf 'Panel Secret: ' >/dev/tty
	stty -echo </dev/tty || fail "cannot disable terminal echo"
	tty_echo_disabled=1
	secret_value=
	IFS= read -r secret_value </dev/tty || {
		restore_tty
		printf '\n' >/dev/tty
		fail "cannot read the Panel Secret"
	}
	restore_tty
	printf '\n' >/dev/tty
	[ -n "$secret_value" ] || fail "the Panel Secret cannot be empty"
	temporary_secret=$temporary_directory/secret.key
	(umask 077; printf '%s\n' "$secret_value" >"$temporary_secret") \
		|| fail "cannot create the temporary Secret file"
	unset secret_value
	secret_file=$temporary_secret
fi

if [ "$archive_install" -eq 1 ]; then
	set -- install --bundle "$bundle_path" --sha256 "$expected_sha256"
else
	set -- install --bundle-root "$bundle_root"
fi
[ -z "$expected_version" ] || set -- "$@" --expected-version "$expected_version"
[ -z "$port" ] || set -- "$@" --port "$port"
[ -z "$secret_file" ] || set -- "$@" --secret-file "$secret_file"
[ "$prepare_only" -eq 0 ] || set -- "$@" --prepare-only

printf 'Installing from the verified linux/%s Native bundle...\n' "$architecture"
"$control_binary" "$@"
