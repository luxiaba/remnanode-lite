# Native Linux Deployment

[Documentation home](README.md) | [Configuration](configuration.md) | [Operations](operations.md) | [Versioning](versioning.md)

Native deployment runs Remnanode Lite directly under the host service manager. It is useful on small servers where Docker cannot be installed or the Docker Engine daemon and container runtime are not appropriate for the host. Native does not remove the need for a background service: `remnanode-lite` runs under systemd or, on qualified Alpine hosts, distribution OpenRC. Docker Compose remains the default path for most installations. Self-contained Native lifecycle bundles are distributed as exactly tagged GitHub Release assets.

Each published Native bundle contains the Node, `rnlctl`, rw-core, GeoIP, GeoSite, ASN data, service definitions, license notices, an SPDX SBOM, and a manifest that records every file digest. The installer verifies the outer archive checksum and the bundle manifest before changing the host.

Native installation and upgrade always use an exact version from a Release
that includes the Native lifecycle assets. A Release is Native-capable only
when it offers `install.sh`, `SHA256SUMS`, and the archive for the host
architecture. Moving names such as `latest`, `preview`, and `edge` are not
accepted.

## Supported hosts

| Host | Service manager | Support level |
| --- | --- | --- |
| Rocky Linux 9 | systemd | Primary Native target |
| Rocky Linux 8 | systemd 239 | Compatible; the newer hardening drop-in is omitted automatically |
| Debian 12 | systemd | Compatible |
| Other current systemd distributions | systemd | Expected to work; test before fleet rollout |
| Alpine Linux 3.22.x (persistent `sys` install) | Distribution OpenRC | Supported with prerequisites |

Native lifecycle bundles are built for Linux `amd64` and `arm64`. The maintained resource profile limits the service to `448 MiB RAM`, no additional service swap, `1 CPU`, and `256 tasks`, leaving room for the host on a `512 MiB / 1 vCPU / 2 GB` machine.

The Alpine row is deliberately specific; it is not a claim of generic OpenRC support. The host must be a persistent Alpine Linux 3.22.x `sys` installation on `amd64` or `arm64`, run the distribution OpenRC as PID 1, use Linux 5.14 or newer, and mount a unified cgroup v2 hierarchy. The `cpu`, `memory`, and `pids` controllers, `memory.swap.max`, the parent `cgroup.procs`, and the service cgroup's `cgroup.kill` must all be usable. The managed service verifies its exact limits and cgroup membership in `start_pre` and refuses to start if any requirement is missing.

Docker containers, init-less images, and nested or virtualized environments that do not expose that cgroup contract are not supported Native Alpine hosts. A nested guest can qualify only when the same runtime checks pass; its distribution name alone is not sufficient. Do not bypass or weaken the service check to make a constrained host start.

The installer does not configure repositories, sysctl, firewall rules, SELinux policy, or time synchronization. Those remain host-administration responsibilities.

## Prerequisites

Run the installer as root on Linux. Before an active installation, the host needs:

- systemd, or the qualified Alpine/OpenRC environment described above;
- `nft` from nftables and `ss` from iproute2;
- `useradd` and `groupadd` when the dedicated `remnanode-lite` account does not already exist;
- a trusted CA store and either `curl` or `wget` for an online install;
- GNU tar and gzip to unpack a release bundle;
- the Node port open from the Panel, plus any proxy inbound ports sent by the Panel.

On the primary distributions, install missing runtime commands with:

```bash
# Rocky Linux 8/9
sudo dnf install -y ca-certificates curl nftables iproute

# Debian 12
sudo apt-get update
sudo apt-get install -y ca-certificates curl nftables iproute2

# Alpine Linux 3.22.x (root shell)
apk add --no-cache ca-certificates curl openrc shadow nftables iproute2 tar
rc-update add cgroups boot
rc-service cgroups start
```

On Alpine, `shadow` supplies `useradd` and `groupadd`, and the `tar` package supplies GNU tar; BusyBox tar is not sufficient for the Native bundle's strict extraction path. OpenRC supplies `checkpath` as an internal helper in the `openrc-run` service environment; it is not a separate dependency or a command that must pass a normal `PATH` preflight.

Keep the system clock synchronized. mTLS and JWT authentication can fail when the clock is wrong.

## Install an exact release

Choose a version shown on the GitHub Releases page, then download the installer
and checksum list from that exact published Release. A source version or a
candidate image is not a downloadable Native bundle:

```bash
VERSION="<published-version>" # for example: X.Y.Z or X.Y.Z-rnl.N
BASE="https://github.com/luxiaba/remnanode-lite/releases/download/${VERSION}"

workdir="$(mktemp -d /var/tmp/remnanode-lite-download.XXXXXX)"
cd "$workdir"
curl -fLO "${BASE}/install.sh"
curl -fLO "${BASE}/SHA256SUMS"
grep '  install.sh$' SHA256SUMS | sha256sum -c -

sudo sh ./install.sh --version "$VERSION" --port 2222
```

Replace `2222` with the port configured for this Node in the Panel. If no valid Secret already exists, the installer reads it from the terminal without echoing it. It then asks for a separate installation confirmation.

The online installer downloads only the exact `${VERSION}` archive for the machine architecture. It never follows GitHub Latest and never resolves a moving container channel.

### Unattended install

For automation, place the complete Panel Secret in a temporary regular file and pass it explicitly. `--yes` skips only the non-secret confirmation; it does not invent or fetch a Secret.

```bash
umask 077
printf '%s\n' 'PASTE_THE_COMPLETE_PANEL_SECRET_KEY' >/root/remnanode-lite.secret

sudo sh ./install.sh \
  --version "$VERSION" \
  --port 2222 \
  --secret-file /root/remnanode-lite.secret \
  --yes

rm -f /root/remnanode-lite.secret
```

Do not pass the Secret as a command-line value. Command lines can be visible in process listings and shell history.

### Prepare now, activate later

`--prepare-only` installs and verifies the release without enabling or starting the service. A Secret is optional until activation:

```bash
sudo sh ./install.sh \
  --version "$VERSION" \
  --port 2222 \
  --prepare-only \
  --yes
```

Activate it later with a restricted Secret file:

```bash
sudo rnlctl activate --secret-file /root/remnanode-lite.secret
```

Prepared installations cannot be started with `rnlctl start`; activation is the explicit transition that validates configuration, enables the service, starts it, and waits for internal health.

`--prepare-only` verifies and lays out release files without starting the service, so it can succeed on a host that does not satisfy the Alpine/OpenRC cgroup contract. `rnlctl activate` is the first authoritative check of the managed service's runtime cgroup and limit contract: OpenRC runs `start_pre`, applies and verifies the limits, and fails closed when those controls are unavailable. The Alpine version, persistent `sys` installation, OpenRC PID 1, and kernel version remain operator prerequisites and release-qualification checks; `activate` does not identify them on the operator's behalf.

## Offline or staged install

Download these three assets from one exact GitHub Release on a connected machine:

```text
install.sh
remnanode-lite_<version>_linux_<architecture>.tar.gz
SHA256SUMS
```

Verify both files against that checksum list, transfer all three to the server, and keep their names unchanged:

```bash
VERSION="<published-version>"
ARCH="<amd64-or-arm64>" # architecture of the target host
ARCHIVE="remnanode-lite_${VERSION}_linux_${ARCH}.tar.gz"
awk '$2 == "install.sh"' SHA256SUMS | sha256sum -c -
awk -v archive="$ARCHIVE" '$2 == archive' SHA256SUMS | sha256sum -c -
```

On the target host:

```bash
VERSION="<published-version>"
ARCH="<amd64-or-arm64>"
ARCHIVE="remnanode-lite_${VERSION}_linux_${ARCH}.tar.gz"
sudo sh ./install.sh \
  --bundle "./${ARCHIVE}" \
  --port 2222
```

When `--sha256` is omitted, the installer reads the unique matching entry from the `SHA256SUMS` file beside the archive. You may instead pass the 64-character archive digest with `--sha256`.

An extracted bundle can install itself with `sudo ./install.sh`, but an archive plus the independently downloaded checksum list gives a stronger outer trust anchor and is preferred for production staging.

## What the installer owns

```text
/usr/local/sbin/rnlctl
/usr/local/bin/remnanode-lite -> /usr/local/lib/remnanode-lite/current/bin/remnanode-lite

/usr/local/lib/remnanode-lite/
├── current -> generations/<current-id>
├── previous -> generations/<previous-id>       # after the first upgrade
└── generations/<generation-id>/

/etc/remnanode-lite/
├── node.env
└── secret.key

/var/lib/remnanode-lite/
/var/log/remnanode-lite/
/run/remnanode-lite/

/var/lib/remnanode-lite-installer/
├── state.json
├── journal.json                                # present only during/recovering an operation
├── retained.json                               # may remain after a non-purge uninstall
├── bundles/
└── tmp/                                        # short-lived private workspaces
```

