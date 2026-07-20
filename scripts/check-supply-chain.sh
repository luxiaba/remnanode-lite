#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# shellcheck source=scripts/install-env-helpers.sh
source scripts/install-env-helpers.sh

version="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' internal/version/version.go)"
tag="v${version}"
version_output="$(go run ./cmd/remnanode-lite version)"
release_binary_version_matches_tag "$version_output" "$tag"
if release_binary_version_matches_tag "$version_output" "v${version}.invalid"; then
  echo "release version matcher accepted the wrong tag" >&2
  exit 1
fi

if grep -Eq '^[[:space:]]+ASN_SOURCE_URL:.*latest' .github/workflows/release.yml; then
  echo "release workflow ASN source must not use a floating latest reference" >&2
  exit 1
fi
grep -Fq 'ASN_SOURCE_URL: https://github.com/ipverse/as-ip-blocks/archive/56d021c7536afb15317155e45b57e7b5c87a4700.tar.gz' \
  .github/workflows/release.yml || {
  echo "release workflow ASN source is not pinned to the audited ipverse commit" >&2
  exit 1
}
grep -Fq 'ASN_SOURCE_SHA256: fc8be15bfbef3134f603276a26364935dbd2543d099dbaafa978a33b674a58ec' \
  .github/workflows/release.yml || {
  echo "release workflow ASN source digest does not match the audited commit archive" >&2
  exit 1
}

for workflow in \
  .github/workflows/ci.yml \
  .github/workflows/release.yml \
  .github/workflows/security.yml; do
  grep -Fq 'run: bash scripts/install-ci-checks.sh' "$workflow" || {
    echo "$workflow does not use the pinned CI check installer" >&2
    exit 1
  }
done

dependabot_config=.github/dependabot.yml
[ -f "$dependabot_config" ] || {
  echo "Dependabot configuration is missing" >&2
  exit 1
}
for ecosystem in gomod github-actions docker; do
  grep -Eq "^[[:space:]]*- package-ecosystem: ${ecosystem}$" "$dependabot_config" || {
    echo "Dependabot does not maintain ${ecosystem} dependencies" >&2
    exit 1
  }
done
for expected_count in \
  'target-branch: dev' \
  'interval: weekly' \
  'open-pull-requests-limit: 2'; do
  if [ "$(grep -Ec "^[[:space:]]+${expected_count}$" "$dependabot_config")" -ne 3 ]; then
    echo "every Dependabot ecosystem must use ${expected_count}" >&2
    exit 1
  fi
done

grep -Fq 'branches: [dev, main]' .github/workflows/ci.yml || {
  echo "CI must run after pushes to dev and main" >&2
  exit 1
}
for expected in \
  'issues: write' \
  'gh api repos/remnawave/node/releases/latest' \
  'gh issue create'; do
  grep -Fq "$expected" .github/workflows/contract-sync.yml || {
    echo "contract sync does not automate official release detection: $expected" >&2
    exit 1
  }
done
for oracle_caller in \
  .github/workflows/ci.yml \
  .github/workflows/contract-sync.yml \
  scripts/release-check.sh; do
  grep -Fq 'go run ./cmd/contract-source-check' "$oracle_caller" || {
    echo "$oracle_caller does not verify the pinned official source oracle" >&2
    exit 1
  }
done
for job in go repository installer netadmin gate; do
  grep -Eq "^  ${job}:$" .github/workflows/ci.yml || {
    echo "CI workflow is missing the ${job} job" >&2
    exit 1
  }
done
grep -Fq 'run: bash scripts/check-go.sh' .github/workflows/ci.yml || {
  echo "CI does not use the Go check component" >&2
  exit 1
}
grep -Fq 'run: bash scripts/check-repository.sh' .github/workflows/ci.yml || {
  echo "CI does not use the repository check component" >&2
  exit 1
}
if grep -R -Fq 'runs-on: ubuntu-latest' .github/workflows; then
  echo "GitHub workflows must use an explicit Ubuntu runner release" >&2
  exit 1
fi
grep -Fq 'readonly SHELLCHECK_VERSION=0.11.0' scripts/install-ci-checks.sh || {
  echo "CI ShellCheck version is not pinned" >&2
  exit 1
}
grep -Fq \
  'readonly SHELLCHECK_LINUX_X86_64_SHA256=8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198' \
  scripts/install-ci-checks.sh || {
  echo "CI x86_64 ShellCheck digest is not pinned" >&2
  exit 1
}
grep -Fq \
  'readonly SHELLCHECK_LINUX_AARCH64_SHA256=12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588' \
  scripts/install-ci-checks.sh || {
  echo "CI arm64 ShellCheck digest is not pinned" >&2
  exit 1
}

