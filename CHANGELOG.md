# Changelog

[Back to the documentation index](docs/README.md)

This file follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and focuses on changes that matter to users and operators. GitHub Releases provide the full diff for each published version.

## [2.8.0] - 2026-07-22

This stable release implements the official Node `2.8.0` contract and adds the
self-contained Native Linux distribution. It publishes exact `2.8.0` images
and bundles and advances the stable GHCR and GitHub `latest` channels.

### Added

- Added verified Native bundles for Linux `amd64` and `arm64`. Each bundle
  contains `remnanode-lite`, `rnlctl`, rw-core, GeoIP, GeoSite, ASN data,
  service material, exact source and license notices, a strict file manifest,
  and an SPDX SBOM.
- Added the POSIX `install.sh` bootstrap for an exact online Release, a local
  archive plus SHA-256, or an extracted bundle. Interactive and unattended
  Secret input, custom Node port, and prepare-only installation are supported.
- Added `rnlctl` as the Native lifecycle interface: install, activate, exact
  upgrade, rollback, repair, uninstall, start/stop/restart, structured status
  and doctor output, and service/core log access.
- Added durable Native transaction state, a verified repair cache, and atomic
  `current`/`previous` generation selection. Only the active and one rollback
  generation are retained.
- Added a systemd 239-compatible base unit for Rocky Linux 8/9 and Debian 12.
  A version-gated hardening drop-in is installed on systemd 247 or newer.
  OpenRC is available as an experimental cgroup v2 path.
- Added draft-first Release publication for Native assets, SHA-256 and GitHub
  attestations for every asset, exact image promotion without rebuilding, and
  a reconcile action for a failed post-publication channel update.

### Changed

- Native host paths now consistently use `/etc/remnanode-lite`,
  `/usr/local/lib/remnanode-lite`, `/var/lib/remnanode-lite`, and
  `/var/log/remnanode-lite`; the service name is `remnanode-lite` on systemd
  and OpenRC.
- Replaced the former distribution-specific shell installers and independent
  runtime-asset update paths with one complete release bundle and a Go
  lifecycle engine. Node, rw-core, geo data, ASN data, notices, and service
  definitions now upgrade and roll back as one generation.
- Native install and upgrade accept exact `X.Y.Z` or `X.Y.Z-rnl.N` versions
  only. `latest`, `preview`, `edge`, and `sha-*` are container references and
  are never resolved by Native lifecycle commands.
- Reworked the English deployment, configuration, operations, architecture,
  development, security, and contribution documentation around Docker as the
  default path and Native Linux as a first-class release format.

### Security

- Verify the outer Native archive digest, strict manifest schema, target
  architecture, project and contract versions, every payload path/mode/size/
  digest, and the embedded Node/rnlctl ELF architecture before installation.
- Snapshot caller-provided bundles into a root-only workspace before
  validation, preventing path replacement during installation.
- Keep `rnlctl` as an independent root-owned regular file so repair remains
  available while generation links are damaged or changing.
- Track user and group creation separately. Purge removes only identities the
  installer created and refuses deletion when an identity no longer matches
  the recorded account.

## Pre-release implementation history

This entry covers the first independent release line of
`luxiaba/remnanode-lite`. It implements the official Node 2.8.0 contract.
Panel 2.8.1 is used for integration testing but does not determine the project
version.

The release candidate passed GitHub CI, multi-architecture image and
attestation checks, and maintainer verification with a real Panel 2.8.1
connection and real proxy traffic under the maintained Compose limits.
Operational test data is intentionally not stored in the source repository.

### Added

- Split CI into focused Go, repository, offline-installer, and Linux
  network-administration jobs under the stable `ci / gate` check. Vulnerability
  scanning runs separately on a schedule, and GitHub-hosted runners use Ubuntu
  24.04.