`rnlctl` is a separate root-owned regular file, not a symlink into the active generation. This keeps the repair tool available while generation links are being inspected or replaced.

The service runs as the non-login `remnanode-lite` user and group. The installer records whether it created each account object; `uninstall --purge` removes only objects it owns and refuses to remove an identity that has changed.

The service name is `remnanode-lite` on both managers:

```bash
systemctl status remnanode-lite.service
rc-service remnanode-lite status
```

The base systemd unit works with systemd 239. On systemd 247 or newer, the installer also places `20-remnanode-lite-hardening.conf` in the unit's drop-in directory. Local overrides belong in a later file such as `/etc/systemd/system/remnanode-lite.service.d/90-local.conf`; do not edit the managed unit in place.

## Verify the installation

Use `rnlctl` for the lifecycle view and the service manager for low-level detail:

```bash
sudo rnlctl status
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl logs node --lines 100
sudo rnlctl logs core-errors --lines 100
remnanode-lite version
```

Bare `rnlctl status` now prints a consistent human-readable lifecycle summary; it no longer proxies raw `systemctl status` or `rc-service status` output. Its layout is not a parsing contract. Existing automation should use `status --json`, whose schema is unchanged. Use the service-manager commands shown above when you need low-level detail.

For an active installation, status checks generation selection, managed configuration, service state, permissions, repair cache, and the internal health socket. `doctor` expands those checks into one result per subsystem, then prints a summary and deterministic `Next` suggestions for known failures. `doctor --json` retains its existing machine-readable schema. Neither command proves Panel reachability or proxy traffic; confirm both in the Panel and with a representative client connection.

Lifecycle states reported by `status` and `status --json` are:

| State | Meaning |
| --- | --- |
| `absent` | No managed Native installation exists |
| `prepared` | Installed and verified, intentionally disabled and stopped |
| `installed` | Managed state, service state, files, and health agree |
| `degraded` | An installation exists but one or more checks fail |
| `recovery-required` | A transaction journal or unreadable state requires repair |

## Command-line experience

The global options may appear before or after the command or subcommand:

```bash
sudo rnlctl --quiet config set LOW_MEMORY=1
sudo rnlctl status --no-color
```

`--quiet` (or `-q`) hides successful lifecycle/configuration mutation messages, the `configuration ok` line from `config check`, and human `status`/`doctor` output. It never hides help, version, `config show`/`get`, logs, completion scripts, upgrade dry-run plans, JSON, or errors.

Human status and doctor output use restrained color only when stdout is a TTY. Output has no ANSI sequences when redirected, when `--no-color` is present, when `NO_COLOR` is set to a non-empty value, or when `TERM=dumb`.

Exit codes are normally `0` for success, `1` for a runtime failure or unhealthy result, and `2` for invalid usage. `status` treats `absent` as a valid state and returns `0`; automation that requires an installation must also inspect the JSON `installed` or `deployment` field. Once `logs` starts `journalctl` or `tail`, it passes through that reader's exit code, including `128 + signal` when terminated by a signal.

### Shell completion

`rnlctl completion bash|zsh|fish` writes a completion script to stdout. It never installs a file or edits a shell startup file.

For Bash with `bash-completion`, install it in the per-user XDG directory:

```bash
bash_dir="${BASH_COMPLETION_USER_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion}/completions"
mkdir -p "$bash_dir"
/usr/local/sbin/rnlctl completion bash >"$bash_dir/rnlctl"
```

Start a new Bash session after `bash-completion` is loaded. As a current-session fallback, or as a line you add yourself to `.bashrc`, use:

```bash
source <(/usr/local/sbin/rnlctl completion bash)
```

For Zsh, place `_rnlctl` in a user `fpath` directory:

```zsh
mkdir -p ~/.zfunc
/usr/local/sbin/rnlctl completion zsh > ~/.zfunc/_rnlctl
```

Add the directory to `fpath` before your existing `compinit` call, or initialize completion if your configuration does not already do so:

```zsh
fpath=(~/.zfunc $fpath)
autoload -Uz compinit
compinit
```

Fish loads per-user completion files directly:

```fish
mkdir -p ~/.config/fish/completions
/usr/local/sbin/rnlctl completion fish > ~/.config/fish/completions/rnlctl.fish
```

The generated completion is static: it does not query Releases, generation IDs, or service state, and it contains no Secret values or internal configuration names. Regenerate the file after upgrading `rnlctl`. Completion after `sudo` depends on the user's shell framework and is not provided by these scripts themselves.

