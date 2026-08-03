# Operations and Troubleshooting

[Documentation home](README.md) | [Docker deployment](deployment-docker.md) | [Native deployment](deployment-native.md) | [Configuration](configuration.md)

Remnanode Lite has a deliberately small persistent footprint. Panel remains the source of truth for active proxy configuration, so routine operations focus on four things: the Node process, Panel connectivity, rw-core state, and real proxy traffic.

## What each check proves

| Check | Proves | Does not prove |
| --- | --- | --- |
| Container or service is running | A supervisor sees a live Node process | The process accepts internal health checks |
| Docker health or `rnlctl status --json` is healthy | The Node accepts a request through its private Unix socket and managed state is coherent | Panel can reach the public port |
| Node is online in Panel | mTLS/JWT and the Panel-to-Node path work | rw-core has a working proxy configuration |
| rw-core is online in Panel | Core startup and its internal gRPC path succeeded | Every inbound and outbound route carries traffic |
| A representative client transfers traffic | The tested proxy path works end to end | Every protocol, address family, or route works |

The public `/node/xray/healthcheck` route is authenticated with mTLS and JWT. It is not an anonymous HTTP monitoring endpoint.

## Routine checks

### Docker Compose

Run commands from the directory containing `docker-compose.yaml` and its optional `.env`:

```bash
docker compose ps
docker compose logs --tail=100 remnanode-lite
docker inspect remnanode-lite --format \
  'image={{.Config.Image}} status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}} oom={{.State.OOMKilled}} restarts={{.RestartCount}}'
ss -H -lntp 'sport = :2222'
```

Replace `2222` with the effective `NODE_PORT`. The Compose healthcheck runs `remnanode-lite healthcheck` inside the container. It connects to the private Unix socket with a short deadline; it does not contact Panel.

Check the running identity:

```bash
docker exec remnanode-lite remnanode-lite version
```

### Native Linux

Use the lifecycle view first:

```bash
sudo rnlctl overview
sudo rnlctl status
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl logs node --lines 100
sudo rnlctl logs core-errors --lines 100
ss -H -lntp 'sport = :2222'
```

`overview` is the first human-oriented check. It combines lifecycle and service
state, version and generation information, the configured non-secret listen
endpoint, known problems, and a short state-specific command list. It reads
only local lifecycle status and the safe administrator-controlled
configuration; it does not contact Panel or generate proxy traffic. The command
has no JSON form and its layout is not a parsing interface.

Bare `rnlctl status` now returns a consistent human-readable lifecycle summary instead of proxying raw service-manager output. Its layout is not a parsing interface. `status --json` retains its existing machine-readable schema with deployment state, current and previous generation IDs, version, service manager, enabled/active flags, repair capability, pending operation, and problems. Both exit non-zero for a degraded or recovery-required installation.

`doctor` verifies lifecycle state, generation manifests and file digests, generation links, configuration, the Secret, service state, internal health, and cached repair material. Human output ends with a summary and deterministic `Next` suggestions for known failures; `doctor --json` keeps its existing schema. It does not connect to Panel or generate proxy traffic.

Low-level service-manager views remain useful:

```bash
# systemd
sudo systemctl --no-pager --full status remnanode-lite.service
sudo systemctl show remnanode-lite.service \
  --property=ActiveState,SubState,MainPID,MemoryCurrent,MemoryPeak,TasksCurrent

# Alpine/OpenRC
sudo rc-service remnanode-lite status
```

Use these service-manager commands when you need their full low-level output. Scripts that previously parsed bare `rnlctl status` should move to `rnlctl status --json`.

## `rnlctl` output and automation

`--quiet`/`-q`, `--no-color`, and `--progress auto|plain|never` are global and
may appear anywhere in an `rnlctl` command. Progress goes to stderr; final
results and command data go to stdout. Errors also go to stderr.

