# Development Guide

This guide is for maintainers approaching Remnanode Lite for the first time. It is designed to get you from environment setup to code navigation, implementation, and local verification without starting a real node or preparing a Panel and `SECRET_KEY`.

Linux is the production target, but routine Go development works on Linux or macOS. Any conclusion involving nftables, socket destruction, process groups, or cgroups must still be verified on Linux.

## The 15-Minute Onboarding Path

You do not need to read every long-form document before making a first change:

1. Read the goals, boundaries, and current status in [Project Scope and Goals](../project.md).
2. Prepare the toolchain on this page and complete the first verification.
3. Use the code map below to find the relevant package, then read its implementation and tests together.
4. In the [architecture guide](../architecture.md), read only the data-flow, ownership, and concurrency sections for the component you will change.
5. Choose the smallest sufficient verification from the [testing guide](testing.md). Read the [contribution guide](../../CONTRIBUTING.md) before preparing a commit.

Go deeper only when the change requires it: read the [current contract baseline](contract-2.8.0.md) for Panel-visible behavior, the [versioning policy](../versioning.md) for versions or images, and the [release guide](../release.md) for a formal release. The [documentation index](../README.md) provides complete role-based navigation.

## Development Environment

### Required Tools

- Git.
- Bash 4 or later; Bash 5 is recommended for CI parity and installer work. The Bash 3.2 bundled with macOS does not support syntax used by the scripts, including `${var,,}`.
- The exact Go version selected by the `toolchain` directive in `go.mod`, currently Go `1.26.5`.
- `gofmt`, included with the Go toolchain.
- A C compiler and working CGO environment. `check-go.sh` always runs the race detector. Install Xcode Command Line Tools on macOS or the appropriate build toolchain on Linux.
- GNU `timeout`, called directly by supply-chain and installer checks. On macOS, install Homebrew `coreutils` and add `$(brew --prefix coreutils)/libexec/gnubin` to `PATH`.

Use the same Go patch release as CI whenever possible. A normal `go test` may allow Go to download another toolchain automatically, but release builds set `GOTOOLCHAIN=local` and reject a mismatched local version.

The following tools are needed only for their corresponding checks:

- ShellCheck `0.11.0` for shell and OpenRC static analysis.
- actionlint `1.7.7` for GitHub Actions static analysis.
- govulncheck `1.1.4` for reachable Go vulnerability scanning.
- Docker Engine and Docker Compose v2 for Compose validation, image builds, and resource testing.
- Linux `iproute2`, `nftables`, `unshare`, and root privileges for network-management integration tests.

The repository intentionally has no `Makefile`. Scripts under `scripts/` are the shared local and CI entry points, and documentation calls them directly rather than maintaining another layer of similarly named wrappers with potentially different behavior.

`scripts/install-ci-checks.sh` is specific to GitHub-hosted runners. It requires `GITHUB_PATH`, `RUNNER_TEMP`, and a Linux toolchain; do not run it as a local setup script. Install actionlint and govulncheck at the versions pinned by CI:

```bash
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
export PATH="$(go env GOPATH)/bin:$PATH"
```

If you use a custom `GOBIN`, add it to `PATH` and confirm that the scripts can find the tools:

```bash
command -v actionlint govulncheck timeout
```

Install ShellCheck `0.11.0` from the system package manager or its official release archive. Check the environment before continuing:

```bash
go version
shellcheck --version
actionlint -version
govulncheck -version
```

### Getting the Code

Maintainers start from the stable development branch:

```bash
git clone git@github.com:luxiaba/remnanode-lite.git
cd remnanode-lite
git switch dev
go mod download
```

Create a short-lived branch for each focused change. Do not develop directly on `main`:

```bash
git switch -c fix/short-description
```

Branch and pull-request rules are documented in the [contribution guide](../../CONTRIBUTING.md). The official `remnawave/node` repository is behavioral evidence, not this repository's Git upstream. Do not merge, rebase, or otherwise use it as a source-history synchronization target.

### First Verification

