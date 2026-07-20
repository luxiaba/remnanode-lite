# Contributing to Remnanode Lite

Remnanode Lite is an independent Go implementation of Remnawave Node for
resource-constrained Linux hosts. It follows the observable behavior of a
pinned official Node release, but its code, architecture, and release schedule
are maintained here.

When you contribute, make the behavior easy to verify and the design easy for
the next maintainer to follow. New state needs a clear owner, resource use needs
a bound, and failure paths need to be recoverable.

## Before You Start

Read the material relevant to your change:

- [Development guide](docs/development/README.md) for the toolchain, code map,
  and common change paths.
- [Architecture](docs/architecture.md) for request flow, dependency direction,
  concurrency, and lifecycle boundaries.
- [Testing strategy](docs/development/testing.md) for the test layers and the
  minimum checks required for each class of change.
- [Versioning policy](docs/versioning.md) for the independent meanings of the
  project version and official contract version.
- [Documentation index](docs/README.md) for deployment, operations, contract,
  and release material.

Do not open a public issue for a vulnerability or suspected secret exposure.
Use the private process in [SECURITY.md](SECURITY.md), and do not publish
exploitation details.

## Branch Model

- `main` is the protected release branch. A tested `dev` promotion becomes the
  M8 release candidate after it is merged. Once that candidate passes
  acceptance, a release-only branch may add the allowlisted release material.
- `dev` is the stable development and integration branch. Normal features,
  fixes, refactors, and maintenance changes ultimately merge here.
- Daily work belongs on a short-lived topic branch created from the latest
  `dev`.

Start a change with:

```bash
git fetch origin
git switch dev
git pull --ff-only origin dev
git switch -c fix/short-description
```

Use a branch prefix that describes the change:

- `feat/` for a new verified contract capability or project feature.
- `fix/` for a behavior, resource, security, or operational correction.
- `refactor/` for a structural change that preserves external behavior.
- `test/` for tests and acceptance tooling.
- `docs/` for documentation.
- `chore/` for dependencies, CI, releases, and repository maintenance.

Normal pull requests target `dev`. A maintainer preparing a release opens a
`dev -> main` pull request. After candidate acceptance, the maintainer opens a
final `release/v*-docs -> main` pull request containing only the allowed
README, root [`CHANGELOG.md`](CHANGELOG.md), roadmap, evidence, and release-note
paths. See the [release procedure](docs/release.md) for the protected-main
finalization rules.
Do not develop directly on `main`, and do not create or move a final release
tag from a feature pull request.

The official `remnawave/node` repository is a protocol and behavior reference,
not this repository's Git upstream. Never merge or rebase its branches into
this project. Keep the pinned source checkout outside this repository and let
the contract checks verify the exact official commit.

## Define the Change Before Coding

Before implementing a change, answer these questions:

1. Is this purely internal, or can Panel or rw-core observe it?
2. Which component should own any new state, process, queue, or background
   task?
3. What bounds apply to input, memory, disk, concurrency, and external-command
   output?
4. What happens on cancellation, process exit, partial failure, and repeated
   calls?
5. Which macOS stub, Linux implementation, Docker, systemd, or OpenRC paths are
   affected?
6. Which tests prove the behavior rather than merely execute the code?

For a broad change, document the contract, ownership, migration, and validation
plan in an issue or design note first. A narrow and well-understood fix may go
directly to a pull request, but its description must still explain why the
change is needed and how it was tested.

## Implementation Standards

### Go code

- Run `gofmt` on every changed Go file. Avoid unrelated formatting or renames.
- Prefer the standard library and existing internal packages. Do not add a
  dependency for minor syntactic convenience.
- Name packages, types, and functions for domain responsibilities. Avoid vague
  containers such as `util`, `common`, or `manager2`.
- Define small interfaces at the consuming boundary. Do not introduce an
  abstraction solely to make mocking easier when no second real boundary
  exists.
- Constructors must establish complete invariants. Required dependencies must
  not become silently optional through runtime `nil` checks.
- Add operational context while preserving the error chain, for example
  `fmt.Errorf("start core: %w", err)`.