`auto` is the default. It uses a live display when stderr is a TTY and stable,
line-oriented stages otherwise; `TERM=dumb` also selects plain output. `plain`
forces line-oriented progress without cursor rewriting. `never` suppresses only
progress. Only transfers with a known total size show percentages, rates, and
estimated time. Lifecycle phases describe real work and are not an overall
percentage or a stable parsing interface.

Successful `install`, `activate`, `upgrade`, `rollback`, and `repair` results add
state-aware `Next` commands. A failed lifecycle mutation may add safe local
`status`, `doctor`, or Node-log diagnostics to stderr. These suggestions are
never executed automatically, are omitted for cancellation errors, and are
suppressed by quiet mode.

Quiet mode overrides the progress choice and suppresses successful mutation
output, `config check` success, and human `overview`/`status`/`doctor`. It does not suppress
help, version, `config show`/`get`, logs, completion, dry-run plans, JSON, or
errors. JSON output disables progress.

Human output uses restrained color only when its own stream is a TTY. Overview,
status, and doctor inspect stdout; progress inspects stderr. `--no-color`, a non-empty
`NO_COLOR`, or `TERM=dumb` disables color on both streams. A redirected stream
does not contain color sequences.

The first `SIGINT`, `SIGTERM`, `SIGHUP`, or `SIGQUIT` requests graceful
cancellation. Context-aware host commands and health checks used for required
cleanup or rollback run under a one-minute recovery deadline. Local filesystem
work may finish its current bounded step before exit. Ctrl-C returns `130` after
that attempt. The signal returns to its default behavior after the first
delivery, so a second signal can force the process to exit immediately; inspect
`status --json` and run `repair` afterward if recovery is required.

Exit codes are normally `0` for success, `1` for runtime failure or an unhealthy result, and `2` for usage errors. `status` and `overview` treat `absent` as valid and return `0`; `overview` also returns `1` when the safe configuration summary cannot be read. Scripts that require an installation must use `status --json` and inspect `installed` or `deployment`. `logs` passes through the exit code from `journalctl` or `tail` once the reader starts, including `128 + signal` when signaled.

