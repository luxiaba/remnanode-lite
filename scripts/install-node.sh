#!/usr/bin/env bash
# remnanode-lite one-command installer
# shellcheck source-path=SCRIPTDIR
set -Eeuo pipefail

VERSION="2.8.0-rnl.1"
PREFIX="/usr/local/bin"
ETC_DIR="/etc/remnanode"
DATA_DIR="/var/lib/remnanode"
LOG_DIR="/var/log/remnanode"
UNIT="/etc/systemd/system/remnawave-node.service"
BIN_NAME="remnanode-lite"
NODE_ENV="${ETC_DIR}/node.env"
SECRET_FILE="${ETC_DIR}/secret.key"
SERVICE_USER="remnanode"
SERVICE_GROUP="remnanode"
REPO="${RNL_REPO:-luxiaba/remnanode-lite}"  # must match internal/version/version.go releaseRepo
BOOTSTRAP_TAG="${RNL_TAG:-v${VERSION}}"

bootstrap_helper_is_trusted() {
  local script="$1" helper="$2" script_owner helper_owner trusted_uid
  local logical_dir physical_dir current owner mode links size function_name
  [ -f "$script" ] && [ ! -L "$script" ] \
    && [ -f "$helper" ] && [ ! -L "$helper" ] || return 1
  script_owner="$(stat -c '%u:%g' "$script" 2>/dev/null \
    || stat -f '%u:%g' "$script" 2>/dev/null)" || return
  helper_owner="$(stat -c '%u:%g' "$helper" 2>/dev/null \
    || stat -f '%u:%g' "$helper" 2>/dev/null)" || return
  trusted_uid="${script_owner%%:*}"
  [ "${helper_owner%%:*}" = "$trusted_uid" ] || return 1
  for current in "$script" "$helper"; do
    mode="$(stat -c '%a' "$current" 2>/dev/null \
      || stat -f '%Lp' "$current" 2>/dev/null)" || return
    links="$(stat -c '%h' "$current" 2>/dev/null \
      || stat -f '%l' "$current" 2>/dev/null)" || return
    [[ "$mode" =~ ^[0-7]{3,4}$ ]] && [ $((8#$mode & 022)) -eq 0 ] \
      && [ "$links" = 1 ] || return 1
  done
  logical_dir="$(cd "$(dirname "$helper")" && pwd -L)" || return
  physical_dir="$(cd "$(dirname "$helper")" && pwd -P)" || return
  [ "$logical_dir" = "$physical_dir" ] || return 1
  current="$physical_dir"
  while :; do
    [ -d "$current" ] && [ ! -L "$current" ] || return 1
    owner="$(stat -c '%u:%g' "$current" 2>/dev/null \
      || stat -f '%u:%g' "$current" 2>/dev/null)" || return
    mode="$(stat -c '%a' "$current" 2>/dev/null \
      || stat -f '%Lp' "$current" 2>/dev/null)" || return
    { [ "${owner%%:*}" = 0 ] || [ "${owner%%:*}" = "$trusted_uid" ]; } \
      && [[ "$mode" =~ ^[0-7]{3,4}$ ]] \
      && [ $((8#$mode & 022)) -eq 0 ] || return 1
    [ "$current" = / ] && break
    current="$(dirname "$current")" || return
  done
  size="$(wc -c <"$helper" | tr -d '[:space:]')" || return
  [[ "$size" =~ ^[0-9]+$ ]] && [ "$size" -le 1048576 ] || return 1
  for function_name in \
    installer_acquire_lock installer_run_nested installer_run_without_lock; do
    grep -Eq "^${function_name}\\(\\) [({]$" "$helper" || return 1
  done
}

if ! command -v curl >/dev/null 2>&1; then
  echo "Required command not found: curl" >&2
  exit 1
fi
if [ -n "${BASH_SOURCE[0]:-}" ] \
  && _HELPERS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)" \
  && bootstrap_helper_is_trusted \
    "${BASH_SOURCE[0]}" "${_HELPERS_DIR}/install-env-helpers.sh"; then
  # shellcheck source=install-env-helpers.sh
  source "${_HELPERS_DIR}/install-env-helpers.sh"
else
  if ! [[ "$REPO" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] \
    || ! [[ "$BOOTSTRAP_TAG" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
    echo "Invalid RNL_REPO or RNL_TAG; refusing to download the bootstrap helper" >&2
    exit 2
  fi
  _HELPERS_TMP="$(mktemp -d /var/tmp/remnanode-bootstrap.XXXXXX)"
  trap 'rm -rf "${_HELPERS_TMP:-}"' EXIT
  set +o pipefail
  curl --fail --location --silent --show-error --proto '=https' --tlsv1.2 \
    --connect-timeout 15 --max-time 60 --speed-limit 1024 --speed-time 30 \
    --max-filesize 1048576 \
    "https://raw.githubusercontent.com/${REPO}/${BOOTSTRAP_TAG}/scripts/install-env-helpers.sh" \
    | head -c 1048577 >"${_HELPERS_TMP}/install-env-helpers.sh"
  _HELPERS_DOWNLOAD_STATUS=("${PIPESTATUS[@]}")
  set -o pipefail
  _HELPERS_DOWNLOAD_BYTES="$(wc -c <"${_HELPERS_TMP}/install-env-helpers.sh" | tr -d '[:space:]')"
  if [ "${_HELPERS_DOWNLOAD_STATUS[0]:-1}" -ne 0 ] \
    || [ "${_HELPERS_DOWNLOAD_STATUS[1]:-1}" -ne 0 ] \
    || [ "$_HELPERS_DOWNLOAD_BYTES" -gt 1048576 ]; then
    echo "Bootstrap helper download failed or exceeded the 1048576-byte hard limit" >&2
    exit 1
  fi
  for _HELPERS_FUNCTION in \
    installer_acquire_lock installer_run_nested installer_run_without_lock; do
    grep -Eq "^${_HELPERS_FUNCTION}\\(\\) [({]$" \
      "${_HELPERS_TMP}/install-env-helpers.sh" || {
      echo "Bootstrap helper is missing lock API: ${_HELPERS_FUNCTION}" >&2
      exit 1
    }
  done
  # shellcheck source=install-env-helpers.sh
  source "${_HELPERS_TMP}/install-env-helpers.sh"
  rm -rf "${_HELPERS_TMP}"
  trap - EXIT
fi
TAG="$(resolve_install_tag "$REPO" "v${VERSION}")"
INSTALL_XRAY="${RNL_INSTALL_XRAY:-1}"
SKIP_XRAY="${RNL_SKIP_XRAY:-0}"
SECRET_FILE_ARG=""

YES=0
DRY_RUN=0
LOW_MEMORY=0
PORT_EXPLICIT=0
ACTION=""
UNINSTALL_MODE=""
STAGE="initialization"
DELEGATE_TO_UPGRADE=0

usage() {
  cat <<EOF
Usage: install-node.sh [options]

Remnanode Lite ${VERSION} - install, upgrade, or uninstall

With no arguments, an interactive menu is shown when a terminal is available.
For non-interactive use, specify an action:
  --install           Perform a fresh install; use a transactional upgrade when a complete install exists
  --upgrade           Transactionally upgrade the node, service, and support files (preserves rw-core by default)
  --uninstall         Uninstall

Other options:
  --yes, -y           Skip confirmation prompts
  --dry-run           Preview the operation
  --skip-xray         Skip rw-core installation
  --low-memory        Enable low-memory mode
  --port PORT         Set the listening port (default: 2222)
  --secret-file PATH  Import the Secret Key from a file
  --help, -h          Show this help
  --version           Show the version

One-command installation (recommended):
  curl -fsSL https://raw.githubusercontent.com/${REPO}/v${VERSION}/scripts/install-node.sh | sudo bash
EOF
}

version() {
  echo "remnanode-lite install ${VERSION}"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --install) ACTION=install ;;
    --upgrade) ACTION=upgrade ;;
    --uninstall) ACTION=uninstall ;;
    --menu) ACTION=menu ;;
    --yes|-y) YES=1 ;;
    --dry-run) DRY_RUN=1 ;;
    --skip-xray) SKIP_XRAY=1 ;;
    --low-memory) LOW_MEMORY=1 ;;
    --port)
      NODE_PORT="${2:-}"
      if [ -z "$NODE_PORT" ]; then
        echo "--port requires a port number" >&2
        exit 1
      fi
      PORT_EXPLICIT=1
      shift 2
      continue
      ;;
    --secret-file)
      SECRET_FILE_ARG="${2:-}"
      if [ -z "$SECRET_FILE_ARG" ]; then
        echo "--secret-file requires a file path" >&2
        exit 1
      fi
      shift 2
      continue
      ;;
    --help|-h) usage; exit 0 ;;
    --version) version; exit 0 ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
  shift
