# Project Scope and Goals

[Back to the documentation index](README.md)

Remnanode Lite is a lightweight Remnawave Node written in Go. It aims to behave predictably with Remnawave Panel while running comfortably on small Linux servers.

The project is maintained independently. Official `remnawave/node` defines the protocol and observable behavior we follow, but this repository has its own architecture, versioning, and development history.

The project name is **Remnanode Lite**, and the executable is `remnanode-lite`. Native installations use `remnanode-lite.service` on systemd and `remnanode-lite` on OpenRC. The names are deliberately distinct from the official `remnawave/node` service and container.

## Origin

Remnanode Lite began with a community-written Go implementation. That gave the project a useful starting point, but an early audit found gaps in compatibility evidence, process ownership, concurrency, plugin transactions, input limits, rollback, and resource control.

The repository was then reset with an independent history. We kept code that held up under review and rewrote the parts that did not. The original implementation explains where the project began, but it does not constrain the architecture we maintain today.

## Motivation

Many edge nodes have very little CPU, memory, or disk, but they still need Panel connectivity, rw-core management, live user changes, statistics, and plugins. On these machines, a few common problems matter quickly:

- multiple resident processes, unbounded queues, or duplicate configuration copies that amplify memory use;
- logs, image caches, and installation staging that gradually consume the disk;
- unclear lifecycle and concurrency ownership that allows state to drift on failure paths;
- matching endpoint names without verifying request, response, error, and side-effect semantics;
- installation, upgrade, and external assets without explicit integrity and rollback boundaries.

Remnanode Lite uses one Go process to coordinate the Node API, rw-core lifecycle, and plugins. Request bodies, concurrency, queues, logs, and runtime memory all have explicit limits. The goal is not simply to use less code, but to remain correct, maintainable, and easy to diagnose on a small server.

## Relationship to Official Node

Official Node defines the external behavior that must be considered for compatibility, including:

- HTTP methods and request and response shapes for the `/node` API;
- mTLS, JWT, and connection-handling semantics;
- Xray start, stop, status, and configuration synchronization;
- results and errors for users, statistics, connections, and plugins;
- ordering of side effects observable between Panel and Node.

Each supported contract is tied to a specific official version and commit, backed by executable tests and black-box comparison where needed. When a new official version appears, automation opens a reminder; maintainers still review the changes, update the implementation, and verify it before changing the reported contract version.

The following do not need to match official Node internally:

- implementation language, directory layout, or framework;
- process supervision and container structure;
- internal interfaces, state machines, or dependency injection;
- resource protection that does not change the external contract;
- project-specific diagnostics, tests, and Release tooling.

In this project, compatibility means matching the behavior of a stated official contract. Remnanode Lite is not an official product, a repackaged official image, or a downstream fork.

## Goals

### Verifiable behavior

- Maintain an executable API contract backed by pinned official source evidence.
- Complete input validation before externally visible side effects.
- Align success responses, application errors, connection closure, and retry semantics.
- Use real Panel, rw-core, and Linux environments for behavior that static tests cannot prove.

### Bounded resources

- Target a complete production host with `512 MiB RAM / 1 vCPU / 2 GB disk`.
- Provide consistent Linux `amd64` and `arm64` container images and self-contained Native bundles.
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
- claim compatibility with a new official version before comparison and verification are complete;
- become a general Xray manager, proxy panel, or host firewall manager;
- take ownership of unrelated processes, generic Xray paths, or the host's global firewall policy;
- persist the complete Panel-provided Xray configuration locally and restore it independently after restart;
- isolate multiple Node instances in the same network namespace;
- treat non-Linux platforms as production targets;
- build distributed transactions or high-availability recovery for every extreme event, including power loss and forced process termination.

Recreating a container is an accepted operational recovery method when its runtime state cannot be recovered safely. Native recovery guarantees are limited to the boundaries documented by the installers, service manager, and release documentation.

## Intended audience

| Role | Primary concern | Recommended entry point |
| --- | --- | --- |
| Node deployer | Panel onboarding, low-resource settings, image or bundle selection | [Docker Compose deployment](deployment-docker.md) or [Native Linux](deployment-native.md) |
| Operator | Health, logs, updates, rollback, and fault diagnosis | Operations path in the [documentation index](README.md) |
| Go developer | Package boundaries, lifecycle, testing, and change standards | [Architecture](architecture.md), [development guide](development/README.md) |
| Release maintainer | Versions, compatibility evidence, image tags, and gates | [Versioning](versioning.md), [Release process](release.md) |
| Security or compatibility auditor | Official evidence, resource boundaries, supply chain, and known differences | [Contract baseline](development/contract-2.8.0.md), [resource budget](development/resource-budget.md) |

## Project status

The repository has an independent Git history, automated tests, a GHCR candidate-image workflow, and a Native bundle release workflow. The contract compiled into the binary is recorded in [`internal/version/contract.version`](../internal/version/contract.version); its official source and known differences are documented in the versioned [contract baseline](development/contract-2.8.0.md).

A version in source identifies the build, not a published Release. Check Git tags, GitHub Releases, and exact GHCR tags to see what is actually available. Project version, contract version, Panel integration target, and rw-core version remain separate; [Versioning and image tags](versioning.md) explains how they relate.

## Engineering decision order

Design and review decisions generally follow this order:

1. Implement the declared external contract correctly.
2. Ensure error, cancellation, and concurrency paths cannot publish false success.
3. Establish testable limits for every resource that can grow with input.
4. Reuse clear existing boundaries before introducing abstractions for hypothetical future requirements.
5. Balance low-probability extreme recovery against real operational cost without blocking core compatibility work.
6. Make every compatibility conclusion traceable to source evidence, automated tests, or explicit environment verification.

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
