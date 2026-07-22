# Configuration Reference

[Documentation home](README.md) | [Docker deployment](deployment-docker.md) | [Native deployment](deployment-native.md) | [Operations](operations.md)

Most nodes need only two values: the port configured in Remnawave Panel and that Node's complete Secret Key. The maintained Docker and Native templates set the runtime paths and small-server profile for you.

## Where configuration comes from

The daemon reads one bounded environment-style data file, then applies known, non-empty process environment variables on top of it:

1. `REMNANODE_ENV`, when the process explicitly sets it;
2. `/etc/remnanode-lite/node.env`, when it exists;
3. `.env` in the working directory.

Only recognized keys affect the daemon. The file is parsed as data; it is never sourced by a shell. Keep each setting on one `KEY=value` line and avoid shell expansion, command substitution, or multiline values.

Native services start with a clean process environment and set only `REMNANODE_ENV=/etc/remnanode-lite/node.env` plus a minimal identity and `PATH`. This prevents an inline Secret or unrelated file entries from being exported to the Node or rw-core process environment.

Docker Compose handles `.env` differently: Compose reads it for YAML interpolation before the container starts. An exported shell variable wins over the same `.env` key. The maintained Compose file then passes only the seven runtime values listed in its `environment` mapping; it does not inject `.env` wholesale.

## Runtime settings

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `NODE_PORT` | Yes | None in the daemon; maintained templates use `2222` | HTTPS port used by Panel to reach the Node. Must match the Panel setting. |
| `NODE_BIND_ADDR` | No | Empty | Local IPv4 or IPv6 address to bind. Empty listens on all local addresses. |
| `SECRET_KEY` | Conditional | Empty | Complete base64/base64url Panel Secret. Primarily for Docker. A non-empty value takes precedence over `SECRET_KEY_FILE`. |
| `SECRET_KEY_FILE` | Conditional | Empty | Read the complete Secret from a bounded regular file. Native uses `/etc/remnanode-lite/secret.key`. |
| `XRAY_BIN` | No | `/usr/local/lib/remnanode-lite/current/lib/rw-core` | Managed rw-core executable. Docker overrides this with its container-private path. |
| `GEO_DIR` | No | `/usr/local/lib/remnanode-lite/current/share/xray` | Directory containing `geoip.dat` and `geosite.dat`. |
| `LOG_DIR` | No | `/var/log/remnanode-lite` | rw-core output directory. |
| `ASN_DB_PATH` | No | `/usr/local/lib/remnanode-lite/current/share/asn/asn-prefixes.bin` | ASN prefix database used by plugin `asList`. A missing database degrades that list to empty. |
| `INTERNAL_SOCKET_PATH` | No | `/run/remnanode-lite/internal.sock` | Private filesystem Unix socket used by rw-core config/webhooks and the local healthcheck. |
| `INTERNAL_REST_TOKEN` | No | Random value generated at startup | Bearer token for the private Unix HTTP service. Leave empty unless debugging a controlled setup. |
| `DISABLE_HASHED_SET_CHECK` | No | `false` | Debug switch. When true, every start request restarts rw-core instead of using the configuration hash. |
| `LOW_MEMORY` | No | `false` in the daemon; maintained Docker and Native templates use `1` | Enables the 512 MiB profile: 180 MiB Go soft memory limit, 16 MiB request budget, and longer rw-core readiness allowance. |
| `BODY_LIMIT_MB` | No | Automatic | Global request budget in MiB. Automatic means 16 MiB in low-memory mode and 256 MiB otherwise; route-specific limits may be lower. |
| `GOMEMLIMIT` | No | Automatic | Go runtime soft memory limit as bytes or `KiB`, `MiB`, `GiB`, `TiB`; `off` disables it. Overrides the low-memory default. |
| `NODE_CONTRACT_VERSION` | No | Compiled `ContractVersion` | Version reported to Panel. Use only for controlled compatibility debugging. |
| `XRAY_CORE_VERSION` | No | Probed from rw-core | Core version override used when probing cannot be trusted or performed. Use only for debugging. |

Boolean values accept `true/false`, `1/0`, or `yes/no`, case-insensitively. `NODE_PORT` must be between `1` and `65535`. `BODY_LIMIT_MB` accepts `1..1024`, but cannot exceed `16` when `LOW_MEMORY=1`; empty or `0` selects the automatic value.

`GOMEMLIMIT` controls memory managed by the Go runtime. It is not a container, service, or whole-process RSS limit. rw-core, native allocations, temporary files, and the host remain outside it. The maintained service/container limit is still `448 MiB`.

## The Panel Secret

The Secret is the complete value issued for one Node by Remnawave Panel. It contains the material used for mTLS and JWT authentication. A JWT, certificate, key, or shortened fragment is not interchangeable with the complete Secret.

### Docker

Put the Secret in the same-directory mode-`0600` `.env` file:

```env
NODE_PORT=38329
SECRET_KEY=PASTE_THE_COMPLETE_PANEL_SECRET_KEY
```

The Compose mapping form is important:

```yaml
environment:
  SECRET_KEY: "${SECRET_KEY:?set SECRET_KEY in .env}"
```

Do not use this list form:

```yaml
- SECRET_KEY="..."
```

In list form, quote characters can become part of the value and cause base64 decoding to fail. Docker also stores injected environment values in local container metadata, so restrict the Compose directory and access to the Docker socket.