done

on_error() {
  local status="${1:-1}"
  local command="${2:-unknown}"
  echo "Installation failed during: ${STAGE}" >&2
  echo "Failed command: ${command}" >&2
  exit "$status"
}

trap 'on_error $? "$BASH_COMMAND"' ERR

step() {
  STAGE="$1"
  echo "==> $1"
}

run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] $*"
  else
    "$@"
  fi
}

read_tty() {
  local _var="$1"
  local _prompt="${2:-}"
  local _line=""
  if [ -n "$_prompt" ]; then
    if [ -t 0 ]; then
      read -r -p "$_prompt" _line || _line=""
    elif [ -r /dev/tty ]; then
      read -r -p "$_prompt" _line </dev/tty || _line=""
    else
      return 1
    fi
  else
    if [ -t 0 ]; then
      read -r _line || _line=""
    elif [ -r /dev/tty ]; then
      read -r _line </dev/tty || _line=""
    else
      return 1
    fi
  fi
  printf -v "$_var" '%s' "$_line"
}

script_dir() {
  if [ -n "${BASH_SOURCE[0]:-}" ]; then
    cd "$(dirname "${BASH_SOURCE[0]}")" && pwd
  else
    echo ""
  fi
}

run_sibling_script() {
  local name="$1"
  shift
  local dir
  dir="$(script_dir)"
  if [ -n "$dir" ] && [ -f "${dir}/${name}" ]; then
    installer_run_nested bash "${dir}/${name}" "$@"
  else
    local support
    support="$(installed_support_file "scripts/${name}")"
    if [ ! -f "$support" ]; then
      echo "Verified support script not found: ${support}" >&2
      return 1
    fi
    installer_run_nested bash "$support" "$@"
  fi
}

