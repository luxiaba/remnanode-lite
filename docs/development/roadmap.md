# Remnanode Lite Roadmap

[Back to development documentation](README.md) · [Project overview](../project.md) · [Versioning model](../versioning.md)

## Project goals

This repository maintains an independent Go implementation with its own release history. The official `remnawave/node` project is a behavioral and contract reference, not a Git upstream. The [project overview](../project.md) defines the long-term goals, audience, and non-goals; this page tracks milestones and future work.

The first release line starts at `2.8.0` with these goals:

- Behavioral compatibility with official Node `2.8.0@596f015`.
- Real integration validation against Panel `2.8.1`.
- Resolution of known lifecycle, plugin, firewall, contract, and installation supply-chain defects.
- Stable operation on a Linux host with `512 MiB RAM / 1 vCPU / 2 GB disk`.
- Linux `amd64` and `arm64` artifacts, with `2.8.0` runtime acceptance scoped to the production `amd64` Docker profile.
- Keep Debian/systemd and Alpine/OpenRC installation paths available, with native runtime acceptance deferred beyond the first release.

The project version and official contract version move independently. `X.Y.Z-rnl.N` identifies a project-specific iteration, whether it develops the next version line early or improves an existing official baseline. A plain `X.Y.Z` release is allowed only after alignment with that official contract is complete. Monitoring a new official release creates an issue; it never changes the contract or publishes anything automatically. See the [versioning model](../versioning.md).

## Design principles

1. The official contract and observable behavior define compatibility; the official TypeScript architecture is not an internal template.
2. Validate every request completely before producing side effects.
3. Perform external side effects through replaceable interfaces and propagate their errors.
4. Commit state only after external operations succeed; failures must permit a safe retry of the same request.
5. Every concurrency limit, queue, request body, and cache must have an explicit bound.
6. The Node owns only its rw-core process, internal sockets, and private nftables table; it does not own the host firewall policy. Destroying sockets by IP can affect the host network namespace and is treated as an explicit, documented side effect.
7. `dev` is the stable development and integration branch. Topic branches enter it through PR and CI. `main` is the release branch and accepts only candidates that have passed the code gate on `dev`.
8. A candidate becomes the frozen M8 candidate `C` only after it reaches `main`. No feature work is mixed into `C` after that point, and all real acceptance results bind to its commit.

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
| M8 Release acceptance | In progress |

The M6 50,000-user measurement from 2026-07-15 and the M7 init/distribution snapshots from 2026-07-19 remain useful engineering baselines. They predate candidate `C`, so they are not runtime evidence for the current M8 candidate and do not need to be repeated for `2.8.0`.

`2.8.0` is still unreleased, with M8 in progress. The implementation, CI, candidate-image pipeline, and code-level 512 MiB controls are in place. One acceptance profile still blocks release: `docker-production-smoke-v1` on the frozen candidate. The run must resolve the candidate tag to an immutable manifest digest, use that digest with the production Compose template on `amd64`, carry a real Panel connection and proxy traffic, and finish with a healthy container, zero OOM kills, and zero restarts. A `sha-*` tag from `main` only locates a candidate; it is neither the acceptance identity nor a formal release.

## Current focus

- **Now:** Complete the frozen candidate's `amd64` Docker production smoke and record the Panel, traffic, process state, memory, OOM, and restart observations required to finish M8.
- **Next:** Evaluate the next official release detected by automation. Pin its source and review the contract diff before selecting a project version line.
- **Later:** Improve observability, upgrade automation, and distribution coverage without compromising the 512 MiB target.

The following are accepted limitations or later enhancements and do not block `2.8.0`:

- `arm64-production-runtime`, `native-systemd-install`, and `native-openrc-install` are deferred.
- Candidate-scale 50,000-user load, a 24-hour soak, and fault/rollback injection are deferred extended-validation profiles.
- The installer has no persistent phase journal. Rerun it after `SIGKILL` or power loss; recreate the container for a container deployment.
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
- Establish the roadmap, acceptance gate, and branch/release policy.

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
- Ubuntu 24.04/systemd and Alpine 3.22/OpenRC have passed real fresh-install, repeat-install, upgrade, invalid-service rollback, start/stop, and isolated-uninstall exercises.
- Both non-root service processes retain only effective and ambient `NET_ADMIN` and `NET_BIND_SERVICE`.
- Pinned rw-core, ASN, and release archives are verified before installation.
- Fault-injection tests cover post-write failures and per-file digest restoration for rw-core assets and Node upgrade transactions.

### M8 - Release acceptance

- Pass Go tests, race tests, vet, static checks, script checks, and multi-platform builds.
- Freeze the code candidate first and bind the blocking acceptance record to its commit and candidate image.
- Complete the sole blocking runtime profile, `docker-production-smoke-v1`: an `amd64` production Compose deployment pinned to the candidate manifest digest, with a real Panel connection and real proxy traffic, observed memory and PID usage, a running and healthy container, zero OOM kills, and zero restarts.
- Keep the existing lifecycle coordinator, process-group cleanup, init, 50,000-user, and rollback tests as code-level or dated engineering evidence; do not present them as current-candidate runtime observations.
- Defer `arm64-production-runtime`, `native-systemd-install`, `native-openrc-install`, `50000-user-load`, `24h-soak`, and `fault-and-rollback-injection` as non-blocking follow-up validation.
- Update the compatibility matrix, risk register, operations documentation, root `CHANGELOG.md`, and `2.8.0` release material.
- Validate the release record and candidate identity according to [`release-acceptance.md`](release-acceptance.md), then permit only finalization changes.

## Development and release rules

- `main` is the protected release branch; `dev` is the stable development and integration branch.
- Daily changes enter `dev` first. Promote a release candidate from `dev` to `main` through a PR.
- Keep each commit explainable and verifiable; do not mix unrelated formatting.
- Run tests proportional to the change risk before merging. Failed checks do not enter `dev` or `main`.
- Formal tags use `vX.Y.Z` or `vX.Y.Z-rnl.N` and exactly match project `Version`. Never overwrite an exact published tag.
- Do not configure an upstream code remote. External implementations are protocol and behavioral evidence only.
