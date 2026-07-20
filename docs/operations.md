# Operations Guide

[Back to the documentation index](README.md)

This guide covers the day-to-day work of running a node: checking its state, reading logs, updating or rolling back, and finding the cause of common failures.

## Runtime state model

Remnanode Lite manages two different levels of process state:

- The Node is the long-running HTTPS service responsible for authenticating Panel requests, statistics, plugins, and the rw-core lifecycle.
- rw-core runs only after the Panel sends `/node/xray/start`.

The Node does not persist the complete Xray configuration received from the Panel. After a container, service, or host restart, the Node comes online and initially reports the core offline. A later Panel health cycle sends the configuration again. This is the normal recovery path aligned with the official Node.

"Process running," "container healthy," "node online in the Panel," and "rw-core online" are different states:

| Layer | What it proves | What it does not prove |
| --- | --- | --- |
| Service or container running | The Node process still exists | Listening port, authentication, or core state |
| Compose `healthy` | The internal Unix listener accepted an active healthcheck connection | Panel reachability, mTLS/JWT, or rw-core online |
| Node owns the TCP port | The Node is listening on the configured port | Correct Secret, Panel network path, or authentication |
| Node online in the Panel | The Panel communicated with the Node over mTLS and JWT | Reachability of every proxy inbound port |
| Core online in the Panel | rw-core started and passed internal gRPC readiness | Correct behavior of every proxy protocol and external network path |

`/node/xray/healthcheck` is a Panel API protected by mTLS and JWT, not an anonymous HTTP probe. Do not send an ordinary curl request to it from external monitoring.

## Routine status checks

### Docker Compose

```bash
docker compose ps
docker compose logs --tail=100 remnanode
ss -H -lntp 'sport = :38329'
```

Replace `38329` with the configured `NODE_PORT`. The Compose healthcheck runs this command inside the container:

```text
remnanode-lite healthcheck
```

The command connects to the Unix socket at `INTERNAL_SOCKET_PATH` with a two-second timeout. This shows that the Node is accepting internal connections, not just that the socket file exists. It does not test the Panel network path, mTLS/JWT, or registration. If Compose reports `healthy` while the Panel shows the node offline, check the port, firewall, Secret, and Panel settings below.

### systemd

```bash
sudo systemctl --no-pager status remnawave-node
sudo systemctl show remnawave-node \
  --property=ActiveState,SubState,MainPID,MemoryCurrent,TasksCurrent
sudo ss -H -lntp 'sport = :2222'
sudo remnanode-lite doctor
```

### OpenRC

```bash
rc-service remnawave-node status
ss -H -lntp 'sport = :2222'
remnanode-lite doctor
```

`doctor` checks configuration, Secret format, rw-core, geo data, ASN data, nft, ss, and current process capabilities. It does not connect to the Panel and does not prove that the core has started. The current implementation also checks the systemd unit, so the missing-unit WARN can be ignored on OpenRC; ERROR findings require action.

To inspect a different native configuration file:

```bash
sudo remnanode-lite doctor --env /path/to/node.env
```

Container deployments normally do not use `doctor` as their healthcheck. The default image has no `/etc/remnanode/node.env`, because configuration comes from environment variables. Use Compose health, Node logs, and Panel state.

## CLI command reference

Running `remnanode-lite` without arguments starts the daemon. Other subcommands provide read-only diagnostics, installer validation, or explicit administrative operations:

| Command | Purpose | Important behavior |
| --- | --- | --- |
| `remnanode-lite version` | Print the project version and compiled default contract version | Does not parse `node.env` or read the process environment, so it is suitable for a binary smoke test. The daemon's reported value can still be overridden by validated `NODE_CONTRACT_VERSION` configuration. |
| `remnanode-lite doctor [--env PATH]` | Check native configuration, Secret, assets, tools, and capabilities | Does not connect to the Panel or start rw-core. |
| `remnanode-lite validate-secret` | Validate and canonicalize a Secret from stdin without printing it | Suitable before writing or restarting; success exits with status 0. |
| `remnanode-lite canonicalize-secret <path\|->` | Write a canonicalized Secret to stdout | Output is still the complete secret material. Redirect it only to a restricted file and never to logs. |
| `remnanode-lite kill-sockets` | Interactively read one IP and destroy connected TCP sockets whose local or remote address matches it | Requires `CAP_NET_ADMIN`. The CLI calls the kernel adapter directly and does not apply the application layer's local-address protection. |
| `remnanode-lite release-url <tag> <arch>` | Produce a validated Release archive URL | Used by installers; invalid tags or architectures fail. |
| `remnanode-lite install-script-url <tag> <script>` | Produce a validated installer script URL | Accepts only allowlisted script names and is primarily used for bootstrap. |