run_upgrade_transaction() {
  local upgrade_xray=1
  local -a args=(--yes)
  if [ "$SKIP_XRAY" -eq 1 ] || [ "$INSTALL_XRAY" -eq 0 ]; then
    upgrade_xray=0
  fi
  if [ "$DRY_RUN" -eq 1 ]; then
    args+=(--dry-run)
  fi
  if [ "$LOW_MEMORY" -eq 1 ]; then
    args+=(--low-memory)
  fi

  echo "Complete installation detected; delegating to upgrade.sh for a rollback-capable upgrade."
  RNL_REPO="$REPO" RNL_TAG="$TAG" RNL_UPGRADE_XRAY="$upgrade_xray" \
    RNL_ENSURE_SERVICE_STARTED=1 RNL_ENSURE_SERVICE_ENABLED=1 \
    run_sibling_script upgrade.sh "${args[@]}"
}

run_explicit_upgrade() {
  local -a args=(--yes)
  if [ "$DRY_RUN" -eq 1 ]; then
    args+=(--dry-run)
  fi
  if [ "$LOW_MEMORY" -eq 1 ]; then
    args+=(--low-memory)
  fi
  RNL_REPO="$REPO" RNL_TAG="$TAG" RNL_UPGRADE_XRAY=0 \
    run_sibling_script upgrade.sh "${args[@]}"
}

run_selected_uninstall() {
  local -a args
  if [ "${UNINSTALL_MODE:-}" = "full" ]; then
    args=(--full)
  else
    args=(--keep-config --yes)
  fi
  if [ "$DRY_RUN" -eq 1 ]; then
    args+=(--dry-run)
  fi
  run_sibling_script uninstall.sh "${args[@]}"
}

show_menu() {
  echo
  echo "Remnanode Lite ${VERSION} (contract 2.8.0)"
  echo "  1) Install"
  echo "  2) Upgrade"
  echo "  3) Uninstall"
  echo "  4) Exit"
  echo
  local choice=""
  read_tty choice "Select an option [1-4]: " || {
    echo "Unable to read input. For non-interactive use, specify --install, --upgrade, or --uninstall." >&2
    exit 1
  }
  case "$choice" in
    1) ACTION=install ;;
    2) ACTION=upgrade ;;
    3) ACTION=uninstall ;;
    4) exit 0 ;;
    *)
      echo "Invalid selection: ${choice}" >&2
      exit 1
      ;;
  esac
}

