# Testing Guide

[Back to developer documentation](README.md) · [Contribution guide](../../CONTRIBUTING.md)

This guide covers Remnanode Lite's test layers, platform boundaries, and the commands used to exercise them. Match the cost of a check to the risk of the change, and keep “passed on this workstation” distinct from “verified for Linux and Panel production behavior.”

## Principles

- Run the owning package first during development, then expand the scope after a coherent batch.
- Changes to state, locks, goroutines, cancellation, or lifecycle behavior require race testing.
- Changes to officially observable behavior require the pinned-source contract tests.
- Only Linux tests can support claims about capabilities, netlink, nftables, process groups, or cgroups.
- Before dispatching a release, verify the immutable `sha-<40-character-main-commit>` candidate with a real Panel and real proxy traffic under the production limits. This is a manual release decision; runtime observations are not committed to the repository.
- The exact whole-host 512 MiB target, untested Native distribution/architecture combinations, large-user load, long soak, and fault injection remain useful follow-up checks. Unit tests must not be presented as substitutes for an environment they do not exercise.
- Test data must not contain real Secrets, JWTs, certificates, private keys, node IPs, hostnames, or raw responses.

## Quick Selection

| Scenario | Command | Expected cost |
| --- | --- | --- |
| Change one Go package | `go test -count=1 ./internal/<package>` | Low |
| Change concurrency or shared state | `go test -race -count=1 ./internal/<package>` | Medium |
| Normal Go regression | `go test -count=1 ./...` | Medium |
| Targeted parser fuzzing | `go test ./internal/<package> -run '^$' -fuzz '^FuzzName$' -fuzztime=30s` | Bounded local run |
| Go pre-commit gate | `bash scripts/check-go.sh` | Medium to high |
| Shell, Docker, workflow, or supply chain | `bash scripts/check-repository.sh` | Medium to high |
| Native bootstrap or bundle format | `sh release/native/install_test.sh`, `go test ./cmd/release-tool` | Medium to high |
| Native lifecycle state or service adapter | `go test ./internal/rnlctl ./cmd/rnlctl` | High |
| `rnlctl` command, progress, help, or completion surface | `go test ./internal/rnlctl` | Low |
| Native host qualification | Full-system VM lifecycle and reboot on every claimed architecture | Linux/root |
| Complete repository gate | `REQUIRE_GOVULNCHECK=1 bash scripts/check.sh` | High |
| Linux network management | Two network-namespace integration tests | Linux/root |
| Low-memory budget | `scripts/test-low-memory.sh --rw-core ...` | Docker/real core |
| Official-versus-candidate behavior | `go run ./cmd/contract-probe ...` | Isolated test environment |
| Formal release | `bash scripts/release-check.sh` | Prepared release source only |

## Go Tests

### Targeted Package Loop

During an edit, run the nearest package first:

```bash
go test -count=1 ./internal/httpserver
go test -run '^TestName$' -count=1 ./internal/httpserver
go test -race -count=1 ./internal/httpserver
```

`-count=1` disables Go's test-result cache, so the command always tests the current implementation. Use `-race` for concurrency work. Do not add sleeps to hide missing synchronization or cancellation propagation.

The Go race detector requires CGO and a working C compiler. If the build toolchain is missing, repair the development environment; a skipped race test is not a passing result.

### Normal Full Regression

```bash
go test -count=1 ./...
```

This runs every ordinary test that compiles on the current platform. Real integration tests in the repository remain protected by environment variables and call `Skip` unless explicitly enabled.

On macOS, files guarded by `//go:build linux` do not compile into the test, including Linux process, nftables, and netlink socket-destruction implementations. `go test ./...` on macOS is useful for fast regression but is not a complete Linux result. On Linux, the same command compiles Linux unit tests; network-namespace and real rw-core tests still require explicit activation.

### Targeted Parser Fuzzing

