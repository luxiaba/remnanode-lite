# Remnanode Lite Documentation

This is the documentation entry point for Remnanode Lite. The root README gives a concise introduction and deployment path; this directory contains the authoritative project scope, architecture, operations, development, compatibility, and Release guidance.

English files in the repository root and `docs/` are the canonical source. Translations are maintained for convenience and may lag behind. See the [localization policy](i18n/README.md) before translating or relying on a localized document.

When documentation conflicts with code, Release assets, or observed behavior, use the truth-source table at the end of this page and correct the documentation in the same change.

## Start by role

### I want to deploy a node

1. Read [Project scope and goals](project.md) to confirm the support boundary and non-goals.
2. Use [Docker Compose deployment](deployment-docker.md) for containers or [Native Linux deployment](deployment-native.md) for systemd and OpenRC.
3. Use the [Configuration reference](configuration.md) for runtime settings, the Panel Secret, and optional capabilities.
4. Read [Versioning and image tags](versioning.md) before selecting `latest`, an exact version, `edge`, or a candidate tag.
5. After startup, follow [Operations and troubleshooting](operations.md) to check container health, Panel connectivity, and rw-core logs.

For a complete host with `512 MiB RAM / 1 vCPU / 2 GB disk`, preserve the repository's memory, CPU, PID, tmpfs, and log limits. Do not build from source on the production node.

### I operate deployed nodes

1. Start with health, logs, updates, rollback, and fault isolation in [Operations and troubleshooting](operations.md).
2. Confirm configuration sources and precedence in the [Configuration reference](configuration.md).
3. Use the [Resource budget](development/resource-budget.md) to understand memory, disk, log, and shutdown budgets.
4. For protocol or lifecycle failures, consult [Architecture and runtime design](architecture.md) and the [2.8.0 contract baseline](development/contract-2.8.0.md).
5. Roll back with a previously recorded exact version or manifest digest. Do not use `edge` or rely on a historical meaning of `latest`.

### I want to read or change the Go code

You do not need to read every design document first:

1. Spend a few minutes on [Project scope and goals](project.md) to understand compatibility boundaries and non-goals.
2. Follow [Development and code navigation](development/README.md) to install the toolchain, run ordinary tests, and find the relevant package.
3. Read only the ownership, data-flow, and lock-order sections of [Architecture and runtime design](architecture.md) that affect your change.
4. Select verification proportional to risk from the [Testing strategy](development/testing.md), then follow [CONTRIBUTING.md](../CONTRIBUTING.md) before submitting.
5. If the change affects `/node` behavior, DTOs, or errors, first read the versioned [official 2.8.0 contract baseline](development/contract-2.8.0.md).

### I want to synchronize an official version or publish a Release

1. Read [Versioning and image tags](versioning.md); never couple project `Version` to `ContractVersion` implicitly.
2. Review the [roadmap](development/roadmap.md) and current contract baseline.
3. Follow the [Release process](release.md) to freeze a candidate, collect evidence, and run the gates.
4. Store acceptance data according to the [Release acceptance evidence protocol](development/release-acceptance.md).
5. Record the result in the root [CHANGELOG](../CHANGELOG.md) and versioned Release note. A version is not published until its Git tag and Release assets actually exist.

## Complete index

### Project and governance

| Document | Purpose |
| --- | --- |
| [Project scope and goals](project.md) | Motivation, relationship to the official project, goals, non-goals, audience, and status |
| [Versioning and image tags](versioning.md) | Project versions, contract versions, aligned Releases, and GHCR tag semantics |
| [Roadmap](development/roadmap.md) | Completed milestones, current Release acceptance, and later work |
| [Contributing](../CONTRIBUTING.md) | Branches, commits, testing, review, and documentation requirements |
| [Security policy](../SECURITY.md) | Private vulnerability reporting, supported versions, and sensitive-data boundaries |
| [Localization policy](i18n/README.md) | Canonical-language rules, translation layout, and synchronization expectations |
| [License](../LICENSE) | AGPL-3.0-only license text |

### Deployment and operations

| Document | Purpose |
| --- | --- |
| [Docker Compose deployment](deployment-docker.md) | Single-file deployment, resource limits, image selection, logs, updates, and rollback |
| [Native Linux deployment](deployment-native.md) | Debian/Ubuntu systemd and Alpine OpenRC installation, upgrade, and uninstall |
| [Configuration reference](configuration.md) | Runtime, container, installer, and build variables with defaults and security rules |
| [Operations and troubleshooting](operations.md) | Health, logs, updates, rollback, disk maintenance, and fault diagnosis |
| [Root Compose file](../compose.yaml) | Executable production container constraints |
| [Single-file Compose template](../deploy/compose.single-file.yaml) | Complete inline-variable template for many independent small nodes |
| [Container environment template](../.env.example) | Variables for the optional `.env` deployment model |
| [Native environment template](../deploy/node.env.example) | Node configuration template used by systemd and OpenRC |
| [Resource budget](development/resource-budget.md) | 512 MiB target, measured engineering baselines, protections, and shutdown budget |