- Added multi-architecture GHCR publishing with an SBOM, BuildKit provenance,
  and a GitHub build attestation. Every `main` commit publishes a `sha-*`
  candidate and updates `edge` while it remains the branch head;
  the release workflow promotes the accepted digest to the version tag and
  `latest` without rebuilding it. Relevant pull requests validate the
  container build without publishing an image.
- Added scheduled monitoring for official Node Releases. A changed compatibility baseline opens a synchronization Issue but never changes code or publishes an image automatically.
- Added amd64/arm64 multi-stage Docker images and production Compose files with pinned and verified rw-core, geo, and ASN assets. The deployment preserves the official host-network and capability model while applying 448 MiB memory, no additional container swap, 1 CPU, 256 PID, read-only-rootfs, health, and log limits.
- Standardized the Compose service, container, and hostname as
  `remnanode-lite`. Both maintained production templates now interpolate the
  same explicitly allowed runtime settings from `.env`, apply documented
  defaults, and reject an unset or empty `SECRET_KEY` during Compose expansion.
- Removed persistent container log volumes. rw-core logs use bounded tmpfs storage, Docker logs rotate within fixed limits, and recreating the container reclaims all runtime logs.
- Captured 26 routes, Zod request and response behavior, error shapes, and side effects from official Node `2.8.0@596f015` as an executable contract.
- Added a read-only-by-default `contract-probe`, protected by mTLS and JWT, for controlled black-box comparison between official Node and the Go implementation.
- Added a unified Node API boundary covering Zod-equivalent required fields, unions, UUID/IP formats, enums, nullable and default values, and array-length rules.
- Added Linux network-namespace integration gates for nftables and socket destruction, including real dual-stack replacement, block, unblock, recreation, shutdown cleanup, and TCP connection termination.
- Added a streaming ASN build pipeline pinned to an `ipverse/as-ip-blocks` commit and archive digest. Releases include the compact `asn-prefixes.bin` database and `SHA256SUMS`.
- Added a real rw-core resource gate at `448 MiB / 1 CPU / no swap`. The dated 2026-07-15 M6 engineering baseline peaked at `143.9 MiB` with 50,000 users. It remains a historical baseline; repeating the 50,000-user load is deferred rather than a `2.8.0` candidate blocker.
### Security

- Strip the Panel Secret, Secret-file path, Node configuration path, and caller-supplied internal token from the rw-core child environment. Only required resource paths and a controlled internal webhook token are reintroduced; that token is random by default for each start.
- Require JWT headers and claims to contain exactly one complete JSON value. A correctly signed token with a second trailing JSON value is rejected.
- Require TLS 1.3 or newer externally and disable HTTP/2. Invalid JWTs, unknown routes, and unsupported methods destroy the connection in line with the official behavior.
- Run native systemd and OpenRC services as the dedicated `remnanode-lite` user with only `CAP_NET_ADMIN` and `CAP_NET_BIND_SERVICE`. The systemd unit also applies capability bounding, sandboxing, 448 MiB/no-swap/1 CPU, and 256-task limits.
- Verify SHA-256, structure, and version before writing Release archives, rw-core, custom core, or ASN assets. The audited digest for the pinned rw-core version cannot be overridden, and GitHub Actions are pinned to complete commit SHAs.
- Start systemd and OpenRC from an empty environment. Go reads `node.env` and the Secret through the same bounded file descriptor with `O_NOFOLLOW|O_NONBLOCK`; symlinks, FIFOs, devices, oversized files, and files changed during reading fail before startup.
- Reject unsafe ownership, permissions, symlinks, and hard links in managed paths. Log helpers, rw-core, geo files, and ASN data use same-directory staging and atomic replacement; the outer upgrade transaction backs up and verifies service-file changes.
- Share one kernel lock across installation, upgrades, rw-core installation, and
  uninstall. Nested operations reuse the lock, and child processes inherit it
  only while they perform synchronous package, file, or service changes. Alpine
  installations explicitly depend on `util-linux`.

### Fixed