The repository keeps focused Go fuzz targets at external or integrity-sensitive parsing boundaries. A normal `go test` run executes their small seed corpus as ordinary regression tests; it does not start mutation fuzzing. Run one target at a time with an explicit budget when changing its parser or before accepting a release candidate:

```bash
go test ./internal/secret -run '^$' -fuzz '^FuzzParse$' -fuzztime=30s
go test ./internal/rnlctl -run '^$' -fuzz '^FuzzDecodeReleaseManifest$' -fuzztime=30s
```

Keep fuzz harnesses deterministic, side-effect free, and bounded using explicit parser-appropriate caps. Do not invoke services, real nftables, or full Native archive extraction from a fuzz target. Long-running mutation fuzzing is deliberately absent from the required pull-request workflow; adding it there would increase cost and nondeterministic failure handling without improving the seed regression gate.

Only commit a minimized `testdata/fuzz` input when it reproduces a confirmed defect that a readable unit-test fixture cannot express as well. The corpus must follow the repository's test-data policy and contain no production material.

### Standard Go Gate

```bash
bash scripts/check-go.sh
```

The script performs, in order:

1. Whitespace checks for the worktree and index.
2. `gofmt` verification for every tracked and unignored Go file.
3. Project-version format, contract-version format, cross-file synchronization, and official-alignment version checks.
4. `go mod verify` and `go mod tidy -diff`.
5. The normal full test suite.
6. The race-instrumented runtime suite: `./internal/...` and
   `./cmd/remnanode-lite`.
7. `go vet ./...`.

The ordinary suite still covers every package. Race instrumentation covers all
`internal` packages and the daemon command, while excluding the other `cmd/*`
helper commands such as `release-tool` and `docs-check`. This stable package-root
boundary includes a few inexpensive pure-logic packages but avoids repeatedly
instrumenting build-heavy offline tools that own no production concurrency.
Each material stage prints its elapsed time so a slow test, scanner, or cold
build is visible in the job log.

The script does not prepare official source automatically. Without `REMNANODE_OFFICIAL_SOURCE`, regeneration from the pinned Git object is skipped, but the checked-in source manifest is still compared offline with the local Go route contract. A change that aligns official behavior should therefore prepare the official Git repository as described below.

## Pinned Official Source Contract Tests

Read the contract version from `internal/version/contract.version` instead of duplicating it in commands:

```bash
contract_version="$(tr -d '[:space:]' < internal/version/contract.version)"
official_dir="../remnawave-node-official-${contract_version}"

git clone --depth 1 --branch "$contract_version" \
  https://github.com/remnawave/node.git "$official_dir"

export REMNANODE_OFFICIAL_SOURCE="$(cd "$official_dir" && pwd)"
go run ./cmd/contract-source-check
go test -count=1 ./internal/contract
```

`contract-source-check` reads the pinned commit object directly, disables replace refs, and does not trust the checkout, index, or `HEAD`. It verifies every evidence-blob digest and rebuilds the method/path manifest from the official `REST_API`, global prefix, route constants, and controller decorators.

The same check enumerates controllers and modules from the Git tree. It then verifies the actual Nest bootstrap, static imports, strict metadata, decorator ownership, module-registration reachability, and prefix exclusions for internal controllers.

Unknown conditions, spreads, aliases, composite decorators, or unapproved dynamic modules fail closed rather than being guessed. Keep the environment variable when running the Go gate so the contract package repeats the source verification:

```bash
REMNANODE_OFFICIAL_SOURCE="$REMNANODE_OFFICIAL_SOURCE" \
  bash scripts/check-go.sh
```

This is required for changes to:

- `/node` methods, paths, or route count.
- Request fields, unions, defaults, or unknown-field handling.
- Success responses, application errors, HTTP status, or transport-close semantics.
- Side effects such as stats reset, user mutations, or plugin synchronization.
- The official contract version or pinned commit.