Normal tests and builds do not require `.env`, Panel, `SECRET_KEY`, or a local rw-core:

```bash
go test -count=1 ./...
mkdir -p bin
go build -trimpath -o bin/remnanode-lite ./cmd/remnanode-lite
./bin/remnanode-lite version
```

This binary is suitable for a CLI smoke test. Running the full daemon also requires a valid Panel Secret, mTLS material, rw-core, Linux capabilities, and a supported host-network environment. Starting the daemon directly on macOS is not integration acceptance.

## Pinned Official Contract Source

Most Go tests run without official source. A change to the Panel API, request/response schemas, error semantics, or other official behavioral evidence should also prepare the official source repository for the current contract version:

```bash
contract_version="$(tr -d '[:space:]' < internal/version/contract.version)"
official_dir="../remnawave-node-official-${contract_version}"

git clone --depth 1 --branch "$contract_version" \
  https://github.com/remnawave/node.git "$official_dir"

export REMNANODE_OFFICIAL_SOURCE="$(cd "$official_dir" && pwd)"
go run ./cmd/contract-source-check
go test -count=1 ./internal/contract
```

`cmd/contract-source-check` does not trust files in the checkout. It reconstructs `official-source-manifest.json` directly from the pinned commit's Git objects, verifies the package name and version plus the SHA-256 of every evidence blob, and independently extracts methods and paths from `REST_API` and NestJS controller decorators. It also verifies controller and module inventory from the Git tree and registration reachability from `AppModule`.

A dirty worktree, staged changes, replace refs, or a different `HEAD` cannot contaminate the evidence. A missing pinned object, a changed content digest, or manual drift in the local route inventory fails verification. The tool does not attempt to translate all Zod schemas automatically and does not download the external plugin schema.

Keep the official repository outside this repository. `.official-source/` is ignored if it is created here accidentally, but it still must not be committed.

## Code Map

### Executable Entry Points

| Path | Responsibility |
| --- | --- |
| `cmd/remnanode-lite` | CLI, dependency composition, HTTPS and internal-socket startup, signals, and bounded shutdown |
| `cmd/contract-probe` | mTLS black-box contract comparison between an official node and a candidate |
| `cmd/contract-source-check` | Reconstruct and verify the source-evidence manifest from pinned official Git objects |
| `cmd/asn-builder` | Build a compact, read-only prefix database from the pinned ASN source |
| `cmd/release-evidence-check` | Validate the release acceptance manifest, Git ancestry, and release-asset digests |
| `cmd/docs-check` | Validate Markdown structure, relative links, anchors, and entry-point reachability |

### Runtime Packages

| Path | Responsibility and ownership |
| --- | --- |
| `internal/config` | Bounded parsing of configuration files and environment overrides; does not orchestrate services |
| `internal/secret`, `internal/auth` | Secret decoding, mTLS material, and JWT verification |
| `internal/bodylimit` | Compressed request decoding plus body and decompression resource limits |
| `internal/httpserver` | TLS, authentication, routes, transport errors, capacity limits, and the cross-component lifecycle gate |
| `internal/nodeapi` | Request DTOs, unions, JSON decoding, and official error models |
| `internal/stats` | Statistics use cases; does not own the rw-core process |
| `internal/nodehandler` | User add/remove, query, and connection-cleanup use cases |
| `internal/plugin` | Plugin snapshots, plans, torrent reports, nftables, and the operation gate |
| `internal/xray` | Sole owner of the rw-core process, configuration, hashes, logs, and lifecycle |
| `internal/xrayrpc` | Minimal protobuf/gRPC client for rw-core |
| `internal/xrayrpc/wire` | Generated minimal protobuf wire types used by `internal/xrayrpc` |
| `internal/unixconfig` | Internal Unix-socket service through which rw-core reads configuration and sends webhooks |
| `internal/connections`, `internal/netadmin` | User/IP connection resolution and Linux socket destruction |
| `internal/system`, `internal/asn` | System metrics, network monitoring, and compact ASN lookup |
| `internal/contract` | Executable behavioral contract and differential semantics for the pinned official version |
| `internal/version` | Project version and official contract version; see the versioning policy for their distinct meanings |

