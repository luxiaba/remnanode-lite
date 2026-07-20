#!/usr/bin/env bash
# remnanode-lite uninstaller (systemd / Alpine OpenRC)
# shellcheck source-path=SCRIPTDIR
set -Eeuo pipefail

VERSION="2.8.0-rnl.1"
PREFIX="/usr/local/bin"
BIN_NAME="remnanode-lite"
RUN_WRAPPER="${PREFIX}/remnawave-node-run"
XLOGS="${PREFIX}/remnanode-xlogs"
XERRORS="${PREFIX}/remnanode-xerrors"
UNIT="/etc/systemd/system/remnawave-node.service"
OPENRC_SVC="/etc/init.d/remnawave-node"
ETC_DIR="/etc/remnanode"
LOG_DIR="/var/log/remnanode"
DATA_DIR="/var/lib/remnanode"
OWNED_LIB_DIR="/usr/local/lib/remnanode"
OWNED_SHARE_DIR="/usr/local/share/remnanode"
GEO_DIR="${OWNED_SHARE_DIR}/xray"
ASN_DIR="${OWNED_SHARE_DIR}/asn"
XRAY_BIN="${OWNED_LIB_DIR}/rw-core"
RNL_REPO="${RNL_REPO:-luxiaba/remnanode-lite}"
RNL_TAG="${RNL_TAG:-v${VERSION}}"

YES=0
DRY_RUN=0
PURGE_CONFIG=0
PURGE_LOGS=0
PURGE_DATA=0
PURGE_XRAY=0
STAGE="Initialization"
CONFIGURED_XRAY_BIN="$XRAY_BIN"

usage() {
  cat <<EOF
Usage: uninstall.sh [options]

Uninstall Remnanode Lite ${VERSION}

Options:
  --yes, -y           Skip confirmation (non-interactive)
  --dry-run           Preview what would be removed
  --purge             Remove configuration, logs, and data (preserve rw-core)
  --purge-all         Remove everything, including rw-core and geo data
  --full              Full uninstall (equivalent to --purge-all --yes)
  --keep-config       Remove only the service and binary; preserve ${ETC_DIR}
  --help, -h          Show this help

Interactive mode prompts separately before removing configuration, logs, data,
or rw-core. Alpine uses OpenRC; other distributions use systemd.
EOF
}

version() {
  echo "remnanode-lite uninstall ${VERSION}"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --yes|-y) YES=1 ;;
    --dry-run) DRY_RUN=1 ;;
    --purge)
      PURGE_CONFIG=1
      PURGE_LOGS=1
      PURGE_DATA=1
      ;;
    --purge-all)
      PURGE_CONFIG=1
      PURGE_LOGS=1
      PURGE_DATA=1
      PURGE_XRAY=1
      ;;
    --full)
      PURGE_CONFIG=1
      PURGE_LOGS=1
      PURGE_DATA=1
      PURGE_XRAY=1
      YES=1
      ;;
    --keep-config) PURGE_CONFIG=0 ;;
    --help|-h) usage; exit 0 ;;
    --version) version; exit 0 ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
  shift
done

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

bootstrap_run_without_installer_lock() (
  local fd="${RNL_INSTALLER_LOCK_FD:-${INSTALLER_LOCK_FD:-}}"
  if [[ "$fd" =~ ^[0-9]+$ ]] && [ "${#fd}" -le 6 ] && [ "$fd" -ge 10 ]; then
    exec {fd}>&-
  fi
  unset RNL_INSTALLER_LOCK_FD RNL_INSTALLER_LOCK_ID
  "$@"
)

bootstrap_path_owner_ids() {
  local path="$1" owner
  if owner="$(stat -c '%u:%g' "$path" 2>/dev/null)"; then
    printf '%s' "$owner"
    return 0
  fi
  stat -f '%u:%g' "$path" 2>/dev/null
}

bootstrap_path_mode() {
  local path="$1" mode
  if mode="$(stat -c '%a' "$path" 2>/dev/null)"; then
    printf '%s' "$mode"
    return 0
  fi
  stat -f '%Lp' "$path" 2>/dev/null
}