Display the argument summary:

```bash
remnanode-lite --help
```

Do not start a second daemon inside a running production container; process and nftables ownership assume one instance.

`kill-sockets` is an administrative tool, not a health check. It matches the local **or** remote address across the whole network namespace and does not filter by PID or container. A host-local address can therefore close unrelated connections. Use it only on an isolated node and only after confirming that the address is not local.

## Logs

### Node logs

| Deployment | Command | Storage |
| --- | --- | --- |
| Docker | `docker compose logs -f remnanode` | Docker `json-file`; the production template uses `2 MiB x 2`. |
| systemd | `journalctl -u remnawave-node -f` | Controlled by host journald quotas. |
| OpenRC | `tail -F /var/log/remnanode/openrc.log` | File checked for rotation by the Node every 10 seconds. |

systemd's `LogRateLimitIntervalSec=30s` and `LogRateLimitBurst=200` limit message count over time; they are not a long-term disk limit. On a 2 GB host, configure an appropriate total journald quota and inspect it regularly:

```bash
journalctl --disk-usage
df -h
```

### rw-core logs

rw-core stdout and stderr are stored separately:

```text
/var/log/remnanode/xray.out.log
/var/log/remnanode/xray.err.log
```

Docker:

```bash
docker exec -it remnanode \
  tail -n 50 -F /var/log/remnanode/xray.out.log

docker exec -it remnanode \
  tail -n 50 -F /var/log/remnanode/xray.err.log
```

Native deployment:

```bash
remnanode-xlogs
remnanode-xerrors
```

Each rw-core stream keeps a current file and one `.1` file, both limited to 4 MiB. Temporary rotation files can briefly add about 8 MiB beyond the normal 16 MiB total.

Docker stores `/var/log/remnanode` on a 28 MiB tmpfs, so recreating the container clears the logs without using persistent disk. OpenRC also writes `openrc.log` and `openrc.err.log`; it checks them every 10 seconds and copy-truncates at 4 MiB. A file may grow slightly past that threshold between checks.

## Start, stop, and recreate

Docker:

```bash
docker compose restart remnanode
docker compose stop remnanode
docker compose up -d --no-build
docker compose down
```

systemd:

```bash
sudo systemctl restart remnawave-node
sudo systemctl stop remnawave-node
sudo systemctl start remnawave-node
```

OpenRC:

```bash
rc-service remnawave-node restart
rc-service remnawave-node stop
rc-service remnawave-node start
```

After SIGTERM or SIGINT, the Node uses one 25-second application shutdown budget: stop accepting requests, stop the rw-core process group, then clean up plugins and the private nftables table. Compose supplies a 35-second grace period, systemd supplies a 30-second outer timeout, and OpenRC uses `TERM/30/KILL/5`. Do not use `kill -9` for routine restarts.

## Docker update and rollback

### Image references

| Reference | Property | Recommended use |
| --- | --- | --- |
| `latest` | Moves with the newest stable Release | Small nodes that intentionally follow the stable channel after an explicit pull. |
| `X.Y.Z` | Formal project version aligned with the corresponding official release | Pin an official-aligned build. |
| `X.Y.Z-rnl.N` | Independent project iteration | Precise deployment and incident correlation. |
| `sha-<commit>` | Candidate built from `main` | Real-server acceptance before formal release. |
| `candidate-sha-<commit>` | Manually dispatched independent candidate | Acceptance when the automatic candidate is absent or must be rebuilt. |
| `name@sha256:<digest>` | Registry content address | Strongest immutable pin and rollback identity. |

By project policy, exact tags and `sha-*` tags should not move, but registry tags are not technically immutable. Pin a manifest digest for strict reproduction.

### Controlled update

1. Record the current Compose file and image reference.
2. Read the target Release notes and identify contract, rw-core, and configuration changes.
3. Change `image:` to the new exact tag or digest.
4. Pull and force-recreate.
5. Check the container, port, logs, and Panel.

```bash
cp -p docker-compose.yaml docker-compose.yaml.rollback

docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode
```