### Native Linux

Native lifecycle management keeps the Secret out of `node.env`:

```env
SECRET_KEY=
SECRET_KEY_FILE=/etc/remnanode-lite/secret.key
```

The installer writes `/etc/remnanode-lite/secret.key` as `root:remnanode-lite 0640` after validating it. Use `--secret-file` during install or activation; do not pass the Secret itself as an argument.

To rotate it safely, follow [Change the port or Secret](deployment-native.md#change-the-port-or-secret).

## Docker Compose variables

The production Compose files interpolate these values:

| Variable | Compose fallback | Passed to the Node | Notes |
| --- | --- | --- | --- |
| `REMNANODE_IMAGE` | Release file: exact release; repository template: current exact default; single-file template: `latest` | No | Image tag or `name@sha256:...`. Prefer an exact version or digest for a fleet. |
| `NODE_PORT` | `2222` | Yes | Panel-to-Node port. |
| `NODE_BIND_ADDR` | Empty | Yes | Optional local bind address. |
| `SECRET_KEY` | No fallback | Yes | Compose fails interpolation when it is missing or empty. |
| `LOW_MEMORY` | `1` | Yes | Small-server policy. |
| `DISABLE_HASHED_SET_CHECK` | `false` | Yes | Debug only. |
| `BODY_LIMIT_MB` | Empty | Yes | Empty lets the daemon select the low-memory default. |
| `GOMEMLIMIT` | Empty | Yes | Empty lets the daemon select 180 MiB in low-memory mode. |

Precedence for Compose interpolation is shell environment, then `.env`, then the `${NAME:-fallback}` in YAML. A blank value also selects the `:-` fallback. Run `docker compose config --quiet` to validate syntax without printing the expanded Secret.

The image uses these container-private paths. They carry the same project name
as the Native layout, but are still inside the image filesystem:

```text
XRAY_BIN=/usr/local/lib/remnanode-lite/rw-core
GEO_DIR=/usr/local/share/remnanode-lite/xray
ASN_DB_PATH=/usr/local/share/remnanode-lite/asn/asn-prefixes.bin
LOG_DIR=/var/log/remnanode-lite
INTERNAL_SOCKET_PATH=/run/remnanode-lite/internal.sock
```

They are internal to the published image and do not conflict with the Native host layout. The maintained Compose tmpfs mounts and log commands already match them; keep overrides consistent with the image.

## Native `node.env`

The maintained template is [`deploy/node.env.example`](../deploy/node.env.example). `rnlctl` owns the settings that select the active generation:

```env
NODE_PORT=2222
SECRET_KEY=
SECRET_KEY_FILE=/etc/remnanode-lite/secret.key
XRAY_BIN=/usr/local/lib/remnanode-lite/current/lib/rw-core
GEO_DIR=/usr/local/lib/remnanode-lite/current/share/xray
LOG_DIR=/var/log/remnanode-lite
ASN_DB_PATH=/usr/local/lib/remnanode-lite/current/share/asn/asn-prefixes.bin
INTERNAL_SOCKET_PATH=/run/remnanode-lite/internal.sock
LOW_MEMORY=1
```

The lifecycle engine rewrites managed path keys during installation and rejects duplicate managed assignments. Keep administrator choices such as `NODE_BIND_ADDR`, `BODY_LIMIT_MB`, and `GOMEMLIMIT` in the same file, but do not redirect managed paths to a system-wide Xray installation. Bundled Node and runtime assets are one tested generation.

The file and Secret must remain regular, non-symlink files. The daemon opens them with no-follow semantics, limits `node.env` to 1 MiB, and verifies that each file did not change while it was read.

## Changing settings

### Docker

Edit `.env` or an intentionally inline Compose mapping, validate, then recreate:

```bash
docker compose config --quiet
docker compose up -d --no-build --force-recreate
```

`docker compose restart` does not reread the Compose model and does not pull a new image.

### Native Linux

Edit `/etc/remnanode-lite/node.env` as root, keep ownership `root:remnanode-lite` and mode `0640`, then validate and restart:

```bash
sudo rnlctl doctor
sudo rnlctl restart
```

When changing `NODE_PORT`, update the Panel and host firewall at the same time. With host networking, neither Docker nor the Native service translates ports.

## Build and test variables

The following variables belong to maintainers and CI, not production daemon configuration:

| Variable | Purpose |
| --- | --- |
| `REMNANODE_OFFICIAL_SOURCE` | Path to the pinned official Node source used by contract evidence tests. |
| `REMNANODE_CONTRACT_CA`, `REMNANODE_CONTRACT_CERT`, `REMNANODE_CONTRACT_KEY` | mTLS inputs for the black-box contract probe. |
| `RNL_ASSET_CACHE_DIR` | Content-addressed cache used while building release assets. |
| `RNL_OFFLINE_BUILD=1` | Forbid network access during Native bundle construction and require a complete cache. |
| `SOURCE_REVISION`, `SOURCE_DATE_EPOCH` | Reproducible release metadata inputs. |

Runtime asset versions and digests are not configurable installer environment variables. They are pinned together in [`release/runtime-assets.lock.json`](../release/runtime-assets.lock.json), embedded in every Native generation and Docker image, and changed through the normal review and release process.