bootstrap_installed_helpers() {
  local link=/usr/local/lib/remnanode/support-current target base component
  local helper mode links current=/
  local -a components
  [ -L "$link" ] && [ "$(bootstrap_path_owner_ids "$link")" = 0:0 ] || return 1
  target="$(readlink "$link")" || return
  [[ "$target" =~ ^support/[A-Za-z0-9][A-Za-z0-9._-]*$ ]] || return 1
  [ "$target" = "support/${RNL_TAG}" ] || return 1
  base="$(dirname "$link")/$target"
  IFS=/ read -r -a components <<<"${base#/}"
  for component in "${components[@]}" scripts; do
    current="${current%/}/${component}"
    [ -d "$current" ] && [ ! -L "$current" ] \
      && [ "$(bootstrap_path_owner_ids "$current")" = 0:0 ] || return 1
    mode="$(bootstrap_path_mode "$current")" || return
    [[ "$mode" =~ ^[0-7]{3,4}$ ]] && [ $((8#$mode & 022)) -eq 0 ] || return 1
  done
  helper="${base}/scripts/install-env-helpers.sh"
  [ -f "$helper" ] && [ ! -L "$helper" ] \
    && [ "$(bootstrap_path_owner_ids "$helper")" = 0:0 ] || return 1
  if links="$(stat -c '%h' "$helper" 2>/dev/null)"; then
    :
  else
    links="$(stat -f '%l' "$helper" 2>/dev/null)" || return
  fi
  [ "$links" = 1 ] || return 1
  mode="$(bootstrap_path_mode "$helper")" || return
  [[ "$mode" =~ ^[0-7]{3,4}$ ]] && [ $((8#$mode & 022)) -eq 0 ] || return 1
  bootstrap_helper_is_trusted "$helper" "$helper" || return
  printf '%s' "$helper"
}

load_installer_helpers() {
  local helpers_dir helpers_tmp helpers_bytes installed_helpers function_name
  local -a helpers_status
  if [ -n "${BASH_SOURCE[0]:-}" ]; then
    helpers_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if bootstrap_helper_is_trusted \
      "${BASH_SOURCE[0]}" "${helpers_dir}/install-env-helpers.sh"; then
      # shellcheck source=install-env-helpers.sh
      source "${helpers_dir}/install-env-helpers.sh"
      return
    fi
  fi
  if installed_helpers="$(bootstrap_installed_helpers)"; then
    # shellcheck disable=SC1090
    source "$installed_helpers"
    return
  fi
  if ! [[ "$RNL_REPO" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$ ]] \
    || ! [[ "$RNL_TAG" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
    echo "Invalid RNL_REPO or RNL_TAG; refusing to download the installer helper" >&2
    return 2
  fi
  if ! command -v curl >/dev/null 2>&1 || ! command -v head >/dev/null 2>&1; then
    echo "The standalone uninstaller requires curl and head to fetch the installer helper" >&2
    return 1
  fi
  helpers_tmp="$(mktemp -d /var/tmp/remnanode-bootstrap.XXXXXX)" || return
  set +o pipefail
  bootstrap_run_without_installer_lock \
    curl --fail --location --silent --show-error --proto '=https' --tlsv1.2 \
    --connect-timeout 15 --max-time 60 --speed-limit 1024 --speed-time 30 \
    --max-filesize 1048576 \
    "https://raw.githubusercontent.com/${RNL_REPO}/${RNL_TAG}/scripts/install-env-helpers.sh" \
    | bootstrap_run_without_installer_lock head -c 1048577 \
      >"${helpers_tmp}/install-env-helpers.sh"
  helpers_status=("${PIPESTATUS[@]}")
  set -o pipefail
  helpers_bytes="$(wc -c <"${helpers_tmp}/install-env-helpers.sh" | tr -d '[:space:]')"
  if [ "${helpers_status[0]:-1}" -ne 0 ] \
    || [ "${helpers_status[1]:-1}" -ne 0 ] \
    || [ "$helpers_bytes" -gt 1048576 ]; then
    rm -rf "$helpers_tmp"
    echo "Installer helper download failed or exceeded the 1048576-byte hard limit" >&2
    return 1
  fi
  for function_name in \
    installer_acquire_lock installer_run_nested installer_run_without_lock; do
    grep -Eq "^${function_name}\\(\\) [({]$" \
      "${helpers_tmp}/install-env-helpers.sh" || {
      rm -rf "$helpers_tmp"
      echo "Installer helper is missing lock API: ${function_name}" >&2
      return 1
    }
  done
  # shellcheck disable=SC1091
  if ! source "${helpers_tmp}/install-env-helpers.sh"; then
    rm -rf "$helpers_tmp"
    return 1
  fi
  rm -rf "$helpers_tmp"
}

if [ "$DRY_RUN" -eq 0 ] && [ "$(id -u)" -ne 0 ]; then
  echo "Run this script as root (Alpine usually has no sudo): use su - and then run bash uninstall.sh" >&2
  exit 1
fi
load_installer_helpers

on_error() {
  local status="${1:-1}"
  local command="${2:-unknown}"
  echo "Uninstall failed during: ${STAGE}" >&2
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

cleanup_runtime() {
  step "Clean project runtime files"
  run rm -rf /run/remnanode 2>/dev/null || true
  run rm -f /run/remnawave-node-supervise.pid 2>/dev/null || true
  run rm -f /run/remnawave-internal-*.sock 2>/dev/null || true
  if [ "$PURGE_CONFIG" -eq 1 ]; then
    run rm -f "${ETC_DIR}/node.env.bak."* 2>/dev/null || true
  fi
}

cleanup_firewall() {
  step "Clean project-specific nftables tables"
  if ! command -v nft >/dev/null 2>&1; then
    return 0
  fi
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] Remove ip/remnanode and ip6/remnanode6 if present"
    return 0
  fi
  if nft list table ip remnanode >/dev/null 2>&1; then
    nft delete table ip remnanode
  fi
  if nft list table ip6 remnanode6 >/dev/null 2>&1; then
    nft delete table ip6 remnanode6
  fi
}

is_alpine() {
  [ -f /etc/alpine-release ]
}

require_root() {
  if [ "$DRY_RUN" -eq 1 ]; then
    return 0
  fi
  if [ "$(id -u)" -ne 0 ]; then
    echo "Run this script as root (Alpine usually has no sudo): use su - and then run bash uninstall.sh" >&2
    exit 1
  fi
}

installed() {
  [ -x "${PREFIX}/${BIN_NAME}" ] || \
    [ -f "$UNIT" ] || \
    [ -f "$OPENRC_SVC" ] || \
    [ -d "$ETC_DIR" ]
}

detect_install_type() {
  if [ -f "$OPENRC_SVC" ] || is_alpine; then
    echo "openrc"
  elif [ -f "$UNIT" ]; then
    echo "systemd"
  else
    echo "unknown"
  fi
}

current_version() {
  if [ -x "${PREFIX}/${BIN_NAME}" ]; then
    installer_run_without_lock "${PREFIX}/${BIN_NAME}" version 2>/dev/null || echo "unknown"
  else
    echo "not installed"
  fi
}

read_env_value() {
  local key="$1" file="$2" line value
  [[ "$key" =~ ^[A-Z][A-Z0-9_]*$ ]] || return 2
  [ -f "$file" ] || return 0
  line="$(grep -E "^[[:space:]]*(export[[:space:]]+)?${key}[[:space:]]*=" "$file" 2>/dev/null | tail -n 1 || true)"
  [ -n "$line" ] || return 0
  value="${line#*=}"
  value="$(printf '%s' "$value" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
  case "$value" in
    \"*\") value=${value#\"}; value=${value%\"} ;;
    \'*\') value=${value#\'}; value=${value%\'} ;;
  esac
  printf '%s' "$value"
}

running_pids_for_binary() {
  local binary="$1" exe pid target
  for exe in /proc/[0-9]*/exe; do
    [ -e "$exe" ] || [ -L "$exe" ] || continue
    pid="${exe%/exe}"
    pid="${pid##*/}"
    if [ -e "$binary" ] && [ "$exe" -ef "$binary" ]; then
      printf '%s\n' "$pid"
      continue
    fi
    target="$(readlink "$exe" 2>/dev/null || true)"
    if [ "$target" = "$binary" ] || [ "$target" = "$binary (deleted)" ]; then
      printf '%s\n' "$pid"
    fi
  done
}

probe_uninstall_service_state() {
  local platform="$1" output="" load_state="" active_state="" status
  case "$platform" in
    openrc)
      if installer_run_without_lock \
        rc-service remnawave-node status >/dev/null 2>&1; then
        printf 'active'
        return 0
      else
        status=$?
      fi
      [ "$status" -eq 3 ] && printf 'inactive' || printf 'error'
      ;;
    systemd)
      if ! output="$(installer_run_without_lock systemctl show --no-pager \
        --property=LoadState --property=ActiveState \
        remnawave-node.service)"; then
        printf 'error'
        return 0
      fi
      while IFS='=' read -r property value; do
        case "$property" in
          LoadState) load_state="$value" ;;
          ActiveState) active_state="$value" ;;
        esac
      done <<<"$output"
      case "$load_state:$active_state" in
        loaded:active|loaded:reloading|masked:active|masked:reloading|not-found:active|not-found:reloading)
          printf 'active'
          ;;
        loaded:inactive|loaded:failed|masked:inactive|masked:failed|not-found:inactive|not-found:failed)
          printf 'inactive'
          ;;
        *) printf 'error' ;;
      esac
      ;;
    *) return 2 ;;
  esac
}

