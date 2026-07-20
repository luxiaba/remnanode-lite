# Testing Guide

[Back to developer documentation](README.md) · [Contribution guide](../../CONTRIBUTING.md)

This guide describes Remnanode Lite's test layers, platform boundaries, and executable commands. The goal is not to repeat the most expensive gate after every edit. It is to match verification cost to change risk and to distinguish clearly between “passed on this workstation” and “verified for Linux/Panel production semantics.”

## Principles

- Run the owning package first during development, then expand the scope after a coherent batch.
- Changes to state, locks, goroutines, cancellation, or lifecycle behavior require race testing.
- Changes to officially observable behavior require the pinned-source contract tests.
- Only Linux tests can support claims about capabilities, netlink, nftables, process groups, or cgroups.
- Real Panel, real rw-core, the resource gate, and soak tests are candidate acceptance; unit tests do not replace them.
- Test data must not contain real Secrets, JWTs, certificates, private keys, node IPs, hostnames, or raw responses.

## Quick Selection

| Scenario | Command | Expected cost |
| --- | --- | --- |
| Change one Go package | `go test -count=1 ./internal/<package>` | Low |
| Change concurrency or shared state | `go test -race -count=1 ./internal/<package>` | Medium |
| Normal Go regression | `go test -count=1 ./...` | Medium |
| Go pre-commit gate | `bash scripts/check-go.sh` | Medium to high |
| Shell, Docker, workflow, or supply chain | `bash scripts/check-repository.sh` | Medium to high |
| Installer transaction | `bash scripts/test-install-ops.sh` | High |
| Complete repository gate | `REQUIRE_GOVULNCHECK=1 bash scripts/check.sh` | High |
| Linux network management | Two network-namespace integration tests | Linux/root |
| Low-memory budget | `scripts/test-low-memory.sh --rw-core ...` | Docker/real core |
| Official-versus-candidate behavior | `go run ./cmd/contract-probe ...` | Isolated acceptance environment |
| Formal release | `bash scripts/release-check.sh` | Frozen candidate only |

## Go Tests

### Targeted Package Loop

During an edit, run the nearest package first:

```bash
go test -count=1 ./internal/httpserver
go test -run '^TestName$' -count=1 ./internal/httpserver
go test -race -count=1 ./internal/httpserver
```

`-count=1` disables Go's test-result cache so that an earlier success is not mistaken for a result from the current implementation. Keep `-race` while diagnosing concurrency. Do not add sleeps to hide missing synchronization or cancellation propagation.

The Go race detector requires CGO and a working C compiler. If the build toolchain is missing, repair the development environment; a skipped race test is not a passing result.

### Normal Full Regression

```bash
go test -count=1 ./...
```

This runs every ordinary test that compiles on the current platform. Real integration tests in the repository remain protected by environment variables and call `Skip` unless explicitly enabled.

On macOS, files guarded by `//go:build linux` do not compile into the test, including Linux process, nftables, and netlink socket-destruction implementations. `go test ./...` on macOS is useful for fast regression but is not a complete Linux result. On Linux, the same command compiles Linux unit tests; network-namespace and real rw-core tests still require explicit activation.

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
6. The full race test suite.
7. `go vet ./...`.

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

`contract-source-check` reads the pinned commit object directly, disables replace refs, and does not trust the checkout, index, or `HEAD`. It verifies every evidence-blob digest and reconstructs the method/path manifest from the official `REST_API`, global prefix, route constants, and controller decorators. It also enumerates controllers and modules from the Git tree and binds the real Nest bootstrap, static imports, strict metadata, decorator ownership, module-registration reachability, and prefix exclusions for internal controllers.

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

The official Node's plugin `config` schema comes from a separate npm package and is not part of the pinned source repository. Recheck the current `@remnawave/node-plugins@0.4.5` tarball in an isolated temporary directory:

```bash
plugin_tgz="$(mktemp)"
trap 'rm -f "$plugin_tgz"' EXIT

curl --fail --location --silent --show-error \
  --proto '=https' --tlsv1.2 \
  https://registry.npmjs.org/@remnawave/node-plugins/-/node-plugins-0.4.5.tgz \
  -o "$plugin_tgz"

test "$(openssl dgst -sha1 "$plugin_tgz" | awk '{print $NF}')" = \
  3bfc3988278790ec40a93d6e6169f893c31bf62d
test "sha512-$(openssl dgst -sha512 -binary "$plugin_tgz" | openssl base64 -A)" = \
  'sha512-r9Lce/l/kHQATNhWbcutApFSJ5hH/Yu6Kv0+/qjpUDIEa1+DFb54Q8IwuvqWzxxbGkG9oO0cAeN4busBzz0a5Q=='

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
- An available actionlint executable; use `1.7.7` for CI parity.

Install the Go tools with:

```bash
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
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

`check.sh` combines the Go gate, repository gate, offline installer tests, and govulncheck. If `REQUIRE_GOVULNCHECK=1` is not set and govulncheck is unavailable, it skips the vulnerability scan. Release checks and reports that claim complete results must require it explicitly.

Even a successful run does not include Linux network namespaces, a real rw-core, Panel black-box tests, the resource gate, or a long-running soak. Do not describe it as completed production acceptance.