These commands do not start the official Node. Machine extraction proves pinned source contents and public route mapping; it does not claim to translate complete Zod semantics into Go. Local executable schemas remain covered by boundary tests, and real service behavior is compared with `contract-probe` later in this guide.

### External Plugin Schema Evidence

The official Node's plugin `config` schema comes from a separate npm package and is not part of the pinned source repository. Recheck the current `@remnawave/node-plugins@0.7.3` tarball in an isolated temporary directory:

```bash
plugin_tgz="$(mktemp)"
trap 'rm -f "$plugin_tgz"' EXIT

curl --fail --location --silent --show-error \
  --proto '=https' --tlsv1.2 \
  https://registry.npmjs.org/@remnawave/node-plugins/-/node-plugins-0.7.3.tgz \
  -o "$plugin_tgz"

test "$(openssl dgst -sha1 "$plugin_tgz" | awk '{print $NF}')" = \
  d58cc34d15838d6ac543c112ac65265d6189745e
test "sha512-$(openssl dgst -sha512 -binary "$plugin_tgz" | openssl base64 -A)" = \
  'sha512-y1+dIrZVENchojkBJHC5KHocTTDl/xeCdIvbYgoTWYZ5pWuIkyoFYm/fGUA//W3DQ6eAlAKkg1HC24MeKAoIvA=='

tar -tzf "$plugin_tgz" \
  | grep -Fx 'package/build/backend/models/node-plugins.schema.js'
```

Read the actual schema with `tar -xOf` from that pinned path; do not install the package or execute its code. CI does not download this tarball. Automated source evidence covers only registered paths in the official Git repository.

An upgrade to the plugin version must reconcile official `package.json` and `package-lock.json`, update both digests, re-audit the schema, and adjust `internal/nodeapi`, `internal/plugin`, and the related contract tests.

## Repository and Static Checks

### Tool Versions

`scripts/check-repository.sh` requires:

- A Go toolchain exactly matching `go.mod`.
- ShellCheck exactly `0.11.0`.
- An available actionlint executable; use `1.7.12` for CI parity.

Install the Go tools with:

```bash
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
```

Do not call `scripts/install-ci-checks.sh` locally. It is a GitHub Runner bootstrap and depends on `GITHUB_PATH`, `RUNNER_TEMP`, Linux archives, and `sha256sum`.

### Repository Gate

```bash
bash scripts/check-repository.sh
```

The script runs:

- `git diff --check`.
- `go run ./cmd/docs-check`, which checks Markdown H1s, fences, local links, anchors, and entry-point reachability.
- ShellCheck, `bash -n` for every Bash script, and `sh -n` for the OpenRC script.
- actionlint.
- Docker/Compose packaging-policy checks.
- Supply-chain checks for download sources, pinned digests, Action SHAs, and installer bootstrap.
- One `govulncheck ./...` scan when the tool is available or required.
- Cross-builds of Linux `amd64` and `arm64` binaries with the exact Go toolchain.

When Docker Compose is available, the packaging test also validates the Compose schema. If it is unavailable, the script prints an explicit skip while continuing the other static policy checks. Do not ignore that skip when claiming Compose validation.

### Vulnerability Scan and Complete Repository Check

Run the scanner directly with:

```bash
govulncheck ./...
```

The normal complete-repository entry point is:

```bash
REMNANODE_OFFICIAL_SOURCE="$REMNANODE_OFFICIAL_SOURCE" \
REQUIRE_GOVULNCHECK=1 \
  bash scripts/check.sh
```

`check.sh` combines the Go gate, repository gate, and offline Native bootstrap
tests. It is the top-level entry point: do not run `check-go.sh` or
`check-repository.sh` immediately before it on the same checkout. Doing so only
repeats work already included by `check.sh`.

`check-repository.sh` owns the single vulnerability-scan invocation, so the
complete and release gates do not run another scan internally. If
`REQUIRE_GOVULNCHECK=1` is not set and govulncheck is unavailable, the
repository gate skips it. Required CI sets the flag, so a release can only
consume a candidate whose CI ran the scanner.