uninstall_service_manager_state() {
  local state aggregate=inactive
  if is_alpine || [ -f "$OPENRC_SVC" ]; then
    state="$(probe_uninstall_service_state openrc)" || return
    case "$state" in
      error) printf 'error'; return 0 ;;
      active) aggregate=active ;;
      inactive) ;;
      *) printf 'error'; return 0 ;;
    esac
  fi
  if [ -f "$UNIT" ]; then
    state="$(probe_uninstall_service_state systemd)" || return
    case "$state" in
      error) printf 'error'; return 0 ;;
      active) aggregate=active ;;
      inactive) ;;
      *) printf 'error'; return 0 ;;
    esac
  fi
  printf '%s' "$aggregate"
}

service_manager_active() {
  local state
  state="$(uninstall_service_manager_state)" || return 2
  case "$state" in
    active) return 0 ;;
    inactive) return 1 ;;
    *) return 2 ;;
  esac
}

wait_for_stop_confirmation() {
  local i=0 pids binary running manager_state
  while [ "$i" -lt 35 ]; do
    running=0
    manager_state="$(uninstall_service_manager_state)" || return
    case "$manager_state" in
      active) running=1 ;;
      inactive) ;;
      *)
        echo "Could not reliably determine the remnawave-node state; refusing to confirm it stopped" >&2
        return 1
        ;;
    esac
    for binary in "${PREFIX}/${BIN_NAME}" "$CONFIGURED_XRAY_BIN"; do
      pids="$(running_pids_for_binary "$binary")"
      [ -z "$pids" ] || running=1
    done
    [ "$running" -eq 0 ] && return 0
    sleep 1
    i=$((i + 1))
  done
  manager_state="$(uninstall_service_manager_state)" || return
  case "$manager_state" in
    active) echo "The service manager still reports remnawave-node as running" >&2 ;;
    inactive) ;;
    *) echo "Could not reliably confirm the remnawave-node state after stopping" >&2 ;;
  esac
  for binary in "${PREFIX}/${BIN_NAME}" "$CONFIGURED_XRAY_BIN"; do
    pids="$(running_pids_for_binary "$binary")"
    [ -z "$pids" ] || echo "Processes are still using ${binary}: ${pids//$'\n'/,}" >&2
  done
  return 1
}