- Validate `NODE_PORT` while loading configuration and reject zero, negative values, and values above 65535, preventing the direct run path from binding an unintended random port.
- Validate formal tags, target architectures, and script allowlists in `release-url` and `install-script-url`, rejecting path-like and unknown inputs.
- Test the actual dispatcher registry for routes. `/node/xray/stop` now accepts only the official GET method and no longer accepts POST.
- Preserve JSON decoding and type errors in stats, handler, plugin, and Xray-start paths. Malformed, trailing, or incomplete input returns 400 before any provider, process, nftables, connection, or state side effect.
- Complete known application errors with the official `timestamp`, `path`, `message`, and `errorCode`; lower-level SDK errors no longer replace the official A001 and A010-A017 messages.
- Align contract edge cases for stripped unknown object fields, the `forceRestart=false` default, empty strings, arrays without a minimum length, five user unions, and numeric nftables timeouts.
- Model Xray start, stop, health, and natural exit with an explicit four-state lifecycle. Stop can cancel an in-progress start; failures and timeouts do not commit configuration or hashes, and every spawned child is reaped.
- Remove the non-official `last-start.json` persistence and boot-time restoration of stale configuration. After a Node restart, Panel health polling resends start configuration; the `/node/xray/healthcheck` endpoint reads cached state only.
- Make Panel stop confirm that rw-core has stopped before resetting plugins. A stop failure preserves the plugin snapshot and nftables rules so a running core never enters an unfiltered window.
- Place rw-core in a dedicated Linux process group so normal stop, timeout kill,
  and leader-exit cleanup cover the whole group. A parent-death signal protects
  the direct child; a forcibly killed Node or supervisor still requires service
  restart or redeployment.
- Make plugin synchronization an `apply -> Xray reconcile -> commit`
  transaction. nftables or Xray failures keep the old state and attempt to
  restore the previous firewall plan. Plugin changes and Xray lifecycle
  operations now share one gate.
- Consolidate nftables initialization, dual-stack batching, ingress/torrent unblock, recreation replay, error propagation, and shutdown table removal. Multiple kernel messages for an absent element are treated as idempotent success.
- Continue accepting valid plugin configuration with official semantics when nftables is unavailable, while keeping torrent effective state disabled. Reset no longer discards uncollected reports, and ASN/shared-list degradation is logged explicitly.
- Route listener failures through the common shutdown path instead of calling `log.Fatalf` from a goroutine. Shutdown stops rw-core before removing project-owned nftables tables.
- Serialize live-user mutations with cancellation support. Each request holds one process lease bound to a specific rw-core across RPCs, connection cleanup, and inbound-hash commit. A cleanup failure prevents adding that user, and partial batch failure remains visible and retryable.
- Normalize and deduplicate addresses before connection destruction, protecting invalid, special, local, and allowlisted addresses. Missing capability, failed address lookup, or any failed `NETLINK_SOCK_DIAG` destruction no longer reports false success.
- Prefer one batch RPC for `get-users-ip-list`. Older cores fall back only on
  `UNIMPLEMENTED`, use at most eight workers, and cache capability detection.
- Add cancellation propagation and bounded deadlines to every internal Handler and Stats unary gRPC call: five seconds by default, three seconds for health, and one shared budget for a legacy batch lookup.
- Bound the Xray webhook to a 64-item queue and one worker. Queue timeout,
  cancellation, and shutdown return 503. Once Plugin shutdown begins, admission
  never reopens, even if a timeout or nft cleanup fails; a failed `Close` can
  still be retried.
- Use one shared 25-second process shutdown budget. Background version detection is cancellable and joined, and nftables cleanup starts only after rw-core is confirmed stopped, avoiding accumulated inner timeouts beyond the service manager's TERM grace.
- Make user-mutation panics observable without changing the client contract. Clients still receive A001; bounded logs record the operation, panic type, value, and stack, while the process lease and mutation gate are always released.
- Serialize public `xray/stop` with start and stop operations, and reset plugins only after the core stops successfully. Failed stop no longer removes filtering early.
- Treat a repeated installation as a rollback-capable upgrade. Failures while
  replacing service files, binaries, support files, `node.env`, or rw-core
  restore the previous files, enablement, and running state. If restoration is
  incomplete, the backup is kept and the operation fails clearly.
