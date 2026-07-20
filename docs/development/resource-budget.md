# 512 MiB Resource Budget and M6-M8 Baselines

[Back to developer documentation](README.md) · [Operations and troubleshooting](../operations.md)

This document contains dated engineering measurements and the current resource policy. Measurements apply only to the listed commit era, toolchain, architecture, and test assets. They provide engineering context; they are not automatically runtime evidence for a later release candidate.

## Acceptance Boundary

For `v2.8.0`, the only blocking resource/runtime profile is `docker-production-smoke-v1` on the frozen candidate: run the digest-pinned production Compose deployment on `amd64` against a real Panel and real proxy traffic, record observed memory and PID usage, and confirm that the container remains running and healthy with zero OOM kills and restarts. The [acceptance protocol](release-acceptance.md#docker-production-smoke) owns the complete requirement set, including at least 600 seconds under the exact host, container-limit, health, and readiness thresholds.

`arm64-production-runtime`, `native-systemd-install`, `native-openrc-install`, `50000-user-load`, `24h-soak`, and `fault-and-rollback-injection` are deferred, non-blocking follow-up profiles. Results from those profiles must not be implied when they have not been run against the current candidate.

The production target remains a whole machine with `512 MiB RAM / 1 vCPU / 2 GB disk`. The dated M6 engineering gate placed the Node test process and a real rw-core in the same cgroup with these limits:

- A `448 MiB` hard memory limit, leaving at least `64 MiB` for the host kernel and base services.
- `1 CPU`, `256` PIDs, no swap, and no external network access.
- A read-only root filesystem with one `/tmp:size=64m` test tmpfs.
- `LOW_MEMORY=1`, which gives the Go runtime a `180 MiB` soft memory limit.
- A large configuration containing `50,000` VLESS users.

The historical gate is [`scripts/test-low-memory.sh`](../../scripts/test-low-memory.sh), and the Linux integration test is [`internal/xray/resource_linux_integration_test.go`](../../internal/xray/resource_linux_integration_test.go). The M6 run also verified system statistics, inbound user counts, VLESS hot add/remove, and user-IP statistics RPCs through the minimal protobuf wire client.

Production Compose uses a different tmpfs layout that better reflects deployment: `/run`, `/tmp`, and rw-core logs total `48 MiB`, and logs are not written to a persistent volume. The historical gate's single 64 MiB `/tmp` is a test fixture and must not be described as a field-by-field reproduction of the Compose layout. The blocking `amd64` smoke uses the production Compose layout but does not have to repeat the historical 50,000-user workload.

The 2026-07-15 M6 figures and 2026-07-19 M7 init snapshots below predate the current M8 candidate and remain engineering baselines only. They are not current-candidate runtime evidence, and repeating them is not a prerequisite for `2.8.0`.

## M6 Fixed Test Assets (2026-07-15 Engineering Baseline)

- Date: 2026-07-15
- Container architecture: Linux arm64
- Go: `go1.26.5`
- Docker Engine: `29.5.2`
- rw-core: `v26.6.27`
- Official asset: `Xray-linux-arm64-v8a.zip`
- Asset SHA-256: `13a251379bea366c2cf10363ad71e75734193d401f26f518bf0c25e5c8f8c931`

Command:

```bash
scripts/test-low-memory.sh \
  --rw-core /path/to/rw-core-v26.6.27 \
  --users 50000 \
  --memory 448
```

## M6 Measured Results

`cgroup_current` and `cgroup_peak` include the Node test process, rw-core, file-backed pages, and container overhead. `node_test_rss` covers only the Node test process RSS. The gate therefore evaluates `cgroup_peak`.

| Stage | cgroup current | cgroup peak | Node test RSS |
| --- | ---: | ---: | ---: |
| Idle, core not started | 40.3 MiB | 44.3 MiB | 11.1 MiB |
| Start with 1k users | 50.2 MiB | 51.1 MiB | 13.2 MiB |
| Unchanged 1k configuration sync | 50.2 MiB | 51.1 MiB | 13.4 MiB |
| Forced restart with 50k users | 102.2 MiB | 143.9 MiB | 22.6 MiB |
| 50k-user hot add/remove and statistics | 102.3 MiB | 143.9 MiB | 22.6 MiB |

The 50k-user peak is `32.1%` of the budget, leaving about `304 MiB` below the `448 MiB` gate. The unchanged sync did not raise the peak, which confirms that active configuration release and hash-only retained state were working in that baseline.

## M6 Binaries and Disk

Using the same Go toolchain and `CGO_ENABLED=0 go build -trimpath -ldflags='-s -w'`, compared with the pre-optimization engineering baseline:

| Architecture | Baseline | M6 | Reduction |
| --- | ---: | ---: | ---: |
| linux/arm64 | 17,563,810 B | 12,320,930 B | 29.9% |
| linux/amd64 | 18,874,530 B | 13,176,994 B | 30.2% |

## M7 Init Snapshots (2026-07-19 Engineering Baseline)

M7 added two snapshots from real distribution layouts:

| Environment | Runtime memory | Project/whole-system disk | Notes |
| --- | ---: | ---: | --- |
| Ubuntu 24.04 arm64 / systemd | Node RSS `11.9 MiB` | Project files about `74 MiB` | Fresh installation with real rw-core, geo, and ASN assets; Panel had not yet started the core |
| Alpine 3.22 arm64 / OpenRC container | Whole container `44.1 MiB` | Entire rootfs `150.2 MiB` | Container limited to `512 MiB / 1 CPU / 256 PIDs`, with real installation dependencies and service |

Project files consist of roughly `12 MiB` for Node, `34 MiB` for rw-core/support, and `28 MiB` for geo/ASN assets. The two rw-core streams use capped writers. Each current file and its `.1` file has a `4 MiB` rotation threshold, giving the two streams a steady-state threshold budget of `16 MiB`; two fixed `.1.tmp` files can add about `8 MiB` after a crash. Docker's `28 MiB` log tmpfs is sized around this boundary.

OpenRC additionally writes `openrc.log` and `openrc.err.log` through the supervisor. They are checked and copy-truncated every 10 seconds. After a successful check, each `.1` file uses a `4 MiB` threshold, but a current file can exceed the threshold within the polling window; this is not a mathematical hard limit. The OpenRC threshold budget for the four current-plus-`.1` pairs is therefore `32 MiB`, or about `48 MiB` if all four fixed temporary files remain, plus any growth of the two OpenRC current files within a polling interval. The systemd journal accepts at most 200 service log records every 30 seconds, but byte usage and long-term disk growth still follow the host's journald quota. A future extended-validation run should measure a log fault storm and long-term growth on a whole machine with `2 GB` of disk; that run is deferred and does not block `2.8.0`, and these thresholds do not substitute for its results.

Installation and upgrade store large assets in root-only `/var/lib/remnanode-installer`, not in `/tmp`, which may be memory-backed. All five mutating entry points hold the fixed `/run/lock/remnanode-installer.lock`. Nested installers reuse and verify the same open file description. The lock path is unaffected by `RNL_TMP_ROOT`, and no exit path removes the lock inode. Synchronous child processes that mutate packages, files, or services inherit the lock so that, if the parent installer exits unexpectedly, serialization lasts until the mutation finishes. Downloads, archive inspection, Node/rw-core self-checks, status queries, and the OpenRC start chain that may spawn a resident supervisor close their own lock file descriptor first, preventing short-lived tools or a supervisor from retaining the lock after the installer finishes.

Release archives are limited to `64 MiB` compressed, `128 MiB` extracted, and `64` entries. The rw-core zip, custom core, geo, and ASN paths each have hard download and streaming-extraction limits. Local `GEO_ZAPRET_FILE` and `IP_ZAPRET_FILE` inputs are each limited to `64 MiB` and use atomic staging in the destination directory. A download may run for at most `300s` and also has connection and low-speed timeouts; tar and unzip operations may run for at most `120s`.

Upgrade first reserves space for the existing backup plus `512 MiB`. After the rw-core download passes zip-structure validation, it calculates per-filesystem requirements for the installer, core, geo, and ASN target filesystems: actual archive entries, optional custom core/ASN data, backups, target staging, and a `64 MiB` safety margin for each filesystem. When upgrade invokes the rw-core installer, the outer transaction is the sole backup owner and does not make a duplicate copy of the same assets. A standalone installer that cannot complete rollback retains its root-only transaction directory and returns failure rather than deleting the only backup.

Production `node.env` must be a regular, non-symlink file. Go reads at most `1 MiB` before setting the memory soft limit and accepts no more than `4096` lines and `256` assignments. A single line may also be up to `1 MiB`, allowing migration of legacy inline Secrets up to `256 KiB`. Both `node.env` and `SECRET_KEY_FILE` are opened once with `O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`; the same descriptor is consumed only after `fstat -> bounded read -> fstat`, avoiding check/open races and FIFO blocking. systemd and OpenRC launch with fixed `REMNANODE_ENV=/etc/remnanode/node.env` and `/usr/bin/env -i`, retaining only `PATH/HOME/USER/LOGNAME`. The same Go configuration parser validates and applies `GOMEMLIMIT` and contract/core version overrides. Secrets and unknown configuration values do not enter the Node or rw-core environment.

## Protection Policies

- Low-memory mode defaults the request-body limit to `16 MiB`. Explicit `BODY_LIMIT_MB` must be `1..1024`; `0` or empty selects the automatic default.
- The decoder has an absolute `64 MiB` compressed-input ceiling and a `32 MiB` window ceiling. Public routes first apply their smaller route-specific limit, so current effective input and window sizes are no more than `16 MiB`. At most two single-threaded decoders run concurrently.
- A gRPC response is limited to `16 MiB`, and internal RPCs have deadlines.
- The internal Unix service accepts at most `8 KiB` per request, with at most `8` connections and `4` active handlers.
- Decoded webhooks enter a bounded queue of `64` items served by one worker. A full queue waits only within the internal request's `30s` deadline. If capacity does not recover, the request is canceled, or the service is closing, the server returns `503 + Retry-After` rather than reporting an event that was never admitted as successful.
- The torrent-report ring retains at most the newest `1024` entries.
- Once Xray is ready, the decoded configuration tree and canonical JSON are released; only hashes and runtime state remain.
- Debian and Alpine installers automatically set `LOW_MEMORY=1` when `MemTotal <= 512 MiB`.
- OpenRC verifies cgroup v2 limits of `448 MiB` memory, zero swap, 1 CPU, and 256 PIDs, plus the startup shell's actual cgroup membership. It refuses to start if a controller is unavailable or a write does not take effect. Shutdown does not depend on OpenRC 0.62.6 removing the path: `stop_post` first moves itself out, kills the exact service cgroup through `cgroup.kill`, waits up to 5 seconds for `populated=0`, and then removes the directory.

The OpenRC cleanup above covers the normal stop path where init actually runs `stop_post`. The shared installer lock removes concurrent writes, but it is not a persistent phase journal for `SIGKILL` or power loss. Nor does the project promise automatic cleanup of a residual cgroup if `supervise-daemon` itself exits abnormally. These are accepted operational limitations for `2.8.0`: rerun the installer or reboot for a native deployment, and recreate the container for a container deployment. They are not release blockers.

Any change to request decoding, the Xray configuration lifecycle, RPC messages, report queues, or the dependency graph should rerun this engineering gate and compare stage peaks. That comparison is a maintenance guardrail independent of the current M8 blocking profile.

## Shutdown Budget

| Layer | Limit | Semantics |
| --- | ---: | --- |
| Entire Node | `25s` | All application cleanup shares one deadline; this is not 25 seconds per component |
| rw-core | `5s + 5s` | Send SIGINT to the dedicated process group, then SIGKILL if needed; remove plugin nft tables only after whole-group cleanup succeeds |
| Plugin Close | `min(remaining budget, 15s)` | Gate wait, nft commands, and worker join share the remaining time |
| Unix server | `5s` | Shut down after root-context cancellation; force-close on failure |
| HTTPS server | Remaining overall budget | Force-close after the deadline |
| systemd | `30s` | `TimeoutStopSec`, leaving about 5 seconds outside the application's 25-second budget |
| OpenRC | `TERM/30/KILL/5` | Outer `supervise-daemon` fallback |

When the overall deadline expires, shutdown returns an aggregate error; the outer service manager may then force-kill the process. This does not prove that every fault path shuts down gracefully within 25 seconds.

If core or plugin cleanup returns a transient error quickly, the application waits `100ms` and retries once within the same deadline. The retry does not create another 25-second budget. Public `xray/stop` likewise confirms that core has stopped before removing plugin rules, avoiding an unfiltered window while core remains online. `plugin sync/recreate` and `xray start/stop` share the application lifecycle gate. The lock order is fixed as `lifecycle gate -> plugin operation gate -> Manager`, preventing an inconsistent plugin snapshot from being committed while core configuration is starting.
