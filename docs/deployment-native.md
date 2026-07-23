# Native Linux Deployment

[Documentation home](README.md) | [Configuration](configuration.md) | [Operations](operations.md) | [Versioning](versioning.md)

Native deployment runs Remnanode Lite directly under the host service manager. It is useful on small servers where Docker cannot be installed or the Docker Engine daemon and container runtime are not appropriate for the host. Native does not remove the need for a background service: `remnanode-lite` runs under systemd or OpenRC. Docker Compose remains the default path for most installations. Self-contained Native lifecycle bundles are distributed as exactly tagged GitHub Release assets.

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
| OpenRC with writable cgroup v2 controllers | OpenRC | Experimental |

Native lifecycle bundles are built for Linux `amd64` and `arm64`. The maintained resource profile limits the service to `448 MiB RAM`, no additional service swap, `1 CPU`, and `256 tasks`, leaving room for the host on a `512 MiB / 1 vCPU / 2 GB` machine.

OpenRC support is intentionally narrower. It requires `supervise-daemon`, `checkpath`, `rc-update`, cgroup v2, writable memory/CPU/PID controllers, and `cgroup.kill`. The service refuses to start when those limits cannot be applied. Treat OpenRC as an evaluation path until it has been tested on the exact distribution and boot environment you plan to run.

The installer does not configure repositories, sysctl, firewall rules, SELinux policy, or time synchronization. Those remain host-administration responsibilities.

## Prerequisites

Run the installer as root on Linux. Before an active installation, the host needs:

- systemd, or the experimental OpenRC environment described above;
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
```

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
grep '  install.sh$' SHA256SUMS | sha256sum --check --strict -

sudo sh ./install.sh --version "$VERSION" --port 38329
```

Replace `38329` with the port configured for this Node in the Panel. If no valid Secret already exists, the installer reads it from the terminal without echoing it. It then asks for a separate installation confirmation.

The online installer downloads only the exact `${VERSION}` archive for the machine architecture. It never follows GitHub Latest and never resolves a moving container channel.

### Unattended install

For automation, place the complete Panel Secret in a temporary regular file and pass it explicitly. `--yes` skips only the non-secret confirmation; it does not invent or fetch a Secret.

```bash
umask 077
printf '%s\n' 'PASTE_THE_COMPLETE_PANEL_SECRET_KEY' >/root/remnanode-lite.secret

sudo sh ./install.sh \
  --version "$VERSION" \
  --port 38329 \
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
  --port 38329 \
  --prepare-only \
  --yes
```

Activate it later with a restricted Secret file:

```bash
sudo rnlctl activate --secret-file /root/remnanode-lite.secret
```

Prepared installations cannot be started with `rnlctl start`; activation is the explicit transition that validates configuration, enables the service, starts it, and waits for internal health.

## Offline or staged install

Download these three assets from one exact GitHub Release on a connected machine:

```text
install.sh
remnanode-lite_<version>_linux_<architecture>.tar.gz
SHA256SUMS
```

Verify both files against that checksum list, transfer all three to the server, and keep their names unchanged:

```bash
grep -E '  (install\.sh|remnanode-lite_.*_linux_(amd64|arm64)\.tar\.gz)$' \
  SHA256SUMS | sha256sum --check --strict -
```

On the target host:

```bash
sudo sh ./install.sh \
  --bundle "./remnanode-lite_${VERSION}_linux_amd64.tar.gz" \
  --port 38329
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
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl logs node --lines 100
sudo rnlctl logs core-errors --lines 100
remnanode-lite version
```

For an active installation, `status --json` checks generation selection, managed configuration, service state, permissions, repair cache, and the internal health socket. `doctor` expands those checks into one result per subsystem. Neither command proves Panel reachability or proxy traffic; confirm both in the Panel and with a representative client connection.

Lifecycle states reported by `status --json` are:

| State | Meaning |
| --- | --- |
| `absent` | No managed Native installation exists |
| `prepared` | Installed and verified, intentionally disabled and stopped |
| `installed` | Managed state, service state, files, and health agree |
| `degraded` | An installation exists but one or more checks fail |
| `recovery-required` | A transaction journal or unreadable state requires repair |

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

On systemd, Node output goes to journald. On OpenRC, it goes to `/var/log/remnanode-lite/openrc.log` and `openrc.err.log`. rw-core output always uses `/var/log/remnanode-lite/xray.out.log` and `xray.err.log`. `rnlctl logs` selects the correct backend and follows rotated core files with `tail -F`.

## Upgrade

Upgrade to one exact published release:

```bash
VERSION="<published-version>"
sudo rnlctl upgrade --to "$VERSION"
```

`rnlctl` downloads the matching archive and checksum from the exact GitHub Release, validates every bundled file, and builds a new generation. It preserves whether the service was enabled and running before the operation. If the service was active, the transaction stops it, selects the new generation, restores the service state, validates the binary version, and waits for internal health before committing.

Only the current and previous generations are retained. A successful later upgrade removes the superseded third generation and its cache. Runtime assets are part of the generation, so Node, rw-core, GeoIP, GeoSite, ASN data, notices, and service material move together.

For an offline upgrade, use the verified archive directly:

```bash
sudo rnlctl upgrade \
  --bundle "./remnanode-lite_${VERSION}_linux_amd64.tar.gz" \
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

Every mutating command holds `/run/remnanode-lite-installer/operation.lock` and writes a durable journal around lifecycle transitions. If a command reports that repair is required, do not delete the lock, journal, generation, or cache manually.

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
sudo rnlctl repair \
  --bundle "./remnanode-lite_<installed-version>_linux_amd64.tar.gz" \
  --sha256 '<64-character-sha256>' \
  --expected-version '<installed-version>'
```

The supplied bundle must match an installed generation identity. After repair, run `status --json`, check logs, confirm the Panel connection, and test traffic.

## Change the port or Secret

`/etc/remnanode-lite/node.env` is a root-managed data file, not a shell script. The Node reads it directly. Managed path keys must continue to point at the active generation layout.

For a Secret rotation, write the new Secret to a root-only temporary file, validate it, and replace the installed file atomically:

```bash
umask 077
secret_tmp="$(mktemp)"
printf '%s\n' 'PASTE_THE_NEW_COMPLETE_SECRET_KEY' >"$secret_tmp"
remnanode-lite validate-secret <"$secret_tmp"

sudo install -o root -g remnanode-lite -m 0640 \
  "$secret_tmp" /etc/remnanode-lite/secret.key.new
sudo mv -f /etc/remnanode-lite/secret.key.new /etc/remnanode-lite/secret.key
rm -f "$secret_tmp"

sudo rnlctl restart
```

To change the Node port, edit only `NODE_PORT` in `/etc/remnanode-lite/node.env`, update the Panel and firewall to the same value, then run `sudo rnlctl restart`. `rnlctl doctor` should pass before the restart.

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

## Security notes

- Keep `/etc/remnanode-lite` owned by `root:remnanode-lite` with directory mode `0750`; `node.env` and `secret.key` use `0640`.
- Do not put a non-empty `SECRET_KEY` in `node.env`. Native lifecycle management requires `SECRET_KEY_FILE=/etc/remnanode-lite/secret.key`.
- The service receives only `CAP_NET_ADMIN` and `CAP_NET_BIND_SERVICE`. Do not replace the unit with a root service to avoid fixing a capability error.
- Restrict the Node API port to Panel addresses when your network permits it. Open proxy inbound ports according to the Panel configuration.
- Keep one known-good previous generation until the replacement has passed Panel and traffic checks.
- Read the [security policy](../SECURITY.md) before changing service hardening, installer trust, file ownership, or release provenance.