prompt_yes_no() {
  local prompt="$1"
  local default="${2:-n}"
  if [ "$YES" -eq 1 ]; then
    return 0
  fi
  local hint="[y/N]"
  [ "$default" = "y" ] && hint="[Y/n]"
  local ans=""
  read_tty ans "${prompt} ${hint} " || ans=""
  ans="${ans:-$default}"
  case "$ans" in
    y|Y|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

interactive_options() {
  if [ "$YES" -eq 1 ]; then
    return 0
  fi
  if [ "$PURGE_CONFIG" -eq 1 ] || [ "$PURGE_LOGS" -eq 1 ] || [ "$PURGE_DATA" -eq 1 ] || [ "$PURGE_XRAY" -eq 1 ]; then
    return 0
  fi

  echo
  echo "----------------------------------------"
  echo " Uninstall options (Enter uses the default)"
  echo "----------------------------------------"
  echo "Current version: $(current_version)"
  echo "Installation type: $(detect_install_type)"
  echo

  prompt_yes_no "Remove configuration directory ${ETC_DIR} (node.env / secret.key)?" n && PURGE_CONFIG=1
  prompt_yes_no "Remove log directory ${LOG_DIR}?" n && PURGE_LOGS=1
  prompt_yes_no "Remove data directory ${DATA_DIR}?" n && PURGE_DATA=1
  prompt_yes_no "Remove rw-core / Xray (${XRAY_BIN}) and geo data?" n && PURGE_XRAY=1
  echo
}

print_plan() {
  echo "The following actions will be performed:"
  echo "  - Stop and remove the service ($(detect_install_type))"
  echo "  - Remove binary: ${PREFIX}/${BIN_NAME}"
  echo "  - Remove helper commands: ${XLOGS}, ${XERRORS}"
  [ "$PURGE_CONFIG" -eq 1 ] && echo "  - Remove configuration: ${ETC_DIR}"
  [ "$PURGE_LOGS" -eq 1 ] && echo "  - Remove logs: ${LOG_DIR}"
  [ "$PURGE_DATA" -eq 1 ] && echo "  - Remove data: ${DATA_DIR}"
  if [ "$PURGE_XRAY" -eq 1 ]; then
    echo "  - Remove rw-core: ${XRAY_BIN}"
    echo "  - Remove geo data: ${GEO_DIR}"
    echo "  - Remove ASN data: ${ASN_DIR}"
    echo "  - Remove only project-owned directories; preserve generic /usr/local/bin/xray and /usr/local/share/xray paths"
  fi
  echo
}

confirm_uninstall() {
  if [ "$YES" -eq 1 ] || [ "$DRY_RUN" -eq 1 ]; then
    return 0
  fi
  print_plan
  prompt_yes_no "Proceed with uninstall?" n || {
    echo "Cancelled."
    exit 0
  }
}

stop_service() {
  local stop_failed=0 openrc_state=inactive systemd_state=inactive
  step "Stop service"
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] Stop the service and confirm all remnanode-lite/rw-core processes have exited"
    return 0
  fi
  if is_alpine || [ -f "$OPENRC_SVC" ]; then
    command -v rc-service >/dev/null 2>&1 || {
      echo "An OpenRC service file exists but rc-service is unavailable; refusing to uninstall" >&2
      return 1
    }
    openrc_state="$(probe_uninstall_service_state openrc)" || return
  fi
  if [ -f "$UNIT" ]; then
    command -v systemctl >/dev/null 2>&1 || {
      echo "A systemd unit exists but systemctl is unavailable; refusing to uninstall" >&2
      return 1
    }
    systemd_state="$(probe_uninstall_service_state systemd)" || return
  fi
  if [ "$openrc_state" = error ] || [ "$systemd_state" = error ]; then
    echo "Could not reliably determine the remnawave-node state; preserving all files and data" >&2
    return 1
  fi
  [ "$openrc_state" = inactive ] || [ "$openrc_state" = active ] || return 1
  [ "$systemd_state" = inactive ] || [ "$systemd_state" = active ] || return 1
  if [ "$openrc_state" = active ]; then
    rc-service remnawave-node stop >/dev/null 2>&1 || stop_failed=1
  fi
  if [ "$systemd_state" = active ]; then
    systemctl stop remnawave-node.service >/dev/null 2>&1 || stop_failed=1
  fi
  if ! wait_for_stop_confirmation; then
    echo "The service and rw-core were not confirmed stopped; preserving firewall rules, service files, and all data" >&2
    return 1
  fi
  if [ "$stop_failed" -ne 0 ]; then
    echo "The service stop command failed; preserving firewall rules, service files, and all data" >&2
    return 1
  fi
  if is_alpine || [ -f "$OPENRC_SVC" ]; then
    rc-update del remnawave-node default 2>/dev/null || true
  fi
  if [ -f "$UNIT" ]; then
    systemctl disable remnawave-node.service >/dev/null 2>&1 || true
  fi
}

