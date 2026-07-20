# July 2026 Static Audit Remediation Record (Archived)

[Back to developer documentation](../development/README.md) · [Current roadmap](../development/roadmap.md) · [Architecture](../architecture.md)

This document preserves the static-audit scope and remediation principles applied to the first independent release line in July 2026. It is a decision record, not a current backlog or an onboarding entry point. The original audit predates the repository history reset, so its local baseline commit is not part of the current independent Git history. Facts that must remain traceable now live in code and tests, the [2.8.0 contract baseline](../development/contract-2.8.0.md), the [resource budget](../development/resource-budget.md), and the [release acceptance protocol](../development/release-acceptance.md).

The M0-M7 static remediation work is complete. M8 still owns real Panel/Linux, resource, fault-recovery, and long-running evidence for a frozen candidate. Automatic recovery from power loss or a forcibly terminated installer or supervisor remains an accepted operational limitation. Recovery consists of rerunning the installer, rebooting the host, or recreating the container as appropriate.

## Engineering Principles

- The official implementation defines the external compatibility target, not this project's internal architecture. Its fail-open behavior, swallowed errors, unbounded resources, or non-retryable operations must not be copied.
- Every lifecycle, process, rule set, and state domain must have one owner. Manager and Plugin each own their internal state, while the HTTP lifecycle coordinator orders cross-component entry points. Callers must not maintain scattered, competing locking conventions.
- Every successfully spawned process must have its ownership registered before control returns. The system may report `stopped` or remove firewall rules only after confirming that the leader and descendants have been cleaned up.
- Every mutation must bind to an explicit rw-core process identity. The RPC effect and local hash/state commit must occur under the same process lease.
- Firewall updates follow fail-closed plan/apply/reconcile/commit semantics. Failures must be visible, reversible where promised, and safe to retry idempotently.
- All data received from HTTP, Panel, plugin configuration, rw-core webhooks, files, and external commands must have byte, item-count, depth, concurrency, time, and output bounds. Errors and logs are also part of the resource budget.
- `512 MiB RAM / 1 vCPU / 2 GiB disk` is a whole-machine target. A Go runtime soft memory limit does not replace a whole-system budget for rw-core, cgroups, kernel nftables state, page cache, installer temporary files, and logs.
- The compatibility oracle must be generated independently of the Go implementation. Copying implementation constants or comparing a handwritten schema with itself is not evidence.

## Completed Remediation Stages

1. **Input and deployment safety:** removed root shell sourcing from OpenRC; validated Secrets; bounded request arrays, validation issues, JSON depth, plugin expansion, and log fields.
2. **Xray lifecycle:** implemented atomic spawn ownership registration, descendant-cleanup success conditions, and operation/process epoch leases. The HTTP coordinator allows shared starts to enter Manager while giving stop and plugin mutations exclusive, writer-preferred access.
3. **Plugin and connection correctness:** fixed webhook overload feedback, idempotent nft deletion, preservation of dynamic blocks, core-first cleanup, and retryable socket dropping. Disabling torrent blocking without tags hot-removes the outbound rule; healthy `recreate-tables` rebuilds only nftables, and core stops only when recovery from degraded mode makes torrent blocking effective again.
4. **Official behavior alignment:** fixed confirmed differences in stats reset, protobuf wire encoding, HTTP parsing, response models, system information, JWT handling, and the Unix configuration target.
5. **Operational resource boundaries:** completed OpenRC cgroup enforcement, low-memory upgrade migration, download/extraction/disk preflight checks, reliable shutdown, rollback, and process-identity health checks.
6. **Trusted test chain:** derived the static contract from pinned official source and SDK evidence. Full scenario runners and per-case evidence are bound to exit codes, log digests, and binary digests during the later release stage.
7. **Code freeze and acceptance preparation:** complete test, race, vet, static-analysis, and dual-architecture build checks as one batch before freezing the commit; then run systemd, OpenRC, Panel 2.8.1, real rw-core, 50k-user, and at least 24-hour whole-machine soak acceptance.

## Commit Strategy Used at the Time

- Commit related implementation, tests, and documentation in regression-testable stage-sized batches instead of fragmenting one issue into many micro-commits.
- During an architecture migration, commit the owner and interfaces before their callers; do not retain two sources of truth for an extended period.
- Run the complete gate once after the current batch is finished; repeat it only after diagnosing and fixing a real failure.
- Add official-contract issues and confirmed P1/P2 code defects to the current stage immediately. Track deferred release and operational improvements separately.