### Engineering and Delivery Paths

| Path | Responsibility |
| --- | --- |
| `.github/workflows/ci.yml` | Required Go, repository, installer, and Linux network-management CI gate |
| `.github/workflows/container.yml` | Candidate multi-architecture image build, attestation, and immutable candidate tags |
| `.github/workflows/release.yml` | Formal Release assets and promotion of an accepted image digest |
| `.github/workflows/contract-sync.yml`, `.github/workflows/security.yml` | Official-version monitoring and scheduled security checks |
| `scripts/check*.sh` | Stable Go, repository, supply-chain, and complete-gate entry points |
| `scripts/install*.sh`, `scripts/upgrade.sh`, `scripts/uninstall.sh` | Native installation, asset transactions, upgrade rollback, and uninstall |
| `deploy/` | systemd/OpenRC service definitions, native `node.env`, and the production single-file Compose template |
| `compose.yaml`, `compose.build.yaml` | GHCR runtime configuration and local source-build override |
| `docs/development/acceptance/` | Versioned and redacted release acceptance records whose schema and digests are machine-validated, created only for a frozen candidate |
| `docs/releases/` | GitHub Release notes paired with Git tags, created only for a frozen candidate |
| `Dockerfile` | Dual-architecture Node build, pinned rw-core/geo/ASN assets, and minimal runtime image |

The primary request path is:

```text
Panel
  -> HTTPS / mTLS / JWT
  -> httpserver (routing, limits, and lifecycle gate)
  -> nodeapi (decode and validation)
  -> stats / nodehandler / plugin
  -> xray.Manager
  -> xrayrpc gRPC
  -> rw-core
```

See the [architecture guide](../architecture.md) for complete state ownership, internal webhook, and shutdown flows.

## Daily Development Loop

1. Identify the owning component and whether the change affects official observable behavior.
2. Read the implementation and tests in that package, then run the nearest package test first.
3. Change the code and run `gofmt`; do not reformat unrelated files.
4. Repeat the package tests. Changes to concurrency or shared state require at least the package race test.
5. Use the change matrix in the [testing guide](testing.md) to add contract, shell, Docker, or Linux verification where needed.
6. Review the diff, documentation, and `CHANGELOG.md`, then create a logically complete checkpoint.

A typical fast loop is:

```bash
go test -count=1 ./internal/xray
gofmt -w internal/xray/changed_file.go internal/xray/changed_file_test.go
go test -race -count=1 ./internal/xray
git diff --check
git diff
```

Do not run the release gate after every line-level edit. Test cost should match
risk. The complete repository check belongs after a coherent batch or before
opening a pull request.

The `v2.8.0` M8 gate requires the frozen digest to pass the `amd64` Docker
production smoke with a real Panel and real proxy traffic before publication.
`arm64-production-runtime`,
`native-systemd-install`, `native-openrc-install`, a candidate
50,000-user load, 24-hour soak, and fault/rollback injection are expanded
validation that is explicitly deferred and non-blocking for this release.

## Common Change Paths

