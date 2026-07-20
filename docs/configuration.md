# Configuration Reference

[Back to the documentation index](README.md)

This reference describes where Remnanode Lite reads configuration, how precedence works, and what each setting actually controls. Node runtime settings, Docker Compose interpolation, and installer options are separate configuration surfaces. Similar names do not imply that the same program consumes them.

## Sources and precedence

At startup, the Node selects a configuration file in this order:

1. The path specified by `REMNANODE_ENV` in the startup environment.
2. `/etc/remnanode/node.env`, if it exists.
3. `.env` in the current working directory.

The file is read first. Known, non-empty values from the process environment then override file values. An empty environment variable does not clear a file value. A non-empty `SECRET_KEY` takes precedence over `SECRET_KEY_FILE`.

systemd and OpenRC always set `REMNANODE_ENV=/etc/remnanode/node.env`, but they do not source or export the complete file. The Node parses it as a restricted data file, so unknown keys and the Secret are not automatically copied into the process environment. Docker Compose instead passes the selected runtime variables directly into the container.

Restart the Node or recreate the container after changing configuration. Runtime configuration reload is not supported.

## Node runtime configuration

The Go process reads the settings in this table directly. Default paths match the standard layout used by the project container image and native installers.

| Variable | Required | Default | Purpose and constraints |
| --- | --- | --- | --- |
| `NODE_PORT` | Yes | None | HTTPS port used by the Panel to connect to the Node. Every startup path accepts only `1..65535`; invalid values fail before the listener starts. |
| `NODE_BIND_ADDR` | No | Empty | HTTPS listen address. Empty means all local addresses. On a multihomed host, set the specific address reachable by the Panel. |
| `SECRET_KEY` | Conditionally | Empty | Complete Secret Key supplied by the Panel. A non-empty value takes precedence over `SECRET_KEY_FILE`. |
| `SECRET_KEY_FILE` | Conditionally | Empty | Read the Secret from a regular file. Native deployments use `/etc/remnanode/secret.key`. |
| `XRAY_BIN` | No | `/usr/local/lib/remnanode/rw-core` | Path to the rw-core executable. |
| `GEO_DIR` | No | `/usr/local/share/remnanode/xray` | Directory containing `geoip.dat`, `geosite.dat`, and optional zapret data. |
| `LOG_DIR` | No | `/var/log/remnanode` | Directory for rw-core stdout and stderr logs. |
| `ASN_DB_PATH` | No | `/usr/local/share/remnanode/asn/asn-prefixes.bin` | Compact ASN database. If unavailable, the plugin `asList` shared list degrades to an empty list while other core features continue to run. |
| `INTERNAL_SOCKET_PATH` | No | `/run/remnanode/internal.sock` | Unix socket between the Node and rw-core. Production deployments normally should not change it. |
| `INTERNAL_REST_TOKEN` | No | Random value generated at every start | Token for internal configuration and webhooks. Leaving it empty is safest; a fixed value is primarily for controlled debugging. |
| `DISABLE_HASHED_SET_CHECK` | No | `false` | When true, the Node no longer uses the configuration hash to skip an unchanged start; every start request restarts the core. Debugging only. |
| `LOW_MEMORY` | No | `false` | Enables the low-memory policy. Production Compose enables it by default. Native installers enable it automatically when whole-machine memory is at most 512 MiB. |
| `BODY_LIMIT_MB` | No | `0` (automatic) | Additional request-body limit for the public `/node` HTTPS server, allowed range `0..1024`. In low-memory mode, an explicit value cannot exceed 16. The internal Unix webhook remains fixed at 8 KiB. |
| `GOMEMLIMIT` | No | Empty | Soft limit for memory managed by the Go runtime. Accepts a byte count, `B/KiB/MiB/GiB/TiB`, or `off`. An explicit value overrides the low-memory default. |
| `NODE_CONTRACT_VERSION` | No | Compiled contract version | Overrides the `nodeVersion` reported to the Panel. Use only for contract debugging or emergency compatibility validation. |
| `XRAY_CORE_VERSION` | No | Detected from the actual binary | Overrides the reported rw-core version. It does not install, upgrade, or validate that binary. |

Boolean values are case-insensitive and accept `true/false`, `1/0`, or `yes/no`. Invalid Boolean, numeric, or version values make the Node exit before listening instead of silently falling back.

### Low-memory mode

`LOW_MEMORY=1` changes these runtime boundaries together:

| Boundary | Normal mode | Low-memory mode |
| --- | ---: | ---: |
| Go soft memory limit | Go default policy | 180 MiB |
| Node API TCP connection limit | 128 | 16 |
| Active HTTP handlers | 32 | 4 |
| Automatic request-body limit | 256 MiB | 16 MiB |
| rw-core readiness wait | 20 seconds | 90 seconds |

These values are not container or cgroup hard limits. `GOMEMLIMIT` constrains memory managed by the Go runtime; it is neither a heap-only limit nor a limit on total process RSS. rw-core, non-runtime memory in the Go process, tmpfs, and other memory all count toward the shared 448 MiB Compose/systemd/OpenRC limit.

Public routes also have smaller per-route limits. Even if `BODY_LIMIT_MB` is set higher, the effective limit of any current public request does not exceed 16 MiB.

### Secret Key

The Secret is base64- or base64url-encoded JSON, with or without padding, and its encoded size may not exceed 256 KiB. After decoding, it must contain:

```text
caCertPem
jwtPublicKey
nodeCertPem
nodeKeyPem
```

For a native deployment, store it in a separate restricted file:

```bash
sudo install -d -o root -g remnanode -m 0750 /etc/remnanode
printf '%s' 'PASTE_THE_COMPLETE_SECRET_KEY' \
  | sudo tee /etc/remnanode/secret.key >/dev/null
sudo chown root:remnanode /etc/remnanode/secret.key
sudo chmod 0640 /etc/remnanode/secret.key
```

The Secret file must be a regular, non-symlink file. Its content may have no trailing newline, one trailing LF, or one trailing CRLF. Internal whitespace is rejected.

Use a YAML mapping for a single-file Docker deployment:

```yaml
environment:
  NODE_PORT: "38329"
  SECRET_KEY: "PASTE_THE_COMPLETE_SECRET_KEY"
  LOW_MEMORY: "1"
```

Do not use the list form `- SECRET_KEY="..."`. In that form, the quote characters become part of the value and cause base64 decoding to fail. Set a Compose or `.env` file containing the Secret to mode `0600`, and never commit it to Git.

## `node.env` syntax and limits

`node.env` uses a restricted dotenv syntax. It is not a shell script:

```env
NODE_PORT=2222
SECRET_KEY=
SECRET_KEY_FILE=/etc/remnanode/secret.key
LOW_MEMORY=1
BODY_LIMIT_MB=
```

Parsing rules:

- Blank lines, comments beginning with `#`, and an optional `export KEY=value` prefix are accepted.
- Values may be unquoted or enclosed in one matching pair of single or double quotes.
- Commands, variable expansion, and shell substitution are never evaluated.
- If a key appears more than once, the last value wins. The installer merges duplicate keys that it manages.
- The file is limited to 1 MiB, 4096 lines, and 256 assignments.
- On Linux, it is opened with `O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`, and its state before and after reading is compared through the same file descriptor.
- Unknown keys count toward file limits but do not enter the Node configuration or pass automatically to rw-core.

The standard native file is owned by `root:remnanode` with mode `0640`.

### Native low-memory example

```env
NODE_PORT=2222
NODE_BIND_ADDR=
SECRET_KEY=
SECRET_KEY_FILE=/etc/remnanode/secret.key

XRAY_BIN=/usr/local/lib/remnanode/rw-core
GEO_DIR=/usr/local/share/remnanode/xray
LOG_DIR=/var/log/remnanode
ASN_DB_PATH=/usr/local/share/remnanode/asn/asn-prefixes.bin
INTERNAL_SOCKET_PATH=/run/remnanode/internal.sock
INTERNAL_REST_TOKEN=

DISABLE_HASHED_SET_CHECK=false
LOW_MEMORY=1
BODY_LIMIT_MB=
```

There is normally no need to add `GOMEMLIMIT=180MiB`; `LOW_MEMORY=1` already supplies the same default Go soft limit. Override it only after measuring resource use.

The maintained native template is [`deploy/node.env.example`](../deploy/node.env.example).

## Docker Compose interpolation

The repository-root `.env.example` is consumed by the Compose CLI. It is not the container's `node.env`, and the Go process does not discover it on its own.