## Installer Tests

A change to installation, upgrade, uninstall, service definitions, OpenRC, or `install-env-helpers.sh` requires at least:

```bash
bash scripts/test-install-ops.sh
bash scripts/check-repository.sh
```

`test-install-ops.sh` uses temporary directories and command mocks to verify locking, permissions, path safety, Secret migration, atomic replacement, failure rollback, systemd/OpenRC state transitions, and uninstall isolation without changing real `/etc/remnanode` state or starting local services.

Some `flock` branches run only when the system provides `flock`. A macOS result cannot replace real Ubuntu/OpenRC installation acceptance. Installer behavior changes must still pass the Ubuntu CI job and the real-host release acceptance protocol for a frozen candidate.

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

## Low-Memory Resource Gate

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

Run this gate after changes to resource handling, request parsing, retained configuration, queues, logs, concurrency limits, or the rw-core lifecycle. Record the cgroup peak; the Go process RSS alone is not the gate metric. See the [resource budget](resource-budget.md) for the dated baseline.

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

The first target is the comparison baseline. By default, the probe runs only non-destructive safe routes. Start, stop, user mutations, connection cleanup, statistics reset, report draining, and nftables operations require both explicit `-routes` and `-allow-mutating`, and must run only in an isolated acceptance environment.

The probe never prints the JWT or raw response bodies. If a certificate contains only DNS names while the target uses an IP address, pass `-server-name`; there is no option to disable TLS verification.

## Release Gate

```bash
RELEASE_TAG=<tag> \
REMNANODE_OFFICIAL_SOURCE="$REMNANODE_OFFICIAL_SOURCE" \
REQUIRE_GOVULNCHECK=1 \
  bash scripts/release-check.sh
```

This script is only for a frozen release candidate with real acceptance evidence. It requires a clean worktree, finalized Release notes and `CHANGELOG.md`, a valid evidence manifest, and valid candidate ancestry, then runs the complete repository checks. Failure on an ordinary development branch that lacks these materials is expected. Do not fabricate evidence, weaken the checks, or advance release state merely to make it pass.

See the [versioning policy](../versioning.md) for tag, version, and `latest` semantics, and the [release process](../release.md) for candidate freeze and release steps.

## Selecting Tests by Change

| Change | Minimum verification | Expanded verification |
| --- | --- | --- |
| Documentation only | `go run ./cmd/docs-check`, `git diff --check`; manually verify command facts | Run the relevant script for release/deployment documentation |
| Ordinary Go logic | Owning package tests | `bash scripts/check-go.sh` |
| Locks, state, workers, shutdown | Owning package race test | Full race suite and related lifecycle tests |
| HTTP/API/schema | `nodeapi`, `httpserver`, `contract` | Pinned-source contract tests and black-box comparison |
| Xray lifecycle | `xray` and `httpserver` race tests | Real rw-core, Panel, and resource gate |
| Users and statistics | `nodehandler`, `stats`, `xrayrpc` | Contract response tests and Panel differential testing |
| Plugin pure logic | `plugin` race test | HTTP lifecycle interleaving tests |
| nftables/socket destruction | Corresponding Linux unit test | Both namespace integration tests |
| Configuration/Secret/JWT | `config`, `secret`, `auth`, server security | Installer Secret flow |
| Shell/service | `bash scripts/check-repository.sh`, `bash scripts/test-install-ops.sh` | Real systemd/OpenRC |
| Docker/Compose | `bash scripts/test-docker-packaging.sh` | Multi-architecture image build and candidate deployment |
| Dependency or downloadable asset | `go mod tidy -diff`, supply-chain checks, govulncheck | Dual-architecture build, SBOM, and attestation |
| Project version | `bash scripts/check-version.sh` | Release preflight |
| Official contract upgrade | Full contract and pinned-source tests | Black-box all registered routes and complete Panel flow |
| Protobuf wire | `scripts/generate-protobuf.sh --check`, `go test ./internal/xrayrpc` | Real rw-core and golden-wire regression |
| Resource limit | Related unit/race tests | `test-low-memory.sh` and soak |

“Minimum verification” is for the development loop, not necessarily the entire pull-request requirement. For a cross-component change, take the union of the applicable rows.

## CI Mapping

The required gate in `.github/workflows/ci.yml` aggregates four parallel jobs:

| CI job | Primary command |
| --- | --- |
| `go` | Pinned official source plus `scripts/check-go.sh` |
| `repository` | Install pinned static tools plus `scripts/check-repository.sh` |
| `installer` | `scripts/test-install-ops.sh` |
| `netadmin` | Both Linux namespace integration tests |
| `gate` | Requires every job above to report success |

The container workflow is path-filtered, so not every pull request has a container check. When container inputs change on `main`, it builds and attests a manifest before publishing an immutable candidate tag. The tag-triggered release workflow promotes the same digest bound by the acceptance manifest. Do not treat a path-filtered “not run” as failure, and do not make an optional container job a universally required check that can never appear.

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
- `release-check.sh` is not a normal development command and is expected to fail before candidate evidence exists.
- A successful Go binary build does not prove pinned Docker assets, multiple architectures, or Linux capabilities.