## Service and logs

These commands work on both supported service-manager paths:

```bash
sudo rnlctl start
sudo rnlctl stop
sudo rnlctl restart
sudo rnlctl logs node --follow
sudo rnlctl logs core --follow
sudo rnlctl logs core-errors --follow
```

On systemd, Node output goes to journald. It can be filtered by a Go-style positive duration and combined with line and follow options:

```bash
sudo rnlctl logs node --since 15m --lines 100
sudo rnlctl logs node --since 15m --follow
```

`--lines` defaults to `50` and accepts `1..100000`. `--since` is available only for systemd Node logs and accepts positive Go durations such as `15m` or `2h`; it does not accept an absolute date or `1d`. OpenRC Node logs and the `core`/`core-errors` files do not share a reliable timestamp format, so they reject it.

On systemd, `--lines N` selects at most N records in total for the unit. On OpenRC it reads N lines from each of `openrc.log` and `openrc.err.log`. rw-core output uses `xray.out.log` and `xray.err.log`, one file per source. File-backed reads use the current path only: a rotated `.1` file is not used to backfill a short current file. `--follow` uses `tail -F`, so it continues across later rotation.

## Upgrade

Before changing the installation, verify the exact published candidate:

```bash
VERSION="<published-version>"
sudo rnlctl upgrade --to "$VERSION" --dry-run
sudo rnlctl upgrade --to "$VERSION" --dry-run --json
```

The preflight requires root, an existing clean installation, and no pending lifecycle journal. For `--to`, it downloads and statically verifies the complete candidate in a private temporary workspace, then briefly takes the lifecycle lock to verify current state and known host preconditions. It does not create a generation, cache, or transaction journal; switch or restart the service; execute the candidate binary; run target health checks; or retain the downloaded bundle. `--json` is valid only together with `--dry-run`.

A successful dry-run uses temporary disk but does not reserve or guarantee enough space for the real upgrade. It cannot guarantee that host state will remain unchanged or that the later upgrade will succeed. The same flag can inspect a local `--bundle` plus `--sha256`, or a `--bundle-root`. After reviewing the plan, perform the upgrade explicitly:

```bash
sudo rnlctl upgrade --to "$VERSION"
```

`rnlctl` downloads the matching archive and checksum from the exact GitHub Release, validates every bundled file, and builds a new generation. It preserves whether the service was enabled and running before the operation. If the service was active, the transaction stops it, selects the new generation, restores the service state, validates the binary version, and waits for internal health before committing.

Only the current and previous generations are retained. A successful later upgrade removes the superseded third generation and its cache. Runtime assets are part of the generation, so Node, rw-core, GeoIP, GeoSite, ASN data, notices, and service material move together.

For an offline upgrade, use the verified archive directly:

```bash
VERSION="<published-version>"
ARCH="<amd64-or-arm64>"
ARCHIVE="remnanode-lite_${VERSION}_linux_${ARCH}.tar.gz"
sudo rnlctl upgrade \
  --bundle "./${ARCHIVE}" \
  --sha256 '<64-character-sha256>' \
  --expected-version "$VERSION"
```

Do not copy a new binary over `/usr/local/bin/remnanode-lite`. That bypasses generation verification, service preparation, rollback, and lifecycle state.

## Rollback

Roll back to the one retained previous generation:

```bash
sudo rnlctl rollback
```

The command swaps current and previous, preserves the service's enabled/running state, and verifies the selected generation. To make an operator's intent explicit, use the exact generation ID shown by `rnlctl status --json`:

```bash
sudo rnlctl rollback --to '<previous-generation-id>'
```

Rollback is intentionally limited to the retained generation. Use `rnlctl upgrade --to <exact-version>` when you need any other published release.

## Recover an interrupted operation

Every lifecycle or configuration mutation holds
`/run/remnanode-lite-installer/operation.lock`. Generation and service lifecycle
transitions also write a durable journal. Configuration and Secret mutations use
atomic file replacement and the within-process recovery described below; they do
not create a crash-safe journal. If a lifecycle command reports that repair is
required, do not delete the lock, journal, generation, or cache manually.

Root operations use private mode-`0700` workspaces and remove them on exit. A
safe, absolute `TMPDIR` supplied by the operator takes priority; an unsafe path
is ignored. Otherwise the bootstrap and lifecycle controller prefer
`/var/lib/remnanode-lite-installer/tmp` and fall back to `/var/tmp`. These
disk-backed locations keep bundle download and extraction out of the small
runtime `/tmp` tmpfs during a normal install or upgrade.