- Calculate rw-core staging and backup peaks against the actual filesystems holding the installer, core, geo, and ASN destinations. Insufficient space on any mount fails before assets are replaced.
- Enter daemon mode only with zero CLI arguments; reject unknown commands and extra arguments. Unix socket startup rejects live sockets, symlinks, and non-socket paths, and shutdown removes only the socket instance it owns.
- Stop uninstalling by process name and stop deleting generic Xray paths. Uninstall removes only project-owned processes, sockets, nftables tables, and `/usr/local/{lib,share}/remnanode`.
- Let non-interactive installation without a Secret finish writing files while leaving the service stopped, instead of waiting for a port from a service that was never started.
- Keep every installer and upgrade wrapper's `--dry-run` path free of writes. Reject path-like Release tags before bootstrap or transaction start, and always source service/core support files from the verified target Release.
- Migrate legacy generic Xray, geo, and ASN paths only after the corresponding private assets install successfully. An upgrade that preserves the current core no longer redirects a working configuration to an empty path.

### Maintenance

- Rebuilt the documentation information architecture around project scope, runtime architecture, complete configuration, Docker and native deployment, operations, development, testing, versioning, Release management, contribution, and security. The root README is now a concise project entry point.
- Added an executable documentation gate for headings, fences, local files, anchors, and reachability. Added a pinned wire-regeneration entry point using `protoc 35.1` and `protoc-gen-go v1.36.11`.
- Decoupled project `Version` from official `ContractVersion`: `X.Y.Z-rnl.N` identifies an independent project iteration, while a plain `X.Y.Z` is reserved for a completed official-version alignment.
- Require build attestation before publishing a commit-specific candidate tag.
  A release promotes only the digest behind the tagged `main` commit's
  immutable `sha-*` image and refuses to replace an existing version tag with
  different content.
  The Release Git tag must point to the current `main` head.
- Set candidate OCI version metadata from the project version. The Git tag,
  candidate image, and generated GitHub Release all refer to the same `main`
  commit.
- Moved the Go module, installer URLs, Release addresses, and documentation ownership to this repository.
- Established the compatibility, architecture-remediation, and 512 MiB acceptance roadmap.
- Make contract CI read raw Git objects from the pinned official commit and
  verify all 58 registered blobs, including the lockfile. It checks Nest
  bootstrap and metadata, static imports, decorator ownership, global-prefix
  exclusions, and all 26 routes. Updating the official pin still requires human
  review.
- Bind the release gate to the current `main` candidate, CI,
  multi-architecture image shape, and exact build attestation. Maintainers
  verify the digest-pinned candidate with a real Panel and real traffic before
  tagging without committing runtime data.
- Separate HTTP transport from stats, user-handler, and plugin application services. The business layer no longer depends on `net/http` or decodes request JSON itself.
- Make `main` the single composition root. It explicitly constructs and injects the network monitor, system collector, version, request-body budget, and application services, removing import-time goroutines, process-global mutable body limits, and environment-variable rewrites.
- Pin and calibrate external schema evidence for `@remnawave/node-plugins@0.4.5`, including explicit null, AS numbers, `ext:`, and numeric limits.
- Replace the full Xray Go module with a minimal rw-core protobuf wire client, reducing both architecture binaries by about 30%.
- Complete the dated 2026-07-19 M7 engineering baseline on Ubuntu 24.04/systemd and Alpine 3.22/OpenRC for fresh install, upgrade rollback, start/stop, dedicated user and capabilities, logs, disk, and uninstall isolation. These remain historical engineering results; `native-systemd-install` and `native-openrc-install` are deferred from the `2.8.0` candidate gate.