- Comments should explain non-obvious invariants, evidence, or lock ordering,
  not restate the visible operation.
- Add tests with new code. A bug fix should normally include a deterministic
  regression test that fails without the fix.

### Contract and HTTP boundaries

The `/node` API is not an open-ended design surface. Panel may depend on the
HTTP method and path, request shape, union variants, defaults, success body,
error category, status code, connection behavior, and side effects.

When changing externally visible behavior:

1. Locate the evidence in the pinned official source.
2. Update the route, schema, semantic, or source manifest in
   `internal/contract`.
3. Update `internal/nodeapi` and `internal/httpserver` as required.
4. Reach Xray, Stats, Handler, or Plugin through the existing application
   service; do not duplicate domain logic in a route.
5. Add request, response, error, and side-effect tests.
6. During candidate acceptance, compare the official node and candidate with
   `contract-probe`.

Do not diverge from verified official behavior merely because another JSON or
error representation looks more idiomatic in Go.

### State, concurrency, and lifecycle

- `xray.Manager` is the sole owner of the rw-core process, configuration, hash,
  and lifecycle state.
- `plugin.Service` and `plugin.State` own plugin snapshots, firewall plans, and
  torrent reports.
- The lifecycle gate in `httpserver` coordinates operations spanning Xray and
  Plugin.
- The fixed lock order is the outer lifecycle lease, the Plugin operation gate,
  and then Manager-internal state.
- New internal entry points must not bypass these gates, and code holding an
  inner lock must not acquire an outer lock in reverse order.

Propagate `context.Context` through blocking I/O, external commands, gRPC,
queues, and gate waits. Every new goroutine or worker requires an explicit
owner, stop signal, join path, queue capacity, and overload response. The
project does not accept background work that cannot be shut down.

At minimum, concurrency changes require a race test for the affected packages
and deterministic interleaving coverage. Do not substitute long sleeps for
synchronization signals.

### Resource limits

The production target is an entire host with `512 MiB RAM / 1 vCPU / 2 GB
disk`. Every newly consumed resource must have an explicit bound, including:

- Plain and compressed HTTP request bodies.
- JSON, protobuf, command stdout/stderr, and file reads.
- Channels, reports, caches, maps, and goroutine counts.
- Handler, connection, Xray startup, and batch concurrency.
- Log-file size, rotation count, tmpfs use, and persistent writes.
- Startup, stop, retry, and total shutdown time.

Prefer streaming or retaining a hash or summary over keeping a second long-lived
copy of a large configuration. Document the worst case of a resource-affecting
change and use the [testing strategy](docs/development/testing.md) to decide
whether the 50,000-user resource test should be rerun as expanded validation.
Its dated engineering baseline is not evidence for a later frozen candidate.

### Security and secrets

- Never log or commit `SECRET_KEY`, JWTs, client certificates, private keys, or
  complete authentication headers.
- Do not place real node IP addresses, hostnames, or raw Panel responses in test
  fixtures or acceptance evidence.
- Configuration and secret readers must enforce size, file type, symlink,
  owner/mode, and stable-read protections.
- Invoke external commands with argument arrays and bounded input and output.
  Never concatenate untrusted text into a shell command.
- HTTP clients and probes must verify TLS. Do not add a general-purpose option
  to skip verification.
- Docker and native services must preserve least capability, read-only
  filesystem or file-permission controls, and `no-new-privileges`.

If a real secret appears in a local diff, log, or test output, stop propagating
and committing it. Rotate it and clean the affected history; deleting it in a
later commit does not make earlier public history safe.

### Linux and cross-platform code

Linux is the production platform. Put Linux-only behavior behind
`//go:build linux`, and provide a non-Linux stub so ordinary development builds
remain possible. A stub may report unavailability or a portable degradation;
it must not claim that nftables, netlink, capabilities, or process groups
succeeded.

When changing a Linux-specific path:

- Run ordinary package tests on macOS or Linux as applicable.
- Run the corresponding unit tests on Linux.
- For nftables or socket destruction, run the isolated network-namespace
  integration tests.
- For service-manager behavior, account for both systemd and OpenRC.