| Change | Usually touches | Boundary to preserve |
| --- | --- | --- |
| Add or modify a `/node` route | `internal/contract`, `internal/httpserver/node_routes.go`, `internal/nodeapi`, corresponding service | Method/path, request, response, errors, and side effects must move together |
| Xray start/stop or configuration update | `internal/xray/lifecycle.go`, `manager.go`, process files, `apiconfig.go` | Manager is the sole process owner; retain cancellation, timeout, and shutdown order |
| User hot update | `internal/nodehandler`, `internal/xray/handler.go`, `internal/xrayrpc` | The gRPC result and hash commit must retain their transaction semantics under one process lease |
| Statistics semantics | `internal/stats`, `internal/xrayrpc/stats.go`, HTTP route | Check reset behavior, missing values, and official response schemas |
| Plugin or nftables | `internal/plugin`, `internal/connections`, `internal/netadmin` | Lifecycle lease precedes the plugin operation gate; Linux integration tests are required |
| Configuration, Secret, or authentication | `internal/config`, `internal/secret`, `internal/auth`, `internal/httpserver` | Bound inputs, preserve safe file handling, and keep Secrets out of logs |
| Linux system capability | `*_linux.go` and corresponding `*_stub.go` | Non-Linux builds must compile; Linux behavior must be tested on Linux |
| Docker image | `Dockerfile`, `compose*.yaml`, `.dockerignore`, container workflow | Pinned asset digests, multiple architectures, resource limits, and ephemeral logs |
| Install, upgrade, or uninstall | `scripts/`, `deploy/` | Locking, atomic replacement, rollback, permissions, and systemd/OpenRC symmetry |
| Project version | `internal/version`, installers, Compose, release workflow | Do not recouple the project version to the contract version |
| Official contract upgrade | `internal/version/contract.version`, `internal/contract`, source manifest, pinned CI ref, contract documentation | Pin the source commit, extract and review the diff, then implement; never infer complete Zod equivalence automatically |

The minimum test set for each category is in [Selecting Tests by Change](testing.md#selecting-tests-by-change).

## Engineering Constraints

### Compatibility Comes First

The external API is not a free design surface. Do not change status codes, JSON shapes, missing-value behavior, error categories, or side effects merely because another choice seems cleaner. Establish evidence from pinned official source and black-box behavior before updating the executable contract and implementation.

### Resources Must Be Bounded

The project targets a whole machine with `512 MiB RAM / 1 vCPU / 2 GB disk`. Every new request body, buffer, log, queue, cache, goroutine, concurrency slot, or external-command output needs a clear bound and failure behavior. Do not retain a complete Xray configuration indefinitely for debugging convenience.

### State Has One Owner

- `xray.Manager` owns the rw-core process and Xray lifecycle.
- `plugin.Service` and `plugin.State` own plugin state and firewall plans.
- `httpserver` orders operations that cross Xray and Plugin.

Do not bypass these entry points from a new handler to mutate shared state, and do not introduce a reverse lock order.

### Cancellation and Shutdown Are Normal Paths

Potentially blocking I/O, external commands, gRPC calls, and gate waits must accept and propagate `context.Context`. A new background worker must document who starts it, who stops it, how a full queue fails, and how shutdown waits for it.

### Platform Differences Must Be Explicit

Linux-only implementations use build tags and provide compiling non-Linux stubs. macOS is useful for fast development, but neither success nor an unavailable response from a stub proves Linux capability, netlink, nftables, or process-group behavior.

### Generated Files and Pinned Assets

`internal/xrayrpc/wire/wire.pb.go` is generated; do not edit it by hand. The repository uses `protoc 35.1` and `protoc-gen-go v1.36.11`. Regenerate through the pinned entry point:

```bash
scripts/generate-protobuf.sh
go test -count=1 ./internal/xrayrpc
scripts/generate-protobuf.sh --check
```

The script requires `protoc --version` to return exactly `libprotoc 35.1` and installs the pinned Go plugin in an isolated temporary directory. `--check` regenerates and compares the result byte for byte. A wire-schema change must also prove that golden wire encoding, Handler/Stats behavior, and real rw-core integration have not drifted unexpectedly.

Docker base images, GitHub Actions, rw-core, the ASN source, and downloadable assets are all pinned by version or digest. An upgrade must update the verification scripts and provenance documentation together; changing only a URL is not sufficient.

## Next Steps

After the first normal test run, continue according to the change:

- API and behavioral alignment: [current contract baseline](contract-2.8.0.md).
- Concurrency and state ownership: [architecture guide](../architecture.md).
- Local and CI verification: [testing guide](testing.md).
- Commits and review: [contribution guide](../../CONTRIBUTING.md).
- Versioning or release: [versioning policy](../versioning.md) and [release process](../release.md).
