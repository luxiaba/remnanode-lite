# Changelog

[Back to the documentation index](docs/README.md)

This file follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). It records notable user-facing and operational changes; GitHub Releases provide the complete diff for published versions.

## [2.8.0] - Unreleased

This is the first independent release line of `luxiaba/remnanode-lite`. Its implemented contract is pinned to official Node 2.8.0; Panel 2.8.1 is the integration acceptance environment and does not determine the project version.

The first entry also records the repository takeover and architectural remediation, so it is intentionally more detailed than future entries. Later entries should contain only changes that users, operators, or maintainers need to understand.

### Added

- Split development checks into independently diagnosable Go, repository, offline installer, and Linux network-administration jobs, summarized by the stable `ci / gate` check. Vulnerability scanning is a separate scheduled workflow, and every GitHub-hosted runner is pinned to Ubuntu 24.04.
- Added a multi-architecture GHCR delivery chain. `main` first builds an untagged manifest with an SBOM, BuildKit provenance, and a GitHub build attestation, then publishes the immutable-by-policy `sha-*` candidate and moving `edge` tag. Changes to the fleet Compose template also trigger candidate builds. A tag Release validates the accepted digest and its source attestation without depending on a particular automatic or manual candidate alias, then promotes that exact image to the version tag and `latest` without rebuilding it. `dev` and pull requests validate the container build separately.
- Added scheduled monitoring for official Node Releases. A changed compatibility baseline opens a synchronization Issue but never changes code or publishes an image automatically.
- Added amd64/arm64 multi-stage Docker images and production Compose files with pinned and verified rw-core, geo, and ASN assets. The deployment preserves the official host-network and capability model while applying 448 MiB memory, no-swap, 1 CPU, 256 PID, read-only-rootfs, health, and log limits.
- Removed persistent container log volumes. rw-core logs use bounded tmpfs storage, Docker logs rotate within fixed limits, and recreating the container reclaims all runtime logs.
- Captured 26 routes, Zod request and response behavior, error shapes, and side effects from official Node `2.8.0@596f015` as an executable contract.
- Added a read-only-by-default `contract-probe`, protected by mTLS and JWT, for controlled black-box comparison between official Node and the Go implementation.
- Added a unified Node API boundary covering Zod-equivalent required fields, unions, UUID/IP formats, enums, nullable and default values, and array-length rules.
- Added Linux network-namespace integration gates for nftables and socket destruction, including real dual-stack replacement, block, unblock, recreation, shutdown cleanup, and TCP connection termination.
- Added a streaming ASN build pipeline pinned to an `ipverse/as-ip-blocks` commit and archive digest. Releases include the compact `asn-prefixes.bin` database and `SHA256SUMS`.
- Added a real rw-core resource gate at `448 MiB / 1 CPU / no swap`. The M6 engineering baseline peaked at `143.9 MiB` with 50,000 users; the frozen M8 candidate must still repeat this validation.
- Extended M8 evidence with real Compose runs bound to the candidate manifest digest and the deployment template stored in the candidate Git object. Native amd64 and arm64 runs must each pass whole-host resource, cgroup, init/reaping, capability, tmpfs, health, graceful-stop, zombie, log-rotation, disk-headroom, and rollback-image startup checks.

### Security