For Bash, Zsh, and Fish completion, run `rnlctl completion <shell>` and install the emitted script as described in the [Native deployment guide](deployment-native.md#shell-completion). The command only writes the script to stdout; it does not modify completion directories or shell startup files.

## Logs

### Node output

| Deployment | Command | Storage |
| --- | --- | --- |
| Docker | `docker compose logs -f remnanode-lite` | Docker `json-file`, `2 MiB x 2` in maintained Compose |
| Native systemd | `sudo rnlctl logs node --follow` | Host journald policy |
| Native Alpine/OpenRC | `sudo rnlctl logs node --follow` | `/var/log/remnanode-lite/openrc.log` and `.err.log` |

On a small systemd host, set an appropriate host-wide journald quota and monitor it:

```bash
journalctl --disk-usage
df -h
```

The managed systemd drop-in rate-limits service records on systemd 247 or newer, but rate limiting is not a disk quota.

### rw-core output

Docker uses container-private paths:

```bash
docker exec -it remnanode-lite \
  sh -c 'tail -n 50 -F "$LOG_DIR/xray.out.log" "$LOG_DIR/xray.err.log"'
```

Native deployment uses `rnlctl`:

```bash
sudo rnlctl logs node --since 15m --lines 100
sudo rnlctl logs core --follow
sudo rnlctl logs core-errors --follow
```

`--lines` defaults to `50` and accepts `1..100000`. `--since` accepts a positive Go-style duration such as `15m` or `2h`, not an absolute date or `1d`. It can be combined with `--lines` and `--follow`, and is available only for `node` on systemd. OpenRC Node logs and the file-backed `core`/`core-errors` sources reject it.

Systemd applies `--lines N` to the unit as a whole. OpenRC reads N lines from each of `openrc.log` and `openrc.err.log`; core sources read one current file each. The Native core files are `/var/log/remnanode-lite/xray.out.log` and `xray.err.log`. Each stream keeps one current file and one `.1` file with a 4 MiB rotation threshold, but a normal read does not backfill from `.1`. `--follow` uses `tail -F` and continues across later rotation. Docker places its core log directory on a 28 MiB tmpfs, so recreating the container clears those logs and consumes no persistent disk.

## Start and stop

Docker:

```bash
docker compose restart remnanode-lite
docker compose stop remnanode-lite
docker compose up -d --no-build
docker compose down
```

Native:

```bash
sudo rnlctl restart
sudo rnlctl stop
sudo rnlctl start
```

Use `rnlctl activate` instead of `start` for an installation created with `--prepare-only`.

On SIGTERM or SIGINT, the Node shares one 25-second application shutdown budget across HTTP draining, rw-core process-group termination, plugin cleanup, and private nftables cleanup. Compose allows 35 seconds, systemd allows 30 seconds, and OpenRC uses `TERM/30/KILL/5`. Avoid `kill -9` during routine operations; it bypasses coordinated cleanup and can leave Native lifecycle state requiring repair.

## Docker update and rollback

Choose the image reference according to the rollout:

| Reference | Use |
| --- | --- |
| `name@sha256:<digest>` | Strongest production pin and rollback identity |
| `X.Y.Z` | Exact stable release |
| `X.Y.Z-rnl.N` | Exact preview release |
| `latest` | Opt-in moving stable channel; resolves only when pulled |
| `preview` | Opt-in moving preview channel; not a production rollback reference |
| `sha-<40-character-commit>` | Immutable-by-policy `main` candidate for release verification |
| `edge` | Moving `main` build for short-lived development testing |

For a controlled update:

1. Record the current effective image or manifest digest.
2. Read the target Release notes.
3. Change `REMNANODE_IMAGE` in `.env`, or the intentionally inline `image:` value.
4. Pull and recreate the container.
5. Check health, Panel state, and representative traffic.

```bash
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode-lite
```

`latest` and `preview` do not update a running container. `docker compose restart` also does not pull. A moving tag is resolved only by an explicit pull, and a recreate is required to run the new image.

Rollback by restoring the previously recorded exact tag or digest, then repeat the pull and recreate commands. Do not retag an older image as a release version, and do not rely on what `latest` meant at an earlier date.

Before pruning images, confirm that at least one known-good rollback image is still local:

```bash
docker system df
docker image prune
```

The command above removes dangling images by default. Avoid broad prune options on a small node unless you have checked every retained rollback image.

## Native update, rollback, and repair

Native upgrades accept exact versions only:

```bash
sudo rnlctl upgrade --to 2.8.0-rnl.2 --dry-run
sudo rnlctl upgrade --to 2.8.0-rnl.2 --dry-run --json
sudo rnlctl upgrade --to 2.8.0-rnl.2
```

Dry-run requires root, an existing clean installation, and no pending lifecycle journal. With `--to`, it downloads and statically verifies the complete candidate, then briefly holds the lifecycle lock while checking current state and known host preconditions. It does not create a generation, cache, or transaction journal; change the service; execute the candidate binary; run target health checks; or keep the bundle. `--json` is valid only with `--dry-run`. The check uses temporary disk but does not reserve or guarantee space for the real upgrade, and a successful plan does not guarantee that the later upgrade will succeed. Local `--bundle` plus `--sha256` and `--bundle-root` candidates support the same dry-run.

The complete Node/runtime bundle becomes a new generation. The transaction preserves the service's enabled and active state, validates the selected binary, and waits for internal health before committing. It retains the former generation as `previous`.

Human preflight and upgrade commands report exact-release resolution, download,
verification, and the lifecycle stages they actually perform. Percentage and
ETA are limited to downloads whose total size is known. JSON dry-runs never
include progress output.

Rollback is one command:

```bash
sudo rnlctl rollback
```

If `status --json` reports `recovery-required`, inspect the problem first. Run
repair when status lists a readable `pending` transaction; otherwise use doctor
to review unreadable lifecycle metadata. Do not edit links or state files:

```bash
sudo rnlctl status --json
sudo rnlctl repair
sudo rnlctl doctor
```

Repair uses the verified cached bundle for the committed generation and never upgrades. If that cache is damaged, provide the matching exact archive and digest as described in the [Native deployment guide](deployment-native.md#recover-an-interrupted-operation).

Only one lifecycle mutation runs at a time under
`/run/remnanode-lite-installer/operation.lock`. Wait for the active command.
Never delete the lock or `/var/lib/remnanode-lite-installer/journal.json` to
force another operation through.

## Change configuration

For Docker, edit `.env` or the Compose mapping, then validate and recreate:

```bash
docker compose config --quiet
docker compose up -d --no-build --force-recreate
```

For Native Linux, `/etc/remnanode-lite/node.env` is the only runtime-settings source. `rnlctl config` reads and updates it directly and shows only six non-secret administrator keys. It never displays the Secret or managed internal fields.

Inspect, validate, and change an active installation with:

```bash
sudo rnlctl config show
sudo rnlctl config check
sudo rnlctl config set NODE_PORT=2222 --apply
```

`config show` and `get` report the values stored in `node.env`, not defaults computed by the daemon. `config check` is read-only and includes managed `node.env`/`secret.key` permissions. `set` and `unset` validate the complete candidate before writing. With `--apply`, they restart the active service and wait for internal health; a failure after a change triggers a best-effort attempt to restore the previous file and service. This is not a durable or crash-safe transaction. A stopped or prepared installation rejects `--apply` before writing, so change it without `--apply` and then use `rnlctl start` or `rnlctl activate`.

Manual edits are still supported: preserve `root:remnanode-lite 0640`, run `rnlctl config check`, then run `rnlctl config apply` on an active service. `config apply` cannot roll back a manual edit because it did not capture the previous file.

Rotate the Secret from a protected regular file; it is never accepted as a value argument or printed:

```bash
sudo rnlctl secret set --file /root/new-node-secret.key --apply
```

Remove the source file afterward. Secret replacement has the same active-service restriction and best-effort recovery behavior as `config set --apply`. A non-empty inline `SECRET_KEY` is not allowed in a managed Native configuration. See [Native deployment](deployment-native.md#change-the-port-or-secret) for the complete procedure.

`config check` and `config apply` do not contact Panel or test proxy traffic. Confirm Panel state and representative traffic separately.

When changing `NODE_PORT`, update the Panel record and host firewall to the same value. Both deployment methods use host networking; there is no port translation layer to compensate for a mismatch.

## Resource checks

The maintained Docker and Native service profiles enforce `448 MiB RAM`, no additional service/container swap, `1 CPU`, and `256 PIDs/tasks`. The whole-host `512 MiB / 1 vCPU / 2 GB` target remains an engineering target, not a guarantee for every user count, protocol mix, or plugin configuration.

Docker:

```bash
docker stats --no-stream remnanode-lite
docker inspect remnanode-lite --format \
  'oom={{.State.OOMKilled}} restarts={{.RestartCount}}'
docker system df
df -h
```

systemd:

```bash
systemctl show remnanode-lite.service \
  --property=MemoryCurrent,MemoryPeak,TasksCurrent,CPUUsageNSec
journalctl --disk-usage
df -h
```

On the Alpine/OpenRC service path, the service creates `openrc.remnanode-lite` under the detected unified cgroup v2 root. Before starting, it checks `memory.max=469762048`, `memory.swap.max=0`, `cpu.max=100000 100000`, `pids.max=256`, its own cgroup membership, writable parent `cgroup.procs`, and writable service `cgroup.kill`.

Do not build the project on a production host constrained to 2 GB of disk. The Go toolchain, module cache, BuildKit cache, and intermediate assets can exceed the runtime budget.

## Network and security boundary

Both deployment methods run in the host network namespace. `CAP_NET_ADMIN` allows the Node to manage its private nftables table and destroy selected TCP sockets through `NETLINK_SOCK_DIAG`; `CAP_NET_BIND_SERVICE` lets rw-core bind ports below 1024.

- Run only a trusted exact release or verified manifest digest.
- Do not use `privileged: true`, run the Native service as root, or add unrelated capabilities.
- Restrict the Node API port to Panel addresses when practical.
- Open proxy ports according to the configuration sent by Panel.
- Protect Docker socket access, root access, the Compose directory, and `/etc/remnanode-lite`.
- The project does not own the host firewall or sysctl policy beyond its private runtime nftables table.

## Common failures

### `illegal base64 data at input byte 0`

The Secret is not valid base64/base64url, is truncated, contains whitespace, or includes literal Compose quote characters. Obtain the complete Secret from Panel again. In Compose, use the mapping form shown in the [configuration reference](configuration.md#docker).

### `SECRET_KEY missing required fields`

The value decodes, but it is not the complete Secret for this Node. A JWT, public key, certificate, or private-key fragment is insufficient.

### `address already in use`

Another host process owns the configured port:

```bash
ss -H -lntp 'sport = :2222'
```

Stop the conflicting service or change the Node port in Panel, host configuration, and firewall together. Do not run the official and Lite containers on the same host ports.

### Healthy locally, offline in Panel

Check, in order:

1. `NODE_PORT` matches the Panel record.
2. The intended process owns that port.
3. Firewall and routing allow Panel to reach it.
4. The Secret belongs to this Node.
5. System time is correct.
6. Node logs contain no TLS, JWT, or listen error.

Local health does not exercise any of these external links.

### Node online, rw-core offline

Read `core-errors`, check for port conflicts, and review the configuration sent by Panel. Low-memory mode permits a longer readiness window for large configurations. Do not diagnose failure solely from the first few seconds after a restart.

### `CAP_NET_ADMIN not available`

Restore the repository-supplied Compose capabilities or repair the managed Native service definition. Without the capability, the base Panel API may start, but nftables plugins and connection destruction are unavailable. Do not switch to a privileged container or root service to hide the error.

### ASN database unavailable

The Node continues, but plugin `asList` resolves to an empty list. Docker and Native release bundles include one pinned database. Recreate the container from a verified image, or run `rnlctl repair`/an exact Native upgrade; do not download an unpinned database into the active generation.

### Alpine/OpenRC host qualification fails

The host's Alpine release must appear in the [Native host matrix](deployment-native.md#native-host-matrix). It must be a persistent `sys` installation booted through the normal init/OpenRC stack, run Linux 5.14 or newer, and expose unified cgroup v2 with usable CPU, memory, PID, swap-limit, parent-membership, and service-kill controls. A successful `--prepare-only` install does not prove these runtime conditions because it does not start the service. If `activate` or `start` rejects the host, use an environment that exposes the complete contract or choose a maintained systemd host or Docker deployment. Do not bypass the check; the documented resource and cleanup behavior would no longer hold.

### Native mutation says repair is required

An operation left a durable journal or managed state no longer matches the selected generation. Run `rnlctl status --json`, preserve its output for diagnosis, and use `rnlctl repair`. Do not remove files from `/usr/local/lib/remnanode-lite` or `/var/lib/remnanode-lite-installer` by hand.

## Backup scope

Back up only the material needed to recreate the deployment:

- Docker: Compose file, optional `.env`, and the current exact image tag or digest.
- Native: `/etc/remnanode-lite/node.env`, `/etc/remnanode-lite/secret.key`, and the current exact release version.
- Fleet operations: the previous known-good exact version or digest.

Protect Secret backups as private-key material. Do not back up `/run`, Docker tmpfs logs, rw-core configuration sent by Panel, or Native generation directories as a substitute for release assets and `rnlctl` state.
