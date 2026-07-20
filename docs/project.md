# Project Scope and Goals

[Back to the documentation index](README.md)

Remnanode Lite is a lightweight Go implementation of a Remnawave Node. It has two primary concerns: verifiable behavioral compatibility with Remnawave Panel and a clear, predictable operating envelope on resource-constrained Linux nodes.

This is an independently maintained codebase with its own implementation, architecture, versioning, history, and engineering decisions. Official `remnawave/node` is a reference for the external protocol and observable behavior. It is not this repository's Git upstream, and its internal architecture is not a template that must be copied line by line.

The public project name is **Remnanode Lite**, and the executable is `remnanode-lite`. Native installations retain the service name `remnawave-node.service` and its OpenRC counterpart to keep existing host upgrade and operations interfaces stable. Those service names do not imply ownership of or continuity with the official repository.

## Origin

The project initially used a community-written Go implementation as a starting point rather than beginning from an empty directory. The takeover audit found material gaps in contract evidence, process ownership, concurrent state, plugin transactions, input bounds, installation rollback, and resource control. Existing functionality therefore could not be assumed correct or maintainable.

The repository was subsequently reset to an independent history and re-audited, refactored, and completed against this project's engineering standards. Code that is both verified and well designed may remain; anything that fails contract, correctness, security, or maintainability requirements may be replaced. This history explains the starting point but creates no upstream relationship and imposes no obligation to preserve the original architecture.

## Motivation

Many edge nodes have limited CPU, memory, and disk but still need full Panel connectivity, rw-core management, live user changes, statistics, and plugin support. General-purpose deployments often favor larger machines, while small nodes are disproportionately affected by:

- multiple resident processes, unbounded queues, or duplicate configuration copies that amplify memory use;
- logs, image caches, and installation staging that gradually consume the disk;
- unclear lifecycle and concurrency ownership that allows state to drift on failure paths;
- matching endpoint names without verifying request, response, error, and side-effect semantics;
- installation, upgrade, and external assets without explicit integrity and rollback boundaries.

Remnanode Lite uses one Go application process to coordinate the Node API, rw-core lifecycle, and plugins. It bounds request bodies, concurrency, queues, logs, and runtime memory without intentionally changing Panel-observable behavior. Shorter code is not the end goal. The objective is a correct, maintainable, and diagnosable system under tight resource constraints.

## Relationship to Official Node

Official Node defines the external behavior that must be considered for compatibility, including:

- HTTP methods and request and response shapes for the `/node` API;
- mTLS, JWT, and connection-handling semantics;
- Xray start, stop, status, and configuration synchronization;
- results and errors for users, statistics, connections, and plugins;
- ordering of side effects observable between Panel and Node.

This project preserves evidence through a pinned official version and commit, executable contract tests, and controlled black-box comparison where required. When official Node publishes a new version, automation only opens a synchronization reminder. Maintainers must pin the new source, audit differences, adjust the implementation, and complete verification before changing the implemented contract version.

The following do not need to match official Node internally:

- implementation language, directory layout, or framework;
- process supervision and container structure;
- internal interfaces, state machines, or dependency injection;
- resource protection that does not change the external contract;
- project-specific diagnostics, tests, and Release tooling.

Compatibility therefore means behavioral compatibility with a stated contract baseline. It does not mean that Remnanode Lite is an official product, a repackaged official image, or a downstream fork of the official repository.

## Goals

### Verifiable behavior

- Maintain an executable API contract backed by pinned official source evidence.
- Complete input validation before externally visible side effects.
- Align success responses, application errors, connection closure, and retry semantics.
- Use real Panel, rw-core, and Linux environments for behavior that static tests cannot prove.

### Bounded resources

- Target a complete production host with `512 MiB RAM / 1 vCPU / 2 GB disk`.
- Provide consistent Linux `amd64` and `arm64` builds and container images.
- Set explicit limits for memory, request bodies, concurrency, queues, temporary files, and logs.
- Use ephemeral and reclaimable container runtime logs by default; production nodes do not require persistent logs.