The exact commands are in the
[testing strategy](docs/development/testing.md).

### Shell, installers, and service definitions

- Bash scripts use `set -euo pipefail`. OpenRC service files remain compatible
  with POSIX `sh`.
- Shell and service files use LF line endings; `.gitattributes` enforces this.
- Replace files through a restricted temporary directory and an atomic rename
  after validation, retaining an explicit rollback point.
- Do not bypass the installer's shared lock, trust roots, download budgets,
  path validation, or secret migration.
- Keep systemd and OpenRC user, capability, resource, stop, and uninstall
  semantics aligned.
- Installer changes require the offline operational tests. Installing on a
  real host does not replace failure-injection coverage.

### Generated code, dependencies, and supply chain

`internal/xrayrpc/wire/wire.pb.go` is generated code and must not be edited by
hand. Regenerate wire-schema changes with `scripts/generate-protobuf.sh`, using
the pinned `protoc 35.1` and `protoc-gen-go v1.36.11`. Run the protobuf golden
test, and prove there is no generated drift before submission:

```bash
bash scripts/generate-protobuf.sh --check
```

When adding or upgrading a dependency:

- Explain why the standard library and existing dependencies are insufficient.
- Run `go mod tidy -diff`, `go mod verify`, and `govulncheck`.
- Evaluate binary size, initialization cost, resident memory, and transitive
  dependencies.
- Pin GitHub Actions to a complete 40-character commit SHA.
- Pin Docker base images, downloaded assets, rw-core, and ASN sources to a
  version or digest and verify their SHA-256 values.
- Update the supply-chain checks with the source change. Never change only a
  URL in a way that leaves static validation checking the old assumption.

## Documentation and Changelog

Update the canonical documentation in the same pull request whenever a change
affects:

- User-visible configuration, defaults, resource limits, or deployment steps.
- APIs, the official contract version, or known differences.
- Architecture boundaries, lock order, state ownership, or shutdown semantics.
- Branches, CI, versions, image tags, or the release process.
- Install, upgrade, rollback, or uninstall behavior.

Record user-visible changes in the root [`CHANGELOG.md`](CHANGELOG.md). Do not
describe unfinished acceptance as a release. Avoid copying volatile "current
status" statements into several documents; link to the versioning policy,
contract baseline, or release note that owns the fact.

The canonical CI workflow is [`.github/workflows/ci.yml`](.github/workflows/ci.yml),
and the supported single-file deployment template is
[`deploy/compose.single-file.yaml`](deploy/compose.single-file.yaml). Historical
audit records live under `docs/archive/`; the
[2026 audit remediation record](docs/archive/2026-07-audit-remediation.md), for
example, is context only and not a current architecture or release-status
source.

For documentation-only changes, run at least:

```bash
go run ./cmd/docs-check
git diff --check
```

Execute documented commands in their supported environment when practical. If
that is not possible, state the prerequisites, placeholders, and destructive
scope explicitly.

## Testing Requirements

Test scope follows risk; the full matrix is in the
[testing strategy](docs/development/testing.md).
The minimum expectations are:

- Run affected-package tests during development.
- Run affected-package race tests for state or concurrency changes.
- For API changes, run `nodeapi`, `httpserver`, `contract`, and pinned-source
  evidence tests.
- Run repository checks for shell or deployment changes; add offline
  operational tests for installer changes.
- Run Linux network-namespace tests for nftables or netlink changes.
- Run the low-memory test for resource ceilings or large-configuration paths.

Before opening a pull request, run the repository checks equivalent to CI when
your environment supports them:

```bash
REMNANODE_OFFICIAL_SOURCE=/absolute/path/to/pinned-official-source \
REQUIRE_GOVULNCHECK=1 \
  bash scripts/check.sh
```

This command does not exercise a real Panel, Linux network namespaces, a real
rw-core, or a long soak. List platform tests you could not run in the pull
request; an expanded test can be non-blocking for a particular release profile,
but it must never be reported as passed when it was not run.

CI is defined in [`.github/workflows/ci.yml`](.github/workflows/ci.yml). Its
`ci / gate` job aggregates Go and race checks, repository and packaging checks,
offline installer operations, and isolated Linux network-administration tests.

