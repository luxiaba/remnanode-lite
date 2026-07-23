# Remnanode Lite Roadmap

[Back to development documentation](README.md) · [Project overview](../project.md) · [Versioning model](../versioning.md)

## Project goals

This repository maintains an independent Go implementation with its own release history. The official `remnawave/node` project is a behavioral and contract reference, not a Git upstream. The [project overview](../project.md) defines the long-term goals, audience, and non-goals; this page tracks milestones and future work.

The first release line starts at `2.8.0` with these goals:

- Behavioral compatibility with official Node `2.8.0@596f015`.
- Real integration validation against Panel `2.8.1`.
- Resolution of known lifecycle, plugin, firewall, contract, and installation supply-chain defects.
- Stable operation on a Linux host with `512 MiB RAM / 1 vCPU / 2 GB disk` as an engineering target.
- Linux `amd64` and `arm64` artifacts, with real Panel and traffic verification before release.
- Keep Rocky Linux 9/systemd as the primary Native target, with Rocky Linux 8
  and Debian 12 as compatible targets. OpenRC remains an explicitly
  experimental cgroup v2 path.

The project version and official contract version move independently. `X.Y.Z-rnl.N` identifies a project-specific iteration, whether it develops the next version line early or improves an existing official baseline. A plain `X.Y.Z` release is allowed only after alignment with that official contract is complete. Monitoring a new official release creates an issue; it never changes the contract or publishes anything automatically. See the [versioning model](../versioning.md).

## Design principles

1. The official contract and observable behavior define compatibility; the official TypeScript architecture is not an internal template.
2. Validate every request completely before producing side effects.
3. Perform external side effects through replaceable interfaces and propagate their errors.
4. Commit state only after external operations succeed; failures must permit a safe retry of the same request.
5. Every concurrency limit, queue, request body, and cache must have an explicit bound.
6. The Node owns only its rw-core process, internal sockets, and private nftables table; it does not own the host firewall policy. Destroying sockets by IP can affect the host network namespace and is treated as an explicit, documented side effect.
7. `dev` is the stable development and integration branch. Topic branches enter it through PR and CI. `main` is the release branch and accepts only candidates that have passed the code gate on `dev`.
8. Every `main` commit gets one immutable `sha-<40-character-commit>` container candidate and an attested `release-index.json` binding. After maintainer acceptance, the release workflow verifies a draft Release, promotes the recorded exact candidate before publication, publishes and locks it at the current `main` HEAD, then reconfirms that exact tag without rebuilding.

## Compatibility boundary

- `/node` routes follow official Node 2.8.0 HTTP methods, request and response shapes, and error semantics.
- Project-specific diagnostics and operations live in the CLI or a separate internal interface; they do not extend the official `/node` contract.
- After a Node restart, the process waits for Panel to resend configuration instead of restoring a potentially stale full proxy configuration from disk.
- Request-size and resource protections may create documented safety deviations, but they must fail explicitly rather than degrade silently.
- The nftables plugin owns a separate table and can coexist with firewalld. Opening service ports remains the administrator's responsibility.

## Current status

| Milestone | Status |
| --- | --- |
| M0 Independent project baseline | Complete |
| M1 Contract evidence | Complete |
| M2 API boundary | Complete |
| M3 Xray lifecycle | Complete |
| M4 Plugins and nftables | Complete |
| M5 Users, connections, and statistics | Complete |
| M6 512 MiB resource work | Complete |
| M7 System integration and supply chain | Complete |
| M8 Release preparation | Complete |
| M9 Self-contained Native distribution | Ready for publication |

The M6 50,000-user measurement from 2026-07-15 and the M7
init/distribution snapshots from 2026-07-19 remain useful engineering
baselines. They document the resource work and give later changes a stable
comparison point; they are not claims about every future build.

The clean stable `2.8.0` release is the official-contract baseline and includes
the first self-contained Native bundle. Its Release publishes exact Docker and
Native assets and advances the GHCR `latest` channel. Runtime observations stay
outside the source repository, and GitHub generates the Release notes.

## Current focus

- **Now:** Publish the clean stable `2.8.0` release after its exact candidate
  image and Native bundles have passed maintainer acceptance and the release workflow.
- **Next:** Evaluate the next official release detected by automation. Pin its
  source and review the contract diff before selecting a project version line.
- **Later:** Improve observability, upgrade automation, and distribution coverage without compromising the 512 MiB target.

The following are accepted limitations or later enhancements and do not block
the `2.8.0` stable release:

- More whole-host 512 MiB, arm64 runtime, native-install, large-user, soak, and fault-injection coverage can be added when it answers a concrete risk.
- The Native journal cannot recover a host power loss that leaves an abnormal
  OpenRC cgroup populated. Stop the residual process or reboot that host, then
  run `rnlctl repair`; recreate a container when its runtime state is not
  recoverable.
- OpenRC `stop_post` cleans the dedicated cgroup during a normal stop. Recover from an abnormal `supervise-daemon` failure by rebooting or redeploying.
- Revisit the memory tradeoff of a resident active-config copy and runtime `dump-config` only with measured need.
- P3 test additions remain for top-level `runNode` failure convergence and cancellation of active Unix-server handlers.
- After the first real production soak, split process supervision, runtime state, or version tracking from `xray.Manager` only when actual change pressure justifies it. Keep the Manager facade and current concurrency invariants.
- The rw-core gRPC adapter now has the explicit package path `internal/xrayrpc`. Introduce neutral application types only when they create real decoupling value.

The historical remediation record is archived at [`docs/archive/2026-07-audit-remediation.md`](../archive/2026-07-audit-remediation.md).

## Milestones