### Architecture, development, and testing

| Document | Purpose |
| --- | --- |
| [Architecture and runtime design](architecture.md) | Component boundaries, request flow, Xray lifecycle, plugins, networking, and ownership |
| [Development and code navigation](development/README.md) | Go toolchain, directory responsibilities, common commands, and workflow |
| [Testing strategy](development/testing.md) | Unit, race, contract, Linux namespace, container, and Release testing |
| [Official 2.8.0 contract baseline](development/contract-2.8.0.md) | Pinned official evidence, 26 routes, request/response behavior, and known differences |
| [Archived 2026-07 audit remediation](archive/2026-07-audit-remediation.md) | Historical scope of the first static audit; not a current truth source |

### Release and acceptance

| Document | Purpose |
| --- | --- |
| [Release process](release.md) | Candidate freeze, acceptance, tags, GitHub Release, GHCR, and rollback |
| [Release acceptance evidence protocol](development/release-acceptance.md) | Evidence files, environments, data boundaries, and machine validation |
| [Changelog](../CHANGELOG.md) | Published and pending user-visible changes |

## Essential concepts

### Project version is not contract version

`Version` identifies a Remnanode Lite build. `ContractVersion` identifies the official Node behavior currently implemented and reported to Panel. Development can enter a future `rnl.N` line without claiming an unfinished official contract. See [Versioning and image tags](versioning.md).

### Candidate image is not a Release

`edge`, automatic `sha-*`, and manual `candidate-sha-*` images are built from `main` for observation and server acceptance. A formal Release requires an immutable-by-policy Git tag, a GitHub Release, and an exact GHCR version tag that actually exist. Use the multi-architecture manifest digest when technical content addressing is required. A version string in source or documentation is not proof of publication.

### Compatibility conclusions need a scope

Static contract tests, a successful Panel connection, long-running resource tests, and distribution acceptance prove different things. Documentation must distinguish implementation, automated verification, environment acceptance, and publication instead of using one as evidence for all four.

## Terminology

| Term | Meaning in this project |
| --- | --- |
| Node | Long-running `remnanode-lite` control process that receives Panel requests and owns rw-core lifecycle |
| rw-core | Xray Core binary that carries proxy traffic and is started and stopped by Node |
| `Version` | Identity of the project build, GitHub Release, and exact image version |
| `ContractVersion` | Official Node behavior baseline implemented and reported to Panel by default |
| operation epoch | Increasing ownership token for one Xray start or stop operation; not process identity |
| process lease | Short-lived authorization bound to one rw-core process epoch and abstract socket, preventing a mutation from crossing processes |
| lifecycle lease | Shared or exclusive HTTP coordination for start, stop, plugin/user mutation, and reset-capable stats; not a persistent lock file |
| candidate `C` | Commit frozen after code enters `main`, used for real-environment acceptance |
| final commit `F` | The one allowed release-record commit after `C`; the formal Git tag points to `F` |
| manifest digest | The `sha256:...` content address of the multi-architecture GHCR index, stronger than a mutable registry tag |

## Sources of truth

| Fact | Primary source | Documentation responsibility |
| --- | --- | --- |
| Project build version | `internal/version/version.go` | Explain meaning without claiming publication |
| Official contract version | `internal/version/contract.version`, `internal/contract` | Record pinned evidence and known differences |
| Public routes | `internal/httpserver/node_routes.go` | Explain behavior and entry points without duplicating another registry |
| Request and response constraints | `internal/contract`, `internal/nodeapi` | Provide a readable summary and verification path |
| Runtime configuration | `internal/config/config.go` | Explain defaults, precedence, and security boundaries |
| Container constraints | `compose.yaml`, `Dockerfile` | Explain capabilities, resources, and tmpfs choices |
| CI and Release behavior | `.github/workflows`, `scripts/*check*.sh` | Keep maintainer procedures synchronized with automation |
| Formal publication state | Git tags, GitHub Releases, exact GHCR tags | Never present a planned asset or URL as existing |
| Resource limits | Code constants, integration tests, candidate evidence | Separate design limits, engineering baselines, and formal acceptance |

## Documentation maintenance

- Update behavior, configuration, versioning, workflow, and corresponding documentation in the same pull request.
- Versioned contracts and benchmark reports describe a specific point in time; use an explicit version or date instead of an ambiguous "current".
- Keep the root README concise and route detailed design and operations material through this index.
- Never place real Secrets, JWTs, certificates, host addresses, or user data in examples.
- Clearly label illustrative version strings. Deployment commands may refer only to an asset that exists, a manifest digest, or a candidate deliberately selected by the operator.
- Historical audits and roadmaps do not override code or automation as the source of current behavior.
- Keep translated documents subordinate to the English canonical source and follow the [localization policy](i18n/README.md).
- Add every new document to this index and verify its relative links.
- Run `go run ./cmd/docs-check` before committing. The repository gate validates H1 headings, code fences, local files, anchors, and reachability from the root README.