remove_service_files() {
  step "Remove service files"
  if [ -f "$OPENRC_SVC" ]; then
    run rm -f "$OPENRC_SVC"
  fi
  if [ -f "$UNIT" ]; then
    run rm -f "$UNIT"
    run systemctl daemon-reload 2>/dev/null || true
  fi
}

remove_binaries() {
  step "Remove binary and helper commands"
  run rm -f "${PREFIX}/${BIN_NAME}"
  run rm -f "${RUN_WRAPPER}"
  run rm -f "$XLOGS" "$XERRORS"
}

remove_optional_dirs() {
  if [ "$PURGE_CONFIG" -eq 1 ]; then
    step "Remove configuration ${ETC_DIR}"
    run rm -rf "$ETC_DIR"
  else
    echo "Preserving configuration: ${ETC_DIR}"
  fi

  if [ "$PURGE_LOGS" -eq 1 ]; then
    step "Remove logs ${LOG_DIR}"
    run rm -rf "$LOG_DIR"
  else
    echo "Preserving logs: ${LOG_DIR}"
  fi

  if [ "$PURGE_DATA" -eq 1 ]; then
    step "Remove data ${DATA_DIR}"
    run rm -rf "$DATA_DIR"
  else
    echo "Preserving data: ${DATA_DIR}"
  fi
}