### M0 - Independent project baseline

- Normalize the Go module, repository identity, version, and release ownership.
- Pin official Node and Panel compatibility targets.
- Establish the roadmap, release gate, and branch/release policy.

### M1 - Contract evidence

- Fix the 26 routes and HTTP methods.
- Convert the official Zod request and response constraints into executable evidence.
- Cover valid payloads, missing fields, wrong types, unknown variants, extra JSON, and error responses.
- Provide a black-box differential probe for official Node and the Go implementation.
- See [`contract-2.8.0.md`](contract-2.8.0.md) for contract details and known deviations.

### M2 - API boundary

- Introduce strict JSON decoding, DTO validation, and consistent error encoding.
- Separate HTTP transport from application services.
- Ensure malformed requests cannot call Xray, nftables, or `ss`, or mutate in-memory state.

### M3 - Xray lifecycle

- Express startup, shutdown, health, and process exit as an explicit state machine.
- Remove `last-start.json` and offline restoration of stale configuration.
- Correct concurrent starts, timeouts, cancellation, child reaping, and graceful shutdown.
- Preserve official Panel-disable and Node-restart behavior.

### M4 - Plugins and nftables

- Apply synchronization as `plan -> apply -> commit`.
- Unify nftables initialization, availability, error propagation, cleanup, and idempotent retry.
- Correct ingress unblock, shutdown residue, missing ASN data, and torrent-state drift.
- Exercise nftables in Linux network-namespace integration tests.

### M5 - Users, connections, and statistics

- Correct validation and partial-failure semantics for hot user updates.
- Report actual connection-drop results and protect special addresses.
- Replace unbounded goroutines and N+1 amplification with fixed workers or batch RPCs.
- Add bounded deadlines and cancellation propagation to every gRPC call.

### M6 - 512 MiB resource work

- Reduce Xray configuration to one canonical JSON representation instead of retaining map, clone, JSON, and persisted copies.
- Bound zstd decoder memory, report queues, temporary slices, and request peaks.
- Use the minimal `internal/xrayrpc` protobuf client rather than importing the complete Xray Go implementation.
- Record idle, startup, synchronization, and large-user-set peaks under cgroup limits.
- The real rw-core peak with 50,000 users was `143.9 MiB`. See [`resource-budget.md`](resource-budget.md) for the complete budget and reproduction method.

### M7 - System integration and supply chain

- Run under a dedicated user with minimal capabilities and systemd sandboxing.
- Align directory permissions and lifecycle behavior between Debian/systemd and Alpine/OpenRC.
- Pin every Release, rw-core, ASN, and helper-script asset and verify its digest.
- Ensure installation, upgrade, rollback, and uninstall do not affect processes or nftables tables outside this project.
- Ubuntu 24.04/systemd and Alpine 3.22/OpenRC snapshots remain historical
  engineering baselines for predecessor installer work. The supported Native
  lifecycle is now `rnlctl`; systemd is the maintained path and OpenRC remains
  experimental until the target host is checked.
- Both non-root service processes retain only effective and ambient `NET_ADMIN` and `NET_BIND_SERVICE`.
- Pinned rw-core, ASN, and release archives are verified before installation.
- Fault-injection tests cover post-write failures and per-file digest restoration for rw-core assets and Node upgrade transactions.

### M8 - Release preparation

- Pass Go tests, race tests, vet, static checks, script checks, and multi-platform builds.
- Publish one immutable `sha-<40-character-commit>` image for every `main` commit, with runnable `linux/amd64` and `linux/arm64` manifests and their attestations.
- Verify the selected candidate with a real Panel and real proxy traffic under the production container limits before dispatching a release. Keep host details, logs, and runtime records outside the repository.
- Require the release dispatch commit to be the current `main` HEAD. Verify the
  candidate manifest and source attestation, reuse and attest the prebuilt Native bundles,
  bind their Release asset set to the accepted OCI digest, then promote that
  digest to the exact version without rebuilding it.
  Plain stable tags advance `latest`; `rnl.N` tags advance `preview` only.
- Keep the existing lifecycle, process-group cleanup, installer, 50,000-user, and rollback results as code-level tests or dated engineering baselines.
- Update the compatibility documentation and dated root `CHANGELOG.md`; let GitHub generate the Release notes.

### M9 - Self-contained Native distribution

- Publish one verified bundle per Linux architecture with Node, `rnlctl`,
  rw-core, geo/ASN data, service material, manifest, SPDX SBOM, notices, and
  exact provenance.
- Replace distribution-specific shell mutation logic with the tested Go
  lifecycle engine and its durable generation journal.
- Support systemd on Rocky Linux 9 as the primary Native target, Rocky Linux 8
  and Debian 12 as compatible targets, and OpenRC as an explicitly experimental
  cgroup v2 path.
- Exercise exact install, prepare/activate, upgrade, rollback, repair,
  uninstall, tamper refusal, account isolation, and interrupted-operation
  recovery before publishing `2.8.0`.

## Development and release rules

- `main` is the protected release branch; `dev` is the stable development and integration branch.
- Daily changes enter `dev` first. Promote a release candidate from `dev` to `main` through a PR.
- Keep each commit explainable and verifiable; do not mix unrelated formatting.
- Run tests proportional to the change risk before merging. Failed checks do not enter `dev` or `main`.
- Wait for the `main` `sha-*` candidate and verify it with a real Panel and real traffic before dispatching the release workflow. Do not commit operational test data.
- Formal tags use `X.Y.Z` or `X.Y.Z-rnl.N` and exactly match project `Version`. Never overwrite an exact published tag.
- Do not configure an upstream code remote. External implementations are protocol and behavioral evidence only.