A successful `check.sh` run does not prove production behavior. It does not run the candidate image with a real Panel and real traffic, nor does it exercise every supported architecture, init system, host size, load, or fault path.

## Native Delivery and Lifecycle Tests

A change to `release/native/install.sh`, the release bundle format, `rnlctl`,
service definitions, account ownership, upgrade, rollback, repair, or
uninstall requires the focused checks below:

```bash
sh release/native/install_test.sh
go test -count=1 ./cmd/release-tool ./internal/rnlctl ./cmd/rnlctl
go test -race -count=1 ./internal/rnlctl
```

The bootstrap fixtures exercise exact-version downloads, local archive
checksums, `--yes`, `--prepare-only`, `auto|plain|never` progress and forwarding,
Secret-file handling, and refusal of moving channels. `internal/rnlctl` tests
use temporary roots and service fakes to cover strict manifests, locks and
journals, atomic generation selection, service-state restoration, rollback,
repair, account ownership, and purge safety. They never write the real
`/etc/remnanode-lite` tree or start a host service.

`--prepare-only` proves bundle verification and host-file preparation only. It
does not start the service, apply cgroup limits, or qualify an Alpine/OpenRC
host. Those checks first become authoritative during `rnlctl activate`.

For a command-name, option, help, or completion-only change, start with
`go test -count=1 ./internal/rnlctl`. The command specification drives help and
completion, while parser behavior and public-output expectations remain
independent tests; do not replace those tests with output generated from the
same specification.

Progress or signal-handling changes must independently cover TTY and non-TTY
stderr, all three progress modes, quiet and JSON suppression, `NO_COLOR` and
`TERM=dumb`, known and unknown transfer sizes, narrow terminals, write failure,
and interrupted operations. Verify that the first signal cancels through the
active context, required compensation remains bounded, Ctrl-C maps to `130`,
and a repeated signal can fall back to the operating-system default.

Build and verify real bundles when the archive shape, runtime assets, or
release scripts change:

```bash
mkdir -p dist/native
bash scripts/build-native-bundle.sh dist/native amd64 arm64
version="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' internal/version/version.go)"
bash scripts/test-native-release-bundle.sh "dist/native/remnanode-lite_${version}_linux_amd64.tar.gz" amd64
bash scripts/test-native-release-bundle.sh "dist/native/remnanode-lite_${version}_linux_arm64.tar.gz" arm64
```

The build requires the exact Go toolchain and the pinned runtime asset cache.
Use `RNL_OFFLINE_BUILD=1` only with a complete cache. The bundle smoke test
opens the generated archive with real `rnlctl` lifecycle code, installs it into
a temporary test root with a restrictive `umask`, and keeps the service-manager
boundary fake. It does not replace a real systemd or full-system Alpine/OpenRC
check when service-manager behavior changed.

### Native distribution qualification

Initial promotion of a Native platform requires a persistent full-system VM
using the distribution's own kernel and default security profile for every
claimed architecture. Repeat the relevant platform or architecture checks when
a later change affects that service-manager path, native binary shape, or
architecture-specific assets; unrelated releases do not require a full
requalification. Containers and init-less guests are useful for portable
installer tests, and constrained nested guests are useful for testing rejection,
but neither qualifies a distribution. OrbStack Linux machines are useful smoke
environments, but their shared kernel, container boundary, and host-supplied
systemd overrides mean that they can establish only Qualification candidate
status.