### Correct and recoverable state

- Give every process, state object, socket, and nftables rule set one owner.
- Commit local state only after required external effects succeed, and keep failures observable.
- Give concurrent mutations fixed ordering, cancellation propagation, and bounded waits.
- Verify downloaded installation and upgrade assets and roll back within the documented transaction boundary.

### Long-term maintainability

- Keep HTTP details out of application services and isolate system effects behind narrow interfaces.
- Express complex transitions with explicit states and tests instead of implicit Boolean combinations.
- Cover code quality, the official contract, installers, Linux network administration, and containers in CI.
- Document design rationale, support limits, and verification methods rather than only listing commands.

## Non-goals

To keep the project focused, it does not attempt to:

- reproduce the official TypeScript module layout or its internal multi-process structure;
- claim compatibility with a new official version before comparison and acceptance are complete;
- become a general Xray manager, proxy panel, or host firewall manager;
- take ownership of unrelated processes, generic Xray paths, or the host's global firewall policy;
- persist the complete Panel-provided Xray configuration locally and restore it independently after restart;
- isolate multiple Node instances in the same network namespace;
- treat non-Linux platforms as production targets;
- build distributed transactions or high-availability recovery for every extreme event, including power loss and forced process termination.

Recreating a container is an accepted operational recovery method when its runtime state cannot be recovered safely. Native recovery guarantees are limited to the boundaries documented by the installers, service manager, and relevant Release notes.

## Intended audience

| Role | Primary concern | Recommended entry point |
| --- | --- | --- |
| Node deployer | Panel onboarding, low-resource settings, image selection | [Docker Compose deployment](deployment-docker.md) |
| Operator | Health, logs, updates, rollback, and fault diagnosis | Operations path in the [documentation index](README.md) |
| Go developer | Package boundaries, lifecycle, testing, and change standards | [Architecture](architecture.md), [development guide](development/README.md) |
| Release maintainer | Versions, compatibility evidence, image tags, and gates | [Versioning](versioning.md), [Release process](release.md) |
| Security or compatibility auditor | Official evidence, resource boundaries, supply chain, and known differences | [Contract baseline](development/contract-2.8.0.md), [resource budget](development/resource-budget.md) |

## Engineering status

The repository has an independent Git history, a Go implementation, automated tests, and a GHCR candidate-image workflow. The contract baseline compiled into the code remains authoritative in [`internal/version/contract.version`](../internal/version/contract.version); pinned official evidence and known differences are recorded in the versioned [contract baseline](development/contract-2.8.0.md).

The source `Version` identifies what is being built. It does not prove that the version has been published. Determine formal availability from the repository's Git tags, GitHub Releases, exact GHCR tags, and associated Release records. A candidate that connects successfully to a real Panel is valuable evidence, but it does not replace the complete compatibility and Release acceptance declared for that version.

Project version, official contract version, Panel acceptance target, and rw-core version are four separate dimensions. Their relationship and publication rules are defined in [Versioning and image tags](versioning.md).

## Engineering decision order

Design and review decisions generally follow this order:

1. Implement the declared external contract correctly.
2. Ensure error, cancellation, and concurrency paths cannot publish false success.
3. Establish testable limits for every resource that can grow with input.
4. Reuse clear existing boundaries before introducing abstractions for hypothetical future requirements.
5. Balance low-probability extreme recovery against real operational cost without blocking core compatibility work.
6. Make every compatibility conclusion traceable to source evidence, automated tests, or explicit environment acceptance.

## Boundary at a glance

```text
Remnawave Panel
      |
      | mTLS + JWT over HTTPS
      v
remnanode-lite
      |-- rw-core lifecycle and gRPC
      |-- user, statistics, and connection operations
      |-- plugin state and bounded webhook queue
      `-- project-owned nftables rules
```

See [Architecture and runtime design](architecture.md) for component ownership, dependency direction, and runtime data flows.