| Variable | Consumer | Description |
| --- | --- | --- |
| `REMNANODE_IMAGE` | Compose | Image tag or `name@sha256:...`; not passed to the Node. |
| `NODE_PORT` | Compose -> Node | Passed into the container at runtime. |
| `NODE_BIND_ADDR` | Compose -> Node | Passed into the container at runtime. |
| `SECRET_KEY` | Compose -> Node | Passed through the container environment and visible in local Docker metadata. |
| `LOW_MEMORY` | Compose -> Node | Defaults to `1` in production templates. |
| `DISABLE_HASHED_SET_CHECK` | Compose -> Node | Defaults to `false` in production templates. |
| `BODY_LIMIT_MB` | Compose -> Node | Leave empty to use the low-memory default. |
| `GOMEMLIMIT` | Compose -> Node | Leave empty to let `LOW_MEMORY` select the default. |

Large fleets of independent nodes do not need an `.env` file. Runtime values can be written directly into the Compose `environment` mapping. Start from [`deploy/compose.single-file.yaml`](../deploy/compose.single-file.yaml) and follow [Docker Compose deployment](deployment-docker.md).

`latest` only changes what the next pull resolves to. It does not replace a running container:

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

## Installer and upgrader configuration

The shell scripts consume the following variables; they are not daemon runtime settings. Some asset settings may be retained in `node.env` for a later `install-xray.sh` run.

| Variable or option | Consumer | Purpose |
| --- | --- | --- |
| `RNL_REPO` | All installer scripts | Release source repository; defaults to `luxiaba/remnanode-lite`. |
| `RNL_TAG` | Install, upgrade, uninstall | Exact tag, such as `vX.Y.Z` or `vX.Y.Z-rnl.N`. |
| `RNL_INSTALL_XRAY=0` / `--skip-xray` | Install | Skip rw-core during a fresh installation. Not recommended for a normal production install. |
| `RNL_UPGRADE_XRAY=1` / `--upgrade-xray` | Upgrade | Upgrade rw-core, geo data, and ASN data together. The default is to preserve the installed rw-core assets. |
| `RNL_INSTALL_ASN=0` | `install-xray` | Skip the ASN database; `asList` degrades to an empty list. |
| `RNL_TMP_ROOT` | Installer | Advanced override for the root-only transaction directory; defaults to `/var/lib/remnanode-installer`. |
| `CUSTOM_CORE_URL` | `install-xray` | Custom HTTPS URL for a Linux core binary. |
| `CUSTOM_CORE_SHA256` | `install-xray` | Required SHA-256 for a custom core. |
| `ASN_DB_URL` | `install-xray` | Custom HTTPS URL for RWASNDB. |
| `ASN_DB_SHA256` | `install-xray` | Required SHA-256 for a custom ASN database. |
| `GEO_ZAPRET_FILE` | Install | Atomically copy a local file as `geo-zapret.dat`. |
| `IP_ZAPRET_FILE` | Install | Atomically copy a local file as `ip-zapret.dat`. |
| `XRAY_CORE_VERSION` / `--version` | `install-xray` | Select an rw-core Release. A version other than the project-pinned version also requires a SHA-256. |
| `XRAY_CORE_SHA256` / `--sha256` | `install-xray` | Required digest for an rw-core Release that is not project-pinned. |

`RNL_ENSURE_SERVICE_STARTED`, `RNL_ENSURE_SERVICE_ENABLED`, and `RNL_EXTERNAL_ASSET_ROLLBACK` belong to the installer's internal transaction protocol. Do not set them manually.

A custom core still uses geo data from the selected rw-core Release, then replaces the core binary with the verified custom binary. Every custom URL must have a corresponding SHA-256; a missing digest fails before any target path is written.

Entry-point semantics are intentionally different. Re-running `install-node*.sh --install` on a complete installation synchronizes rw-core, geo, and ASN assets from the target Release by default. Explicit `--upgrade` and direct `upgrade.sh` runs preserve those assets unless `--upgrade-xray` or `RNL_UPGRADE_XRAY=1` is supplied.

## Version configuration is not version implementation

The project version, official contract version, and rw-core version are separate concepts:

- The project version identifies Releases, binaries, and image tags.
- The contract version identifies the official Node API baseline actually implemented and reported to the Panel.
- The rw-core version identifies the core bundled or installed in the deployment.

Do not infer the contract version from the project version, and do not use an override to claim compatibility before it has been implemented. Inspect the current binary declaration with:

```bash
remnanode-lite version
```

## Next steps

- Container configuration: [Docker Compose deployment](deployment-docker.md)
- systemd and OpenRC: [Native Linux deployment](deployment-native.md)
- Health, logs, and troubleshooting: [Operations guide](operations.md)