For an Alpine/OpenRC target listed in the
[Native host matrix](../deployment-native.md#native-host-matrix), use a persistent
`sys` installation on the claimed `amd64` or `arm64` architecture. It must boot
through Alpine's normal init/OpenRC stack, use Linux 5.14 or newer, and expose
unified cgroup v2 with usable `cpu`, `memory`, and `pids` controllers,
`memory.swap.max`, writable parent `cgroup.procs`, and writable service
`cgroup.kill`. Exercise at least:

1. dependency installation and boot-time `cgroups` enablement;
2. exact-bundle prepare and activation with non-production test credentials;
3. `rnlctl status`, `doctor`, and `config check`;
4. the expected memory, swap, CPU, and PID limits, plus the two documented
   capabilities and `NoNewPrivs` on the managed Node process;
5. an exact-version upgrade followed by rollback, with the service returning to
   health after each transition;
6. repair from the retained verified cache and one intentional interrupted or
   damaged-state recovery case;
7. stop cleanup of the exact service cgroup, followed by start and restart;
8. a full VM reboot, automatic service return, health, and unchanged limits;
   and
9. normal uninstall plus `--purge --yes`, confirming that unrelated accounts,
   processes, files, and firewall state remain untouched.

Local health does not prove Panel connectivity or proxy traffic. Record those
as separate release acceptance when they were actually tested, and keep host
details, credentials, logs, and smoke output outside the repository.

## Linux Network-Management Integration Tests

On a Linux host with user/network namespaces, nftables, and root privileges:

```bash
sudo env "PATH=$PATH" REMNANODE_NFT_INTEGRATION=1 \
  go test ./internal/plugin \
  -run '^TestNFTManagerInNetworkNamespace$' -count=1 -v

sudo env "PATH=$PATH" REMNANODE_SOCKET_KILL_INTEGRATION=1 \
  go test ./internal/netadmin \
  -run '^TestKillSocketsInNetworkNamespace$' -count=1 -v
```

Ubuntu 24.04 is recommended for CI parity:

```bash
sudo apt-get update
sudo apt-get install --yes iproute2 nftables
```

These tests operate only inside isolated namespaces. Do not remove their environment-variable guards or modify them to operate on the development host's default network namespace.

## Low-Memory Resource Test

The resource test places the test process and a real rw-core in the same Docker cgroup. Its defaults are `448 MiB / 1 CPU / no swap / 256 PIDs / 50,000 users`:

```bash
scripts/test-low-memory.sh \
  --rw-core /path/to/linux/rw-core \
  --users 50000 \
  --memory 448
```

Prerequisites:

- A running Docker daemon.
- `--rw-core` points to an executable Linux rw-core for the same architecture as Docker.
- The host supports Docker memory, CPU, swap, and PID limits.

The dated M6 50,000-user result is an engineering baseline for comparing later resource-sensitive changes; it does not characterize a different build automatically.

Run this test after changes to resource handling, request parsing, retained configuration, queues, logs, concurrency limits, or the rw-core lifecycle. Record the cgroup peak; the Go process RSS alone is not the relevant metric. See the [resource budget](resource-budget.md) for the dated baseline.

## Docker and Image Tests

To validate only packaging policy and the Compose schema:

```bash
bash scripts/test-docker-packaging.sh
```

A local source-image build downloads pinned base images plus rw-core, geo, and ASN assets. It costs substantially more than a Go build and is appropriate only after changes to the Dockerfile, build arguments, or runtime assets:

```bash
SECRET_KEY=packaging-check \
  docker compose -f compose.yaml -f compose.build.yaml build
```

`packaging-check` is only a Compose-parsing placeholder and cannot start a node. A real start requires the complete Secret generated by Panel and the security requirements in the [Docker deployment guide](../deployment-docker.md).

Both maintained production Compose files use `remnanode-lite` as the service,
container, and hostname. Compose reads `.env` for interpolation, shell values
take precedence, and only the explicitly mapped runtime variables enter the
container. The packaging test verifies both templates resolve to the same
service, exercises all supported `.env` overrides, and requires Compose
expansion to fail when `SECRET_KEY` is unset or empty.

## Black-Box Contract Comparison

List the routes and their default safety class:

```bash
go run ./cmd/contract-probe -list
```

Prepare a Panel client certificate signed by the same CA and keep the JWT in a separate file:

```bash
export REMNANODE_CONTRACT_CA=/secure/ca.pem
export REMNANODE_CONTRACT_CERT=/secure/panel-client-cert.pem
export REMNANODE_CONTRACT_KEY=/secure/panel-client-key.pem

go run ./cmd/contract-probe \
  -token-file /secure/panel.jwt \
  -target official=https://127.0.0.1:2222 \
  -target candidate=https://127.0.0.1:3222
```

The first target is the comparison baseline. By default, the probe runs only non-destructive safe routes. Start, stop, user mutations, connection cleanup, statistics reset, report draining, and nftables operations require both explicit `-routes` and `-allow-mutating`, and must run only in an isolated test environment.

The probe never prints the JWT or raw response bodies. If a certificate contains only DNS names while the target uses an IP address, pass `-server-name`; there is no option to disable TLS verification.

## Release Gate

```bash
REMNANODE_OFFICIAL_SOURCE="$REMNANODE_OFFICIAL_SOURCE" \
REQUIRE_GOVULNCHECK=1 \
  bash scripts/release-check.sh
```

This script is the final source preflight. It expects a clean worktree, a dated
`CHANGELOG.md` entry, the pinned official source, and the complete repository
checks. It derives the release tag from the project version unless
`RELEASE_TAG` is explicitly set, and always verifies that they match. Use it instead of
`check.sh` when preparing release source; never run `check.sh` first on the same
checkout. It runs before the release workflow creates a draft Release and later
publishes its tag.

The container released for that version is not rebuilt. Every `main` commit
first publishes `sha-<40-character-commit>`. Before dispatching the release
workflow, the maintainer manually confirms that this exact candidate starts cleanly, connects to a real
Panel, and carries real proxy traffic under the production container limits.
Do not add host details, logs, container identifiers, or runtime JSON to the
repository.

The workflow requires the dispatch commit to remain the current `main` HEAD.
It resolves that commit's `sha-*` candidate, requires the runnable
`linux/amd64` and `linux/arm64` manifests, validates each platform's SPDX SBOM
and BuildKit attestations, verifies the GitHub attestations for the prebuilt
Native assets and their `release-index.json` digest binding, creates and
verifies a draft Release, promotes the same digest to the exact version before
publication, then verifies the immutable Release and reconfirms the exact tag
without rebuilding. A plain
`X.Y.Z` Release is stable and advances `latest`; an
`X.Y.Z-rnl.N` Release is a GitHub prerelease and advances `preview` only.
If publication succeeds but registry promotion does not, `reconcile-release`
revalidates the published Release and its attested digest record before
restoring the exact tag and eligible channel.

See the [versioning policy](../versioning.md) for tag, version, and channel semantics, and the [release process](../release.md) for candidate verification and publication steps.

## Selecting Tests by Change

| Change | Minimum verification | Expanded verification |
| --- | --- | --- |
| Documentation only | `go run ./cmd/docs-check`, `git diff --check`; manually verify command facts | Run the relevant script for release/deployment documentation |
| Ordinary Go logic | Owning package tests | `bash scripts/check-go.sh` |
| Locks, state, workers, shutdown | Owning package race test | Runtime race suite and related lifecycle tests |
| HTTP/API/schema | `nodeapi`, `httpserver`, `contract` | Pinned-source contract tests and black-box comparison |
| Xray lifecycle | `xray` and `httpserver` race tests | Real Panel/container check; resource test when risk requires it |
| Users and statistics | `nodehandler`, `stats`, `xrayrpc` | Contract response tests and Panel differential testing |
| Plugin pure logic | `plugin` race test | HTTP lifecycle interleaving tests |
| nftables/socket destruction | Corresponding Linux unit test | Both namespace integration tests |
| Configuration/Secret/JWT | `config`, `secret`, `auth`, server security | Installer Secret flow |
| Native bootstrap | `sh release/native/install_test.sh` | Exact-release install on the target host |
| Native lifecycle/service | `go test ./internal/rnlctl ./cmd/rnlctl`, `go test -race ./internal/rnlctl` | Real systemd or full-system Alpine/OpenRC host when the change affects Native runtime behavior |
| `rnlctl` command and progress surface | `go test ./internal/rnlctl` | Shell completion syntax, output-mode, signal, and command-surface regression suites |
| Docker/Compose | `bash scripts/test-docker-packaging.sh` | Multi-architecture image build plus risk-driven real-environment verification |
| Dependency or downloadable asset | `go mod tidy -diff`, supply-chain checks, govulncheck | Dual-architecture build, SBOM, and attestation |
| Project version | `bash scripts/check-version.sh` | Release preflight |
| Official contract upgrade | Full contract and pinned-source tests | Black-box all registered routes and complete Panel flow |
| Protobuf wire | `scripts/generate-protobuf.sh --check`, `go test ./internal/xrayrpc` | Real rw-core and golden-wire regression |
| Resource limit | Related unit/race tests | Risk-driven `test-low-memory.sh`, large-user load, or soak |

“Minimum verification” is for the development loop, not necessarily the entire pull-request requirement. For a cross-component change, take the union of the applicable rows.

## CI Mapping

The required gate in `.github/workflows/ci.yml` aggregates four parallel jobs:

| CI job | Primary command |
| --- | --- |
| `go` | Pinned official source plus `scripts/check-go.sh` |
| `repository` | Install pinned static tools plus `scripts/check-repository.sh` |
| `native` | `sh release/native/install_test.sh` |
| `netadmin` | Both Linux namespace integration tests |
| `gate` | Requires every job above to report success |

For pull requests targeting `main`, the `repository` job fetches the full Git
tag history and runs the release-metadata check before installing the static
tools. It rejects a reused published version, an out-of-order preview revision,
an undated changelog, or unsynchronized version references before merge. Normal
pull requests targeting `dev` do not run this release-only preflight.

The candidate workflow remains path-filtered for pull requests, so it does not
run on every PR. Every push to `main`, however, builds and attests the manifest
and publishes the immutable `sha-<40-character-commit>` candidate. The manual
release workflow resolves that candidate, verifies its shape and attestation,
including both per-platform SPDX SBOMs and the Release digest binding, promotes
the exact tag before publishing, then requires the GitHub Release to become
immutable and reconfirms the exact tag. A path-filtered “not run” is not a
failure, and an optional container job must not become a required check on pull
requests where it cannot appear.

## Writing Tests

- Prefer the standard `testing` package, local fakes, and narrow interfaces; do not add a dependency only for assertion syntax.
- Use `t.TempDir()`, `t.Setenv()`, and test-only ports. Never write to real system paths.
- Synchronize concurrent tests with channels, contexts, or explicit signals rather than fragile sleep ordering.
- Give every potentially blocking test a deadline. Failure messages should include the actual value, expected value, and operation stage.
- Linux integration tests require a build tag and explicit environment-variable guard.
- Contract tests cover valid input, missing fields, wrong types, unions, extra fields, and response schemas together.
- Resource tests assert bounded peaks and failure semantics, not only averages or one process's RSS.
- For a bug fix, add a stable reproducer before changing the implementation.

## Common Pitfalls

- `go test ./...` may succeed while official-source evidence verification was skipped because its environment variable was absent.
- A macOS success covers no `//go:build linux` file.
- `check.sh` may skip govulncheck when it is not installed; set `REQUIRE_GOVULNCHECK=1` for a complete report.
- `check-repository.sh` may skip Compose schema validation when Docker Compose is unavailable.
- `release-check.sh` is a final source preflight, not a normal inner-loop development command.
- A successful Go binary build does not prove pinned Docker assets, multiple architectures, or Linux capabilities.