show_uninstall_menu() {
  echo
  echo "Uninstall options:"
  echo "  1) Remove the service only (preserve node.env and rw-core)"
  echo "  2) Remove everything (configuration, logs, and rw-core)"
  echo "  3) Back"
  local choice=""
  read_tty choice "Select an option [1-3]: " || exit 1
  case "$choice" in
    1) UNINSTALL_MODE=keep ;;
    2) UNINSTALL_MODE=full ;;
    3) exit 0 ;;
    *)
      echo "Invalid selection" >&2
      exit 1
      ;;
  esac
}

dispatch_action() {
  case "$ACTION" in
    install) do_install ;;
    upgrade) run_explicit_upgrade ;;
    uninstall)
      show_uninstall_menu
      run_selected_uninstall
      ;;
    menu) show_menu; dispatch_action ;;
    *)
      echo "Unknown action: ${ACTION}" >&2
      usage
      exit 1
      ;;
  esac
}

require_root() {
  if [ "$DRY_RUN" -eq 1 ]; then
    return 0
  fi
  if [ "$(id -u)" -ne 0 ]; then
    echo "Run this script as root: sudo bash install-node.sh" >&2
    exit 1
  fi
}

redirect_alpine() {
  if [ "$DRY_RUN" -eq 1 ]; then
    return 0
  fi
  if [ -f /etc/alpine-release ]; then
    echo "Alpine Linux detected. Use the dedicated installer:"
    echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/${TAG}/scripts/install-node-alpine.sh | bash"
    exit 1
  fi
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required command not found: $1" >&2
    exit 1
  fi
}

validate_port() {
  local port="$1"
  if ! [[ "$port" =~ ^[0-9]+$ ]] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
    echo "Invalid port: ${port} (valid range: 1-65535)" >&2
    exit 1
  fi
}

effective_node_port() {
  echo "${NODE_PORT:-2222}"
}

configured_node_port() {
  local configured
  configured="$(read_env_value NODE_PORT "$NODE_ENV")"
  if [ -n "$configured" ]; then
    printf '%s\n' "$configured"
    return 0
  fi
  effective_node_port
}

prompt_node_port() {
  if [ -n "${NODE_PORT:-}" ] || [ "$YES" -eq 1 ] || [ "$DRY_RUN" -eq 1 ]; then
    return 0
  fi
  echo
  local input=""
  if [ -t 0 ]; then
    read -r -p "Node listening port (used by the Panel; default: 2222): " input || input=""
  elif [ -r /dev/tty ]; then
    read -r -p "Node listening port (used by the Panel; default: 2222): " input </dev/tty || input=""
  fi
  NODE_PORT="${input:-2222}"
  validate_port "$NODE_PORT"
}

confirm_install() {
  if [ ! -x "${PREFIX}/${BIN_NAME}" ] || [ ! -f "$NODE_ENV" ] \
    || [ ! -f "$UNIT" ] || [ -L "$UNIT" ]; then
    if [ -x "${PREFIX}/${BIN_NAME}" ] || [ -f "$NODE_ENV" ] || [ -e "$UNIT" ]; then
      echo "Incomplete installation detected; continuing installation recovery instead of delegating to a stopped-state upgrade."
    fi
    return 0
  fi

  if [ "$PORT_EXPLICIT" -eq 1 ] || [ -n "$SECRET_FILE_ARG" ]; then
    echo "A transactional upgrade of an existing installation does not accept --port or --secret-file. Upgrade first, then edit ${NODE_ENV} separately." >&2
    return 1
  fi

  if [ "$YES" -eq 1 ] || [ "$DRY_RUN" -eq 1 ]; then
    DELEGATE_TO_UPGRADE=1
    return 0
  fi
  echo
  echo "An existing remnanode-lite installation was detected."
  echo "  1) Upgrade (preserve ${NODE_ENV})"
  echo "  2) Fresh install (remove configuration and logs before reinstalling)"
  echo "  3) Cancel"
  local choice=""
  read_tty choice "Select an option [1-3]: " || {
    echo "For non-interactive use, specify --yes or select upgrade from the menu." >&2
    exit 1
  }
  case "$choice" in
    1) DELEGATE_TO_UPGRADE=1 ;;
    2)
      if [ "$DRY_RUN" -eq 1 ]; then
        echo "[dry-run] Remove ${ETC_DIR} ${LOG_DIR} ${DATA_DIR}"
      else
        local configured_xray previous_port
        configured_xray="$(read_env_value XRAY_BIN "$NODE_ENV")" || return
        [ -n "$configured_xray" ] || configured_xray=/usr/local/lib/remnanode/rw-core
        configured_xray="$(canonical_binary_path "$configured_xray")" || return
        previous_port="$(configured_node_port)" || return
        stop_for_fresh_reinstall systemd "${PREFIX}/${BIN_NAME}" \
          "$configured_xray" "$previous_port" || return
        rm -rf "$ETC_DIR" "$LOG_DIR" "$DATA_DIR"
        cleanup_runtime
        rm -f "${ETC_DIR}.bak."* 2>/dev/null || true
        echo "Existing configuration removed; starting a fresh installation."
      fi
      ;;
    *)
      echo "Cancelled."
      exit 0
      ;;
  esac
}