remove_xray() {
  if [ "$PURGE_XRAY" -ne 1 ]; then
    echo "Preserving rw-core: ${XRAY_BIN}"
    return 0
  fi
  step "Remove rw-core and geo data"
  run rm -rf "$OWNED_LIB_DIR" "$OWNED_SHARE_DIR"
}

main() {
  require_root
  if [ "$DRY_RUN" -eq 0 ]; then
    installer_acquire_lock || return
  fi

  if ! installed; then
    echo "No remnanode-lite installation was detected."
    exit 0
  fi

  interactive_options
  confirm_uninstall
  print_plan

  CONFIGURED_XRAY_BIN="$(read_env_value XRAY_BIN "${ETC_DIR}/node.env")"
  [ -n "$CONFIGURED_XRAY_BIN" ] || CONFIGURED_XRAY_BIN="$XRAY_BIN"
  CONFIGURED_XRAY_BIN="$(readlink -f "$CONFIGURED_XRAY_BIN" 2>/dev/null || printf '%s' "$CONFIGURED_XRAY_BIN")"

  stop_service
  cleanup_runtime
  cleanup_firewall
  remove_service_files
  remove_binaries
  remove_optional_dirs
  remove_xray
  cleanup_runtime

  echo
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "Dry run complete; no files were removed."
  else
    echo "Uninstall complete."
    [ "$PURGE_CONFIG" -eq 0 ] && [ -d "$ETC_DIR" ] && echo "  Configuration preserved for reuse: ${ETC_DIR}"
    [ "$PURGE_XRAY" -eq 0 ] && [ -x "$XRAY_BIN" ] && echo "  rw-core preserved: ${XRAY_BIN}"
    echo "  System user remnanode was preserved for retained configuration or future reinstallation."
    echo
    echo "Reinstall:"
    if is_alpine; then
      echo "  curl -fsSL https://raw.githubusercontent.com/luxiaba/remnanode-lite/v${VERSION}/scripts/install-node-alpine.sh | bash"
    else
      echo "  curl -fsSL https://raw.githubusercontent.com/luxiaba/remnanode-lite/v${VERSION}/scripts/install-node.sh | sudo bash"
    fi
  fi
}

main "$@"