for invalid_tag in '../../../../../etc' '..'; do
  if RNL_TAG="$invalid_tag" resolve_install_tag luxiaba/remnanode-lite "$tag" >/dev/null 2>&1; then
    echo "release tag $invalid_tag unexpectedly passed" >&2
    exit 1
  fi
done

for bootstrap in \
  scripts/install-node.sh \
  scripts/install-node-alpine.sh \
  scripts/install-xray.sh \
  scripts/upgrade.sh \
  scripts/uninstall.sh; do
  case "$bootstrap" in
    scripts/install-node.sh|scripts/install-node-alpine.sh)
      bootstrap_args=(--upgrade --yes --dry-run)
      ;;
    scripts/install-xray.sh)
      bootstrap_args=(--dry-run)
      ;;
    scripts/upgrade.sh)
      bootstrap_args=(--yes --dry-run)
      ;;
    scripts/uninstall.sh)
      bootstrap_args=(--yes --dry-run)
      ;;
  esac
  if bootstrap_output="$(RNL_TAG='../../../../../etc' \
    bash -s -- "${bootstrap_args[@]}" <"$bootstrap" 2>&1)"; then
    bootstrap_status=0
  else
    bootstrap_status=$?
  fi
  if [ "$bootstrap_status" -ne 2 ] \
    || ! grep -Fq 'RNL_REPO' <<<"$bootstrap_output" \
    || ! grep -Fq 'RNL_TAG' <<<"$bootstrap_output"; then
    echo "$bootstrap did not reject a path-like bootstrap tag at coordinate validation" >&2
    exit 1
  fi
done

dry_run_path="$(mktemp -d)"
trusted_root="$(mktemp -d "${HOME:?HOME is required}/remnanode-supply-chain.XXXXXX")"
trusted_scripts="$trusted_root/scripts"
curl_called="$trusted_root/curl-called"
trap 'rm -rf "$dry_run_path" "$trusted_root"' EXIT
install -d -m 0700 "$trusted_scripts"
for script_name in \
  install-env-helpers.sh \
  install-node.sh \
  install-node-alpine.sh \
  install-xray.sh \
  uninstall.sh \
  upgrade.sh; do
  install -m 0755 "$ROOT_DIR/scripts/$script_name" "$trusted_scripts/$script_name"
done
for command_name in bash dirname grep head stat tar timeout tr uname wc; do
  command_path="$(command -v "$command_name")"
  [ -n "$command_path" ] || {
    echo "missing command required by portable dry-run test: $command_name" >&2
    exit 1
  }
  ln -s "$command_path" "$dry_run_path/$command_name"
done
# shellcheck disable=SC2016
printf '%s\n' '#!/bin/sh' ': >"${RNL_CURL_CALLED:?}"' 'exit 98' >"$dry_run_path/curl"
chmod 0755 "$dry_run_path/curl"

dry_run_output="$(PATH="$dry_run_path" RNL_CURL_CALLED="$curl_called" RNL_UPGRADE_XRAY=1 \
  bash "$trusted_scripts/upgrade.sh" --yes --dry-run)"
grep -Fq '[dry-run] Run the verified install-xray.sh from the target release' <<<"$dry_run_output"
wrapper_dry_run_output="$(PATH="$dry_run_path" RNL_CURL_CALLED="$curl_called" \
  bash "$trusted_scripts/install-node.sh" --upgrade --yes --dry-run)"
grep -Fq '[dry-run] Update /etc/systemd/system/remnawave-node.service' <<<"$wrapper_dry_run_output"
[ ! -e "$curl_called" ] || {
  echo "supply-chain dry-run attempted a network download" >&2
  exit 1
}

if CUSTOM_CORE_URL=https://example.invalid/rw-core bash scripts/install-xray.sh --dry-run; then
  echo "CUSTOM_CORE_URL without SHA-256 unexpectedly passed" >&2
  exit 1
fi
if ASN_DB_URL=https://example.invalid/asn-prefixes.bin bash scripts/install-xray.sh --dry-run; then
  echo "ASN_DB_URL without SHA-256 unexpectedly passed" >&2
  exit 1
fi
if XRAY_CORE_SHA256=0000000000000000000000000000000000000000000000000000000000000000 \
  bash scripts/install-xray.sh --dry-run; then
  echo "pinned rw-core SHA-256 override unexpectedly passed" >&2
  exit 1
fi

echo "supply-chain checks passed"