update_node_port_in_env() {
  local port="$1"
  validate_port "$port"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] Update ${NODE_ENV}: NODE_PORT=${port}"
    return 0
  fi
  set_env_value NODE_PORT "$port"
  echo "Set NODE_PORT=${port}"
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *)
      echo "Unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

install_packages() {
  step "Install runtime dependencies"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] apt-get install ca-certificates curl tar unzip iproute2 nftables util-linux"
    return 0
  fi
  require_free_bytes / 536870912 "system dependencies and installer workspace"
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive \
      apt-get install --yes --no-install-recommends \
      ca-certificates curl tar unzip iproute2 nftables util-linux
    apt-get clean
    return 0
  fi
  for command in curl tar unzip ss nft; do
    require_command "$command"
  done
}

download_binary() {
  local arch="$1"
  step "Download ${BIN_NAME} ${TAG} (linux/${arch})"
  install_release_binary "$REPO" "$TAG" "$arch" "${PREFIX}/${BIN_NAME}"
}

install_xray() {
  if [ "$SKIP_XRAY" -eq 1 ] || [ "$INSTALL_XRAY" -eq 0 ]; then
    echo "Skipping rw-core installation."
    return 0
  fi

  step "Install rw-core (Xray core)"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] Run the verified install-xray.sh from the target release"
    return 0
  fi
  local support
  support="$(installed_support_file scripts/install-xray.sh)"
  [ -f "$support" ] || { echo "Verified install-xray.sh not found" >&2; return 1; }
  RNL_REPO="$REPO" RNL_TAG="$TAG" installer_run_nested bash "$support"
}

setup_directories() {
  step "Create directories"
  setup_service_directories
}

setup_env_file() {
  step "Configure ${NODE_ENV}"
  local port
  port="$(effective_node_port)"
  validate_port "$port"

  if [ -f "$NODE_ENV" ]; then
    if [ "$PORT_EXPLICIT" -eq 1 ] || [ -n "${NODE_PORT:-}" ]; then
      update_node_port_in_env "$port"
    else
      echo "Preserving existing configuration: ${NODE_ENV} (NODE_PORT=$(configured_node_port))"
    fi
    if [ "$LOW_MEMORY" -eq 1 ]; then
      set_env_value LOW_MEMORY 1
      echo "Enabled LOW_MEMORY=1"
    fi
    return 0
  fi

  local low_mem="${LOW_MEMORY:-0}"
  if [ "$LOW_MEMORY" -eq 1 ]; then
    low_mem=1
  fi

  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] Create ${NODE_ENV}"
    return 0
  fi

  render_env_template "$port" "$low_mem" "install-node.sh" >"$NODE_ENV"
  secure_config_file "$NODE_ENV"
  echo "Created ${NODE_ENV}"
}

setup_secret_file() {
  step "Configure the Secret Key"

  migrate_inline_secret_to_file

  if secret_configured; then
    if [ -s "$SECRET_FILE" ]; then
      validate_secret_file "$SECRET_FILE"
    fi
    if secret_from_env_file; then
      echo "Preserving existing SECRET_KEY in ${NODE_ENV}"
    else
      echo "Preserving existing Secret Key: ${SECRET_FILE}"
    fi
    return 0
  fi

  if [ -n "$SECRET_FILE_ARG" ]; then
    if [ ! -f "$SECRET_FILE_ARG" ]; then
      echo "Path specified by --secret-file not found: ${SECRET_FILE_ARG}" >&2
      exit 1
    fi
    write_secret_from_source "$SECRET_FILE_ARG"
    echo "Imported the Secret Key from a file (SECRET_KEY_FILE mode)."
    return 0
  fi

  prompt_secret_key
}

install_systemd() {
  step "Install the systemd service"

  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] Install ${UNIT}"
    return 0
  fi

  local support
  support="$(installed_support_file deploy/remnawave-node.service)" || return
  [ -f "$support" ] || { echo "Verified systemd unit not found" >&2; return 1; }
  install_managed_file "$support" "$UNIT" 0644 || return

  systemctl daemon-reload || return
  systemctl enable remnawave-node.service || return
}