Start with:

```bash
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl repair
```

Repair restores the committed generation, service definitions, links, ownership, and intended service state from verified cached material. It does not upgrade. When a required cache is unavailable or damaged, provide the archive for one already recorded generation:

```bash
VERSION="<installed-version>"
ARCH="<amd64-or-arm64>"
ARCHIVE="remnanode-lite_${VERSION}_linux_${ARCH}.tar.gz"
sudo rnlctl repair \
  --bundle "./${ARCHIVE}" \
  --sha256 '<64-character-sha256>' \
  --expected-version "$VERSION"
```

The supplied bundle must match an installed generation identity. After repair, run `status --json`, check logs, confirm the Panel connection, and test traffic.

## Change the port or Secret

`/etc/remnanode-lite/node.env` is the single source of truth for Native runtime settings. `rnlctl config` is a safe editing layer over that file, not a separate store. It exposes only the six administrator-editable, non-secret keys documented in the [configuration reference](configuration.md#native-configuration-commands).

For an active Node, change the port and apply it in one operation:

```bash
sudo rnlctl config set NODE_PORT=2222 --apply
```

Update the Node record in Panel and the host firewall to the same port. Host networking provides no translation layer.

For a Secret rotation, place the complete new Secret in a temporary root-only regular file, then let `rnlctl` validate and install it:

```bash
umask 077
sudo install -m 0600 /dev/null /root/new-node-secret.key
sudoedit /root/new-node-secret.key
sudo rnlctl secret set --file /root/new-node-secret.key --apply
sudo rm -f /root/new-node-secret.key
```

The Secret value never appears in `node.env`, command arguments, or command output. If a `set --apply`, `unset --apply`, or `secret set --apply` restart or internal health check fails after changing a file, `rnlctl` attempts to restore the previous file and the active service running with it. This recovery is best effort within the command, not a durable or crash-safe transaction.

`--apply` is available only while the managed service is active. For a stopped service, change the value without `--apply`, then run `rnlctl start`. For a prepared installation, make the change without `--apply`, then run `rnlctl activate`; Secret setup can also be combined with activation through `rnlctl activate --secret-file PATH`.

Manual editing remains supported for `node.env`. Preserve `root:remnanode-lite 0640`, run `sudo rnlctl config check`, then use `sudo rnlctl config apply` on an active installation. `config apply` validates, restarts, and waits for internal health, but cannot roll back a manual edit because it has no previous snapshot. Neither `check` nor `apply` tests Panel connectivity or proxy traffic.

See the [configuration reference](configuration.md) for the complete setting table and managed-path rules.

## Uninstall

A normal uninstall removes the service, binaries, generations, runtime state, logs, and repair bundle cache. It keeps `/etc/remnanode-lite` and records account ownership so a later reinstall can safely reuse the configuration:

```bash
sudo rnlctl uninstall
```

To remove managed configuration and installer metadata as well, use the explicit purge form:

```bash
sudo rnlctl uninstall --purge --yes
```

Purge removes the `remnanode-lite` user or group only when lifecycle state proves that this installer created it and its identity is unchanged. It does not remove nftables packages, iproute2, CA certificates, host firewall policy, sysctl settings, or unrelated Xray installations.

Both uninstall forms remove the managed unit and the managed
`20-remnanode-lite-hardening.conf` drop-in. An empty expected drop-in directory
is removed too. A local override such as `90-local.conf`, or any unusual
directory object, is deliberately left untouched.

## Security notes

- Keep `/etc/remnanode-lite` owned by `root:remnanode-lite` with directory mode `0750`; `node.env` and `secret.key` use `0640`.
- Do not put a non-empty `SECRET_KEY` in `node.env`. Native lifecycle management requires `SECRET_KEY_FILE=/etc/remnanode-lite/secret.key`.
- The managed Node process runs as `remnanode-lite` and receives only `CAP_NET_ADMIN` and `CAP_NET_BIND_SERVICE`. On OpenRC, its root supervisor remains service-manager infrastructure; do not replace the unit with a root Node process to hide a capability error.
- Restrict the Node API port to Panel addresses when your network permits it. Open proxy inbound ports according to the Panel configuration.
- Keep one known-good previous generation until the replacement has passed Panel and traffic checks.
- Read the [security policy](../SECURITY.md) before changing service hardening, installer trust, file ownership, or release provenance.