## Commits

Use Conventional Commits:

```text
feat(contract): support a verified node route
fix(xray): preserve cancellation while waiting for startup
refactor(plugin): isolate firewall plan construction
test(installer): cover failed service rollback
docs: add the developer testing guide
chore(deps): update grpc with verified module state
```

- Write an imperative subject that describes the result. Avoid vague messages
  such as `update files` or `fix bug`.
- Use a stable component for the scope, such as `xray`, `plugin`, `contract`,
  `installer`, or `container`.
- Keep each commit to one explainable and verifiable logical change, but do not
  mechanically split every small step of one fix.
- Exclude unrelated formatting, generated output, and local configuration.
- Before committing, inspect the staged diff and run
  `git diff --cached --check`.

Large work may use a small number of coherent checkpoints, such as contract,
core implementation, and deployment/documentation. Each checkpoint should be
independently reviewable; every minor adjustment does not need its own commit.

## Pull Requests

Normal pull requests target `dev`. The description should include:

- The problem and user-observable impact.
- The chosen design and why the responsibility belongs to that component.
- Official contract evidence, or a statement that external behavior is
  unchanged.
- Concurrency, resource, security, and platform effects.
- Commands actually run and their results, with unrun environment tests listed
  explicitly.
- Configuration, deployment, migration, rollback, and documentation changes.

Before requesting review, confirm:

- [ ] The branch is based on the latest `dev`, and the pull request targets
      `dev`.
- [ ] The diff contains no secrets, local `.env`, real node data, or unrelated
      changes.
- [ ] Go code is formatted and module state did not drift accidentally.
- [ ] Regression tests were added or updated, and risk-appropriate checks ran.
- [ ] Linux-only behavior is not reported as validated solely through a macOS
      stub.
- [ ] User-visible changes update the canonical documentation and root
      `CHANGELOG.md`.
- [ ] New dependencies, Actions, and downloaded assets are pinned and covered
      by supply-chain checks.
- [ ] No final tag was created, overwritten, or moved early.

Review prioritizes correctness and contract fidelity, state and concurrency,
resource bounds, security, maintainability, and then style. If review exposes
an ownership or design problem, fix the design and tests instead of defending
the wrong boundary with more comments.

## Release Boundary

A normal contribution ends when it merges into `dev`; contributors do not
publish it independently. Maintainers control the `dev -> main` promotion,
candidate images, final tags, `latest`, release assets, and acceptance evidence
under the [versioning policy](docs/versioning.md) and
[release procedure](docs/release.md).

The release procedure calls the frozen code candidate on `main` `C`. After M8
acceptance, one squash-merged finalization pull request adds the allowlisted
release material and produces `F`. The Git tag points to `F`; the container
version tag promotes the accepted manifest built from `C`. Candidate tags and
commit-SHA images are not releases.

Release gates require a clean worktree, the pinned official source, a frozen
candidate, and the evidence required by that version's acceptance profile. Do
not weaken a check to work around missing evidence or describe `edge`, `sha-*`,
or `candidate-sha-*` images as releases.

Before `v2.8.0` can be published, its frozen image digest must pass
`docker-production-smoke-v1` on a production `amd64` host. The blocking record
covers the production Compose template, expected version, real Panel
connectivity and proxy traffic, cgroup memory and PID observations, a healthy
running container, no OOM kill, and zero restarts. The `arm64` runtime,
`native-systemd-install`, `native-openrc-install`, candidate 50,000-user load,
24-hour soak, and fault/rollback injection remain deferred and non-blocking and
must be disclosed that way.

Operator-attested runtime observations are accountable audit claims. Validation
can bind a record to a candidate commit and digest and check its schema and
internal consistency; it cannot make the underlying observation unforgeable or
independently prove that the run occurred. Never manufacture or overstate such
evidence.

## License

By contributing, you confirm that you have the right to provide the code and
documentation and agree that it will be distributed under the repository's
[AGPL-3.0-only license](LICENSE). Before quoting or adapting an external
implementation, verify license compatibility and retain attribution when
required.