Tracking `latest` still requires an explicit pull and recreate. `docker compose restart` alone never checks for a new image.

### Rollback

Restore the previous Compose file, or change `image:` back to a verified exact tag or digest:

```bash
cp -p docker-compose.yaml.rollback docker-compose.yaml
chmod 600 docker-compose.yaml

docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
```

Do not move an old tag to implement rollback. Before cleanup, record one verified prior version tag or manifest digest and confirm that the corresponding image is still local. Always retain at least this one explicit rollback image:

```bash
docker system df
docker image prune
```

By default, `docker image prune` removes only dangling images. Do not use broad options that delete every unused image unless you have verified individually that the rollback image will remain.

Do not build from source on a production host with only 2 GB of disk. The Go toolchain, base layers, and BuildKit cache can substantially exceed the runtime disk budget.

## Native update and rollback

Native upgrades use transactional scripts. Do not overwrite a running binary manually. See [Native Linux deployment](deployment-native.md#upgrade) for commands and transaction semantics.

Operational rules:

- Pin the target Release tag; never download from a branch URL.
- Explicit `--upgrade` preserves rw-core by default. Add `--upgrade-xray` only when required by the Release notes. Do not substitute a repeated `--install`, because `--install` on a complete installation synchronizes rw-core, geo, and ASN assets by default.
- A service that was stopped before an explicit upgrade remains stopped.
- The transaction commits only when a target-version process actually owns the configured port.
- On failure, read the reported backup directory and rollback result before changing anything. Do not delete the only retained backup.
- Select only a real older Release for rollback. Restore matching configuration and core assets when compatibility requires it.

Installation, upgrade, rw-core updates, and uninstall share `/run/lock/remnanode-installer.lock`. If an installer is active, wait for it to finish. Do not remove the lock file or start a second mutating entry point in parallel.

## Change the Secret or port

### Docker

Edit the Compose mapping and recreate:

```bash
chmod 600 docker-compose.yaml
docker compose config --quiet
docker compose up -d --no-build --force-recreate
```

Do not run `docker compose config` without `--quiet`; the expanded Secret would be printed to the terminal or collected logs.

### Native deployment

Write the new Secret to a temporary file readable only by the current user, validate it, then replace the production file atomically:

```bash
umask 077
secret_tmp="$(mktemp)"
printf '%s' 'PASTE_THE_NEW_COMPLETE_SECRET_KEY' >"$secret_tmp"
remnanode-lite validate-secret <"$secret_tmp"

sudo install -o root -g remnanode -m 0640 \
  "$secret_tmp" /etc/remnanode/secret.key.new
sudo mv -f /etc/remnanode/secret.key.new /etc/remnanode/secret.key
rm -f "$secret_tmp"
```

Then inspect effective assignments in `/etc/remnanode/node.env`. A non-empty `SECRET_KEY` takes precedence over `SECRET_KEY_FILE`. When migrating from legacy inline configuration, clear the former and point the latter at the file just replaced:

```env
SECRET_KEY=
SECRET_KEY_FILE=/etc/remnanode/secret.key
```

The last occurrence of a duplicated key wins, so remove stale duplicate assignments. Validate configuration before restarting:

```bash
sudo remnanode-lite doctor
sudo systemctl restart remnawave-node
```

On OpenRC, replace the last line with:

```bash
rc-service remnawave-node restart
```

If validation, installation, or configuration editing fails partway through, remove the temporary file from this attempt and do not restart the service. Do not replace only `secret.key` while leaving a non-empty inline `SECRET_KEY`; the Node would continue using the old inline value.

After changing `NODE_PORT`, also update the Panel node configuration and host firewall. With host networking, Compose `ports:` cannot correct a mismatch.

## Resources and disk

Production configuration targets a whole machine with `512 MiB RAM / 1 vCPU / 2 GB disk`, but this is an engineering budget, not a performance guarantee for every host. The Docker daemon, kernel, and other system services consume capacity outside the container's 448 MiB limit.

Docker:

```bash
docker stats --no-stream remnanode
docker system df
df -h
```

systemd:

```bash
systemctl show remnawave-node \
  --property=MemoryCurrent,MemoryPeak,TasksCurrent,CPUUsageNSec
journalctl --disk-usage
df -h
```

OpenRC with cgroup v2:

```bash
service_cgroup=/sys/fs/cgroup/openrc.remnawave-node
cat "${service_cgroup}/memory.current"
cat "${service_cgroup}/memory.peak"
cat "${service_cgroup}/pids.current"
```

Some environments use `/sys/fs/cgroup/unified` as the cgroup root. The OpenRC service validates its actual path and every resource limit before startup.

## Network and security boundary

Docker uses host networking, so `CAP_NET_ADMIN` applies to the host network namespace. The capability is required by the nftables plugin and socket destruction, and it means the container must be treated as a trusted network-management component:

- Run only images published and verified by this project.
- Do not use `privileged: true` or add unrelated capabilities.
- Allow the Node API port through the host firewall only from Panel addresses.
- Open proxy inbound ports according to the configuration actually sent by the Panel.
- Restrict the Docker socket and host administrator access. A host administrator can read an inline Secret through Docker inspect.

Native services run as a non-root user with the same two minimal capabilities. Without `CAP_NET_ADMIN`, basic Node connectivity may still work, but nftables plugins and connection destruction degrade.

## Common failures

### `illegal base64 data at input byte 0`

The common cause is a Compose list value that includes literal quotes:

```yaml
- SECRET_KEY="..."
```

Use a mapping instead:

```yaml
SECRET_KEY: "..."
```

If validation still fails, obtain the complete Secret from the Panel again and check for leading or trailing spaces, truncation, or multiline wrapping.

### `SECRET_KEY missing required fields`

The value decodes as base64, but its JSON is not the complete Secret shown for the current node in the Panel. Regenerate or copy the full node Secret; do not provide only a JWT, public key, or individual certificate.

### `address already in use`

With host networking, another host process already owns the port:

```bash
ss -H -lntp 'sport = :38329'
```

Stop the conflicting service, or change the Node configuration, Panel node port, and firewall together.

### Container healthy, but Panel offline

Check in order:

1. `NODE_PORT` exactly matches the Panel.
2. The intended Node process owns the host port.
3. Firewall and routing allow the Panel to reach the node.
4. The Secret belongs to this node and system time is correct.
5. Node logs contain no TLS, JWT, or listen errors.

Compose health checks only the internal socket and does not cover these paths.

### Node online, but rw-core offline

Immediately after a restart, wait for the next Panel health cycle. If rw-core remains offline:

```bash
docker exec -it remnanode \
  tail -n 100 /var/log/remnanode/xray.err.log
```

For a native deployment, run `remnanode-xerrors`. Check the rw-core binary, geo data, port conflicts, and configuration sent by the Panel. Low-memory mode permits up to 90 seconds for readiness; do not declare failure only a few seconds into a large configuration startup.

### `CAP_NET_ADMIN not available`

Restore the repository-supplied Compose capabilities or native service definition and restart. Do not switch to `privileged: true` to suppress the warning. Without this capability, nftables and `NETLINK_SOCK_DIAG` socket destruction are unavailable.

### `ASN database unavailable`

The Node continues to run, but the plugin `asList` shared list is empty. The Docker image should contain the database. For a native deployment, repeat the upgrade for the target Release with `--upgrade-xray`, or use an `ASN_DB_URL` and `ASN_DB_SHA256` pair whose checksum has been verified.

### OpenRC reports a cgroup controller failure

Confirm that the host uses cgroup v2, that OpenRC has delegated memory, CPU, and PIDs controllers, and that these values are effective in the service cgroup:

```text
memory.max=469762048
memory.swap.max=0
cpu.max=100000 100000
pids.max=256
```

Do not bypass the startup validation. Repair the host cgroup configuration or use a supported Docker or systemd deployment.

### Upgrade or uninstall refuses to continue

The scripts fail conservatively when they cannot reliably establish service state, Node or rw-core exit, lock ownership, or filesystem safety. Resolve the specific condition reported in the log. Do not manually remove the installer lock or transaction backups, and do not overwrite files while processes remain alive.

## Backup scope

Very little persistent state needs backup:

- Single-file deployment: the mode-`0600` Compose file, or Compose plus `.env`.
- Native deployment: `/etc/remnanode/node.env` and `/etc/remnanode/secret.key`.
- Rollback identity: the current image digest or project Release tag.

Do not back up `/run/remnanode`, Docker tmpfs logs, or Xray runtime configuration sent by the Panel. Protect Secret backups with the same encryption, access-control, and destruction policy used for other private keys.
