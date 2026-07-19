#!/usr/bin/env bash
set -euo pipefail

: "${GITHUB_PATH:?GITHUB_PATH is required}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"

readonly SHELLCHECK_VERSION=0.11.0
readonly SHELLCHECK_LINUX_X86_64_SHA256=8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198
readonly SHELLCHECK_LINUX_AARCH64_SHA256=12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588

case "$(uname -m)" in
  x86_64)
    shellcheck_arch=x86_64
    shellcheck_sha256=$SHELLCHECK_LINUX_X86_64_SHA256
    ;;
  aarch64|arm64)
    shellcheck_arch=aarch64
    shellcheck_sha256=$SHELLCHECK_LINUX_AARCH64_SHA256
    ;;
  *)
    echo "unsupported CI runner architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

download_dir="$(mktemp -d "$RUNNER_TEMP/remnanode-ci-checks.XXXXXX")"
trap 'rm -rf "$download_dir"' EXIT

archive="shellcheck-v${SHELLCHECK_VERSION}.linux.${shellcheck_arch}.tar.xz"
curl --fail --location --silent --show-error \
  --proto '=https' --tlsv1.2 --retry 3 --retry-all-errors \
  "https://github.com/koalaman/shellcheck/releases/download/v${SHELLCHECK_VERSION}/${archive}" \
  -o "$download_dir/$archive"
printf '%s  %s\n' "$shellcheck_sha256" "$download_dir/$archive" \
  | sha256sum --check --strict
tar -xJf "$download_dir/$archive" -C "$download_dir"

tool_bin="$RUNNER_TEMP/remnanode-ci-tools/bin"
install -d "$tool_bin"
install -m 0755 \
  "$download_dir/shellcheck-v${SHELLCHECK_VERSION}/shellcheck" \
  "$tool_bin/shellcheck"

go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
go_bin="$(go env GOPATH)/bin"

printf '%s\n' "$tool_bin" "$go_bin" >>"$GITHUB_PATH"

"$tool_bin/shellcheck" --version
"$go_bin/actionlint" -version
[ -x "$go_bin/govulncheck" ] || {
  echo "govulncheck installation did not produce an executable" >&2
  exit 1
}
