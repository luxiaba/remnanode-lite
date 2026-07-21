# Native Linux Deployment

[Back to the documentation index](README.md)

This guide installs Remnanode Lite from GitHub Release binaries on a systemd or OpenRC host. Native deployment avoids Docker daemon overhead and lets the host service manager run the Node directly. If Docker is already available, [Docker Compose](deployment-docker.md) remains the simpler option.

The binary is `remnanode-lite`. Native packages keep the service name `remnawave-node` so existing upgrade, monitoring, and service-management commands continue to work.

## Support boundary

Release binaries are built for Linux `amd64` and `arm64`. The installer supports systemd and OpenRC; the installations recorded during development used:

| Platform | Service manager | Architecture |
| --- | --- | --- |
| Ubuntu 24.04 | systemd | arm64 |
| Alpine 3.22 | OpenRC | arm64 |

CI cross-compiles both architectures and runs Linux network-administration tests on Ubuntu. Before tagging `v2.8.0`, maintainers also checked the Docker candidate on a real `linux/amd64` host, under the maintained container limits, with a real Panel and proxy traffic. That was a release decision, not a repository evidence gate. Native systemd and OpenRC installs, real `arm64` runtime, the whole-host 512 MiB target, the 50,000-user load test, 24-hour soak, and fault and rollback injection remain follow-up validation. Test a native installation on your target distribution before rolling it out widely. On systems other than Debian or Ubuntu, install the commands required by the scripts first.

The target tag must have a published GitHub Release containing binary archives, support files, `SHA256SUMS`, and the ASN database. An `edge` or `sha-*` GHCR candidate image cannot substitute for native Release assets.

## Prerequisites

- Root access.
- Linux amd64 or arm64.
- A node created in the Panel and its complete Secret Key.
- The Node port configured in the Panel matches the host's `NODE_PORT`.
- Correct system time and working network access.
- At least 1 GiB of free disk is recommended before the first installation or an rw-core asset synchronization. The installer calculates the actual per-filesystem budget for downloads, extraction, target staging, and existing backups.
- Bash, curl, and util-linux `flock` installed before bootstrap.
- A host firewall that permits the Panel to reach the Node API port and permits inbound proxy ports required by the deployed configuration.

Both systemd and OpenRC templates limit the service to `448 MiB RAM / 0 swap / 1 CPU / 256 tasks`. OpenRC additionally requires writable, effective memory, CPU, and PIDs controllers under cgroup v2. The service refuses to start if any controller is unavailable.

### Bootstrap dependencies

Ubuntu or Debian:

```bash
sudo apt-get update
sudo apt-get install --yes curl util-linux
```

Alpine:

```bash
apk add --no-cache bash curl util-linux
```

The installer then supplies runtime dependencies such as CA certificates, tar, unzip, iproute2, and nftables.

## Install on systemd

Select an exact tag that already has a published Release. Both official-aligned versions and independent project iterations are valid:

```bash
release_tag='vX.Y.Z-rnl.N' # or vX.Y.Z
```

Interactive installation prompts for the port and Secret:

```bash
curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${release_tag}/scripts/install-node.sh" \
  | sudo env RNL_TAG="${release_tag}" bash
```

For non-interactive installation, pass the Secret through a restricted file so it does not remain in shell history:

```bash
umask 077
printf '%s' 'PASTE_THE_COMPLETE_SECRET_KEY_FROM_THE_PANEL' > /tmp/remnanode-secret.key

curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${release_tag}/scripts/install-node.sh" \
  | sudo env RNL_TAG="${release_tag}" bash -s -- \
      --install --yes --port 2222 --secret-file /tmp/remnanode-secret.key

rm -f /tmp/remnanode-secret.key
```

Verify the installation:

```bash
sudo systemctl --no-pager status remnawave-node
sudo ss -H -lntp 'sport = :2222'
sudo remnanode-lite doctor
```

## Install on Alpine/OpenRC

Alpine has a dedicated entry point:

```bash
release_tag='vX.Y.Z-rnl.N' # or vX.Y.Z

curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${release_tag}/scripts/install-node-alpine.sh" \
  | env RNL_TAG="${release_tag}" bash
```

The non-interactive options are the same as for systemd:

```bash
curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${release_tag}/scripts/install-node-alpine.sh" \
  | env RNL_TAG="${release_tag}" bash -s -- \
      --install --yes --port 2222 --secret-file /root/remnanode-secret.key
```