install_log_helper_command() (
  local target="$1" log_file="$2" tmp=""

  validate_managed_parent_path "$target" || return
  if [ -e "$target" ] || [ -L "$target" ]; then
    validate_managed_regular_file "$target" || return
  fi

  tmp="$(mktemp "$(dirname "$target")/.$(basename "$target").XXXXXX")" || return
  trap 'if [ -n "${tmp:-}" ]; then rm -f -- "$tmp"; fi' EXIT
  [ -f "$tmp" ] && [ ! -L "$tmp" ] || {
    echo "Log helper staging path is not a regular file: ${tmp}" >&2
    return 1
  }
  printf '%s\n' '#!/bin/sh' "exec tail -n +1 -f ${log_file}" >"$tmp" || return
  chmod 0755 "$tmp" || return
  chown root:root "$tmp" || return
  validate_managed_regular_file "$tmp" || return
  mv -f -- "$tmp" "$target" || return
  tmp=""
  validate_managed_regular_file "$target"
)

install_helpers() {
  step "Install log helper commands"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] remnanode-xlogs / remnanode-xerrors"
    return 0
  fi

  install_log_helper_command "${PREFIX}/remnanode-xlogs" \
    /var/log/remnanode/xray.out.log || return
  install_log_helper_command "${PREFIX}/remnanode-xerrors" \
    /var/log/remnanode/xray.err.log || return
}

start_service() {
  if ! secret_configured; then
    echo "Warning: Secret Key is not configured; the service will not be started."
    echo "  Write the Secret Key to ${SECRET_FILE}, verify NODE_PORT in ${NODE_ENV}, then run: systemctl restart remnawave-node"
    return 0
  fi

  step "Start the remnawave-node service"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] systemctl restart remnawave-node"
    return 0
  fi

  systemctl restart remnawave-node.service
  sleep 1
  installer_run_without_lock systemctl --no-pager status remnawave-node.service || true
}

main() {
  require_root
  if [ -z "$ACTION" ]; then
    show_menu
  fi
  if [ "$DRY_RUN" -eq 0 ]; then
    installer_acquire_lock || return
  fi
  dispatch_action
}

detect_low_memory_auto() {
  if [ "$LOW_MEMORY" -eq 1 ]; then
    return 0
  fi
  local total_kb=""
  total_kb="$(awk '/MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null || true)"
  if [ -n "$total_kb" ] && [ "$total_kb" -le 524288 ]; then
    LOW_MEMORY=1
    echo "Detected ${total_kb} KB of memory (<= 512 MB); enabling low-memory mode with LOW_MEMORY=1"
  fi
}

do_install() {
  require_root
  redirect_alpine
  require_command curl
  require_command systemctl

	confirm_install
	if [ "$DELEGATE_TO_UPGRADE" -eq 1 ]; then
		run_upgrade_transaction
		return 0
	fi
	detect_low_memory_auto

	local arch
	arch="$(detect_arch)"
	install_packages
	require_command head
	require_command tar
	require_command timeout
	ensure_service_account
  setup_directories
  print_pre_install_panel_hint
  download_binary "$arch"
  prompt_node_port
  setup_env_file
  normalize_runtime_environment
  ensure_internal_socket_in_env
  setup_secret_file
  install_xray
  migrate_owned_asset_paths
  install_geo_extra_files
  normalize_service_permissions
  install_systemd
  install_helpers
  start_service
  if secret_configured; then
    verify_service_listening "$(configured_node_port)"
    print_panel_address_hint "$(configured_node_port)"
  fi

  echo
  echo "Installation complete."
  echo "  Binary:        ${PREFIX}/${BIN_NAME}"
  echo "  Environment:   ${NODE_ENV}"
  echo "  Listening port: $(configured_node_port) (configure the same port in the Panel)"
  echo "  Configuration: ${NODE_ENV} (NODE_PORT + SECRET_KEY_FILE)"
  echo "  Logs:          journalctl -u remnawave-node -f"
  echo "  Xray logs:     remnanode-xlogs / remnanode-xerrors"
  echo "  Management:    Run install-node.sh again to upgrade or uninstall"
  if ! secret_configured; then
    print_env_config_hint "sudo systemctl restart remnawave-node"
  fi
}

main "$@"