- Strip the Panel Secret, Secret-file path, Node configuration path, and caller-supplied internal token from the rw-core child environment. Only required resource paths and a controlled internal webhook token are reintroduced; that token is random by default for each start.
- Require JWT headers and claims to contain exactly one complete JSON value. A correctly signed token with a second trailing JSON value is rejected.
- Require TLS 1.3 or newer externally and disable HTTP/2. Invalid JWTs, unknown routes, and unsupported methods destroy the connection in line with the official behavior.
- Run native systemd and OpenRC services as the dedicated `remnanode` user with only `CAP_NET_ADMIN` and `CAP_NET_BIND_SERVICE`. The systemd unit also applies capability bounding, sandboxing, 448 MiB/no-swap/1 CPU, and 256-task limits.
- Verify SHA-256, structure, and version before writing Release archives, rw-core, custom core, or ASN assets. The audited digest for the pinned rw-core version cannot be overridden, and GitHub Actions are pinned to complete commit SHAs.
- Start systemd and OpenRC from an empty environment. Go reads `node.env` and the Secret through the same bounded file descriptor with `O_NOFOLLOW|O_NONBLOCK`; symlinks, FIFOs, devices, oversized files, and files changed during reading fail before startup.
- Reject unsafe ownership, permissions, symlinks, and hard links in managed paths. Log helpers, rw-core, geo files, and ASN data use same-directory staging and atomic replacement; the outer upgrade transaction backs up and verifies service-file changes.
- Share one fixed kernel lock across installation, upgrades, rw-core installation, and uninstall. Nested entry points reuse the same lock descriptor. Synchronous package, file, and service mutations hold the lock until child completion, while downloads, Node/rw-core self-checks, status commands, and OpenRC start chains that may create resident processes do not inherit it. Alpine installation explicitly depends on `util-linux`.

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
- Place rw-core in a dedicated Linux process group. Normal SIGINT stop, timeout SIGKILL, and leader-exit cleanup cover the whole group. A parent-death signal protects the direct child; after the Node or supervisor itself is forcibly killed, recovery is by service restart or redeployment.
- Rework plugin synchronization as an immutable `apply -> Xray reconcile -> commit` transaction. nftables or Xray failures do not publish new state and attempt to restore the previous firewall plan. `plugin sync/recreate` and `xray start/stop` share the application lifecycle gate, removing races between core configuration and plugin snapshots.
- Consolidate nftables initialization, dual-stack batching, ingress/torrent unblock, recreation replay, error propagation, and shutdown table removal. Multiple kernel messages for an absent element are treated as idempotent success.
- Continue accepting valid plugin configuration with official semantics when nftables is unavailable, while keeping torrent effective state disabled. Reset no longer discards uncollected reports, and ASN/shared-list degradation is logged explicitly.
- Route listener failures through the common shutdown path instead of calling `log.Fatalf` from a goroutine. Shutdown stops rw-core before removing project-owned nftables tables.
- Serialize live-user mutations with cancellation support. Each request holds one process lease bound to a specific rw-core across RPCs, connection cleanup, and inbound-hash commit. A cleanup failure prevents adding that user, and partial batch failure remains visible and retryable.
- Normalize and deduplicate addresses before connection destruction, protecting invalid, special, local, and allowlisted addresses. Missing capability, failed address lookup, or any failed `NETLINK_SOCK_DIAG` destruction no longer reports false success.
- Prefer one batch RPC for `get-users-ip-list`. Older cores fall back only on `UNIMPLEMENTED`, use at most eight fixed workers, and cache capability detection, eliminating unbounded N+1 goroutines.
- Add cancellation propagation and bounded deadlines to every internal Handler and Stats unary gRPC call: five seconds by default, three seconds for health, and one shared budget for a legacy batch lookup.
- Bound the Xray webhook to a 64-item waiting queue and one worker. Capacity timeout, cancellation, and shutdown return 503 explicitly. Plugin shutdown uses an irreversible admission fence, rejects new mutations after timeout or nft cleanup failure, and allows `Close` to be retried.
- Use one shared 25-second process shutdown budget. Background version detection is cancellable and joined, and nftables cleanup starts only after rw-core is confirmed stopped, avoiding accumulated inner timeouts beyond the service manager's TERM grace.
- Make user-mutation panics observable without changing the client contract. Clients still receive A001; bounded logs record the operation, panic type, value, and stack, while the process lease and mutation gate are always released.
- Serialize public `xray/stop` with start and stop operations, and reset plugins only after the core stops successfully. Failed stop no longer removes filtering early.
- Make a repeated installation enter the same rollback-capable upgrade transaction. Injected failures in systemd/OpenRC service files, binaries, support files, `node.env`, or rw-core restore prior files, enablement, and running state; incomplete restoration preserves the sole backup and fails explicitly.
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
- Require candidate images to be attested before an immutable commit alias is published. The acceptance manifest binds the tested digest; a Release can promote only that digest to the exact version and `latest`, and refuses to replace different content. The Release Git tag must identify the current `main` head.
- Set candidate OCI version metadata from the project version rather than a commit alias. Release records are squash-merged as exactly one documentation-only commit; the Release note records the candidate and digest, while the Git tag resolves the final commit without impossible self-reference.
- Moved the Go module, installer URLs, Release addresses, and documentation ownership to this repository.
- Established the compatibility, architecture-remediation, and 512 MiB acceptance roadmap.
- Make contract CI read raw Git objects from the pinned official commit, verify SHA-256 for 58 registered blobs including the lockfile, and fail closed around Nest bootstrap, static imports, module/controller metadata, decorator ownership, global-prefix exclusions, and all 26 routes. Updating the official pin still requires human review of extraction results and newly unsupported syntax.
- Bind the Release gate to a frozen candidate commit, strict JSON evidence, compatibility/resource/fault results, and a two-stage process that permits only release-document changes after acceptance.
- Separate HTTP transport from stats, user-handler, and plugin application services. The business layer no longer depends on `net/http` or decodes request JSON itself.
- Make `main` the single composition root. It explicitly constructs and injects the network monitor, system collector, version, request-body budget, and application services, removing import-time goroutines, process-global mutable body limits, and environment-variable rewrites.
- Pin and calibrate external schema evidence for `@remnawave/node-plugins@0.4.5`, including explicit null, AS numbers, `ext:`, and numeric limits.
- Replace the full Xray Go module with a minimal rw-core protobuf wire client, reducing both architecture binaries by about 30%.
- Complete the M7 engineering baseline on Ubuntu 24.04/systemd and Alpine 3.22/OpenRC for fresh install, upgrade rollback, start/stop, dedicated user and capabilities, logs, disk, and uninstall isolation. This does not replace M8 acceptance on the frozen candidate.