Verify the installation:

```bash
rc-service remnawave-node status
ss -H -lntp 'sport = :2222'
remnanode-lite doctor
```

`doctor` currently includes a systemd unit check, so a WARN that the systemd unit is absent is expected on OpenRC. ERROR findings affect the exit code and core result. End-to-end Panel connectivity must still be confirmed in the Panel.

## Installer options

Both entry points provide the same common options:

| Option | Description |
| --- | --- |
| `--install` | Fresh installation. If a complete installation is detected, switch to a rollback-capable upgrade and synchronize rw-core, geo, and ASN assets from the target Release by default. Add `--skip-xray` to retain existing assets. |
| `--upgrade` | Explicitly upgrade Node, service, and support files while preserving rw-core by default. |
| `--uninstall` | Enter the uninstall flow. |
| `--yes`, `-y` | Skip confirmation. If no Secret is available, installation completes without starting the service. |
| `--dry-run` | Preview actions without modifying the system. |
| `--skip-xray` | Do not install rw-core. Intended only for advanced environments that supply a compatible core independently. |
| `--low-memory` | Force `LOW_MEMORY=1` into configuration. Recommended for small-memory nodes. |
| `--port PORT` | Node HTTPS port in the range `1..65535`; defaults to 2222. |
| `--secret-file PATH` | Read, canonicalize, and validate the Secret safely from a regular file. |

The installer enables low-memory mode automatically when whole-machine `MemTotal <= 512 MiB`. If `node.env` already exists, the existing port and low-memory choice are preserved unless explicitly overridden.

## Installation transaction

The installer:

1. Acquires the global installer lock, rejecting concurrent installation, upgrade, rw-core update, or uninstall operations.
2. Checks architecture, disk budget, and base commands.
3. Creates the dedicated `remnanode:remnanode` system account and restricted directories.
4. Downloads the target Release's `SHA256SUMS` and architecture archive, then validates checksums, structure, and binary version.
5. Installs the Node, support files, pinned rw-core, geo data, and compact ASN database.
6. Validates and stores the Secret, and installs the service definition and log helper commands.
7. Starts the service and confirms that exactly one target Node process owns the configured TCP port.

Running `--install` again on a complete installation switches to the transactional upgrade path and refreshes rw-core, geo, and ASN assets from the target Release. An explicit `--upgrade` keeps those assets by default; add `--upgrade-xray` to refresh them. A partial installation follows the recovery path instead of being treated as a normal upgrade.

## Filesystem layout

| Path | Owner or purpose |
| --- | --- |
| `/usr/local/bin/remnanode-lite` | Main Node program. |
| `/usr/local/bin/remnanode-xlogs` | Follow rw-core stdout. |
| `/usr/local/bin/remnanode-xerrors` | Follow rw-core stderr. |
| `/etc/remnanode/node.env` | `root:remnanode 0640`; runtime configuration. |
| `/etc/remnanode/secret.key` | `root:remnanode 0640`; Panel Secret. |
| `/usr/local/lib/remnanode/rw-core` | Project-private rw-core. |
| `/usr/local/lib/remnanode/support/<tag>` | Service and installer support matching the installed Release. |
| `/usr/local/lib/remnanode/support-current` | Controlled symlink to the current support directory. |
| `/usr/local/share/remnanode/xray` | Geo and optional zapret data. |
| `/usr/local/share/remnanode/asn/asn-prefixes.bin` | Compact ASN database. |
| `/var/lib/remnanode` | Service working directory. The Node does not persist Panel Xray configuration here. |
| `/var/log/remnanode` | rw-core logs; OpenRC also stores supervisor logs here. |
| `/run/remnanode` | Unix socket directory cleared on reboot. |
| `/var/lib/remnanode-installer` | Root-only download, extraction, and transaction directory. |
| `/run/lock/remnanode-installer.lock` | Lock shared by all mutating installer entry points. |

The project does not own or remove generic `/usr/local/bin/xray` or `/usr/local/share/xray` paths.

Repository service definitions are maintained at [`deploy/remnawave-node.service`](../deploy/remnawave-node.service) and [`deploy/remnawave-node.openrc`](../deploy/remnawave-node.openrc).

## Service security model

The native service does not run as root. Both systemd and OpenRC use the dedicated `remnanode` user and grant only:

- `CAP_NET_ADMIN` to manage the project's nftables table and perform `NETLINK_SOCK_DIAG` socket destruction.
- `CAP_NET_BIND_SERVICE` to let rw-core listen on ports 1 through 1023.

systemd also applies a capability bounding set, `NoNewPrivileges`, read-only system directories, namespace, syscall, and address-family restrictions, and private temporary directories. OpenRC uses `supervise-daemon`, `no_new_privs`, and cgroup v2 limits.

The service manager does not export `node.env`. Before launching rw-core, the Node removes the Panel Secret, Secret file path, and Node configuration file path from the child environment, then supplies the asset paths and internal token required by the core.

## Service management

systemd:

```bash
sudo systemctl status remnawave-node
sudo systemctl restart remnawave-node
sudo systemctl stop remnawave-node
sudo journalctl -u remnawave-node -f
```

OpenRC:

```bash
rc-service remnawave-node status
rc-service remnawave-node restart
rc-service remnawave-node stop
tail -F /var/log/remnanode/openrc.log
```

On either platform, follow rw-core logs with:

```bash
remnanode-xlogs
remnanode-xerrors
```

After a service restart, the Node initially reports rw-core offline and waits for the Panel to send another start request. This is expected; it does not mean local configuration was lost or service startup failed.

## Upgrade

Select the target Release tag:

```bash
target_tag='vX.Y.Z-rnl.N' # or vX.Y.Z

curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${target_tag}/scripts/upgrade.sh" \
  | sudo env RNL_TAG="${target_tag}" bash -s -- --yes
```

By default, this upgrades only the Node, service, and support files and preserves the installed rw-core. If the target Release explicitly requires a matching core, or geo and ASN data need refreshing:

```bash
curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${target_tag}/scripts/upgrade.sh" \
  | sudo env RNL_TAG="${target_tag}" bash -s -- --yes --upgrade-xray
```

The upgrade transaction:

1. Records whether the service is running; when delegated from install, it also records whether the service is enabled at boot.
2. Backs up the binary, service definition, support files, `node.env`, `secret.key`, and optional rw-core, geo, and ASN assets.
3. Stops and confirms the exit of the Node and the rw-core process referenced by configuration.
4. Atomically replaces target files and migrates supported legacy configuration.
5. Restores the running state only if the service was running before upgrade or delegated install requires it to start.
6. Verifies the binary version and commits only after exactly one target process owns the configured port.

An explicit upgrade leaves a previously stopped service stopped. If validation fails, the script tries to restore the original files, boot registration, and service state. If it cannot finish the rollback, it leaves the backup in the root-only installer directory and exits with an error.

Changing `node.env` or the Secret does not require reinstallation. Update the correctly permissioned files as described in the [configuration reference](configuration.md), then restart the service.

## Roll back to an older version

Use only an older tag that this project has actually released:

```bash
old_tag='vX.Y.Z-rnl.N' # or vX.Y.Z

curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${old_tag}/scripts/upgrade.sh" \
  | sudo env RNL_TAG="${old_tag}" bash -s -- --yes
```

Add `--upgrade-xray` if the older version requires its corresponding rw-core. Before rollback, read both Releases' notes and confirm that configuration and contract baselines are compatible.

## Uninstall

Prefer the support script installed with the current version:

```bash
sudo bash /usr/local/lib/remnanode/support-current/scripts/uninstall.sh
```

Non-interactive modes:

| Mode | Command | Retained data |
| --- | --- | --- |
| Keep configuration | `--keep-config --yes` | `node.env`, Secret, logs, data, and rw-core/geo/ASN. |
| Purge runtime data | `--purge --yes` | rw-core/geo/ASN. |
| Remove all project assets | `--full` | No project configuration, logs, data, or rw-core/geo/ASN. |
| Preview | Add `--dry-run` | No changes are made. |

Files are deleted only after the uninstaller confirms that the service manager has stopped and that the target Node and rw-core processes have exited. It also removes the project's private nftables table, but it does not terminate unrelated processes with similar names or remove generic Xray paths.

Even with `--full`, these system-level items remain:

- the `remnanode` system user and group;
- general system packages installed by the installer;
- the root-only marker directory at `/var/lib/remnanode-installer`.

These items are kept to make a later reinstall safer. As a result, `--full` removes all project files but does not return the host to exactly its pre-install state.

## Ongoing operations

See the [operations guide](operations.md) for health checks, log budgets, update policy, and troubleshooting.
