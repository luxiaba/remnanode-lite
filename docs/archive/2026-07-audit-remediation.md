# July 2026 Static Audit Remediation Record (Archived)

[Back to developer documentation](../development/README.md) · [Current roadmap](../development/roadmap.md) · [Architecture](../architecture.md)

This is the record of the static audit and remediation work completed for the
first independent release line in July 2026. It is historical context, not a
backlog or an onboarding guide. The audit predates the repository history
reset, so its original baseline commit is no longer present. Current facts live
in the code and tests, the [2.8.0 contract baseline](../development/contract-2.8.0.md),
the [resource budget](../development/resource-budget.md), and the current
[release process](../release.md).

The M8 plan below originally required native and dual-runtime testing, a
50,000-user run, and a 24-hour soak. The repository no longer treats those
runtime observations as versioned source artifacts. The current process builds
one immutable `sha-<40-character-commit>` candidate for every `main` commit.
After the maintainer verifies that candidate with a real Panel and real traffic,
the current release workflow verifies a draft Release, publishes its tag on
`main`, promotes the same digest to the exact version and `latest`, and
publishes GitHub-generated Release notes. Use the current release
guide for publication policy; this archive records only the historical plan.

When this record was written, M0-M7 remediation was complete and M8 covered the
real Panel/Linux, resource, recovery, and long-running tests planned for the
release candidate. Automatic recovery from power loss or a forcibly terminated
installer or supervisor was out of scope. Operators recover by rerunning the
installer, rebooting the host, or recreating the container as appropriate.

## Engineering Principles

- The official implementation defines compatibility, not this project's
  internal architecture. Do not copy fail-open behavior, swallowed errors,
  unbounded resources, or operations that cannot be retried.
- Every process, lifecycle, rule set, and state domain has one owner. Manager
  and Plugin own their internal state; the HTTP lifecycle coordinator orders
  work that crosses those boundaries.
- Register ownership before returning from a successful process spawn. Report
  `stopped` or remove firewall rules only after the leader and descendants are
  confirmed gone.
- Bind each mutation to one rw-core process identity. Its RPC work and local
  hash or state commit run under the same process lease.
- Firewall updates use fail-closed plan/apply/reconcile/commit behavior.
  Failures stay visible, restore prior state where promised, and remain safe to
  retry.
- Bound all input from HTTP, Panel, plugin configuration, rw-core webhooks,
  files, and external commands by bytes, item count, depth, concurrency, time,
  and output size. Errors and logs count toward the same resource budget.
- `512 MiB RAM / 1 vCPU / 2 GiB disk` is a whole-machine target. Go's soft
  memory limit is only one part of the budget alongside rw-core, cgroups,
  nftables state, page cache, installer files, and logs.
- Generate the compatibility oracle independently of the Go implementation.
  Comparing implementation constants with a handwritten copy of the same
  schema proves nothing.

## Completed Remediation Stages

1. **Input and deployment safety:** removed root shell sourcing from OpenRC,
   validated Secrets, and bounded request arrays, validation issues, JSON depth,
   plugin expansion, and log fields.
2. **Xray lifecycle:** registered process ownership atomically, required complete
   descendant cleanup, and added operation and process leases. The HTTP
   coordinator lets starts share access while stops and plugin mutations remain
   exclusive and writer-preferred.
3. **Plugin and connection correctness:** made overloads visible, nft deletion
   idempotent, dynamic blocks durable, cleanup core-first, and socket drops
   retryable. Torrent-rule changes and `recreate-tables` now update only the
   parts that need to change.
4. **Official behavior alignment:** fixed verified differences in stats reset,
   protobuf encoding, HTTP parsing, response models, system information, JWT
   handling, and the Unix configuration target.
5. **Resource boundaries:** added OpenRC cgroup enforcement, low-memory upgrade
   migration, disk and archive preflight checks, reliable shutdown and rollback,
   and process-identity health checks.
6. **Trusted test chain:** derived the static contract from pinned official
   source and SDK evidence, with executable checks for code and artifacts.
7. **Release preparation:** run tests, race detection, vet, static analysis,
   and both architecture builds before tagging. The original plan then called
   for systemd, OpenRC, Panel 2.8.1, real rw-core, 50,000-user, and 24-hour soak
   tests.

## Commit Strategy Used at the Time

- Commit related implementation, tests, and documentation in regression-testable stage-sized batches instead of fragmenting one issue into many micro-commits.
- During an architecture migration, commit the owner and interfaces before their callers; do not retain two sources of truth for an extended period.
- Run the complete gate once after the current batch is finished; repeat it only after diagnosing and fixing a real failure.
- Add official-contract issues and confirmed P1/P2 code defects to the current stage immediately. Track deferred release and operational improvements separately.
