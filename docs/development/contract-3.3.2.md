# Remnawave Node 3.3.2 Behavioral Contract Baseline

[Back to the development guide](README.md) | [Previous 3.2.2 baseline](contract-3.2.2.md)

This document records the compatibility baseline for official Remnawave Node
`3.3.2`. Earlier version documents remain immutable historical records. This
page describes the reviewed `3.2.2..3.3.2` delta, the equivalent behavior in
Remnanode Lite, and the bounded implementation differences retained by the Go
runtime.

## Pinned Evidence

- Official repository: `https://github.com/remnawave/node.git`
- Version: `3.3.2`
- Commit: `afdfa2d837118efd95c317700e60e9429a169b48`
- Previous baseline: `3.2.2` at
  `2c532c4e33bf5864e9867a7bdc36245cc1057eb1`
- Official contract package: `@remnawave/node-contract@3.2.3`
- External plugin schema: `@remnawave/node-plugins@0.7.3`
- Official GeoCheck asset: `remnawave/geocheck@v0.3.0`, commit
  `50e084bb3ed34b55fc6839fe0dc4bafd9fe275fc`

The reviewed plugin archive is `node-plugins-0.7.3.tgz`, SHA-1
`d58cc34d15838d6ac543c112ac65265d6189745e`, with integrity value
`sha512-y1+dIrZVENchojkBJHC5KHocTTDl/xeCdIvbYgoTWYZ5pWuIkyoFYm/fGUA//W3DQ6eAlAKkg1HC24MeKAoIvA==`.
The schema remains external to the Node repository and is reviewed separately
as described in
[External Plugin Schema Evidence](testing.md#external-plugin-schema-evidence).

`internal/contract/official-source-manifest.json` pins the official source
identity and every reviewed blob. The 3.3.2 manifest contains 85 evidence
files and 27 machine-extracted public routes. The new public route is
`POST /node/stats/get-geocheck`; the previous 26 methods and paths remain.

## Alignment Status

| Area | Status for this baseline | Compatibility meaning |
| --- | --- | --- |
| Public routes and responses | Implemented and source-pinned | All previous routes remain aligned; GeoCheck adds the official success response and `A018` failure envelope. |
| Derived SNI gate | Implemented | TLS presents the node certificate only when ClientHello carries the server name derived from the Panel Secret; mTLS and JWT remain mandatory. |
| Secret integrity | Implemented with fail-closed validation | Invalid or inconsistent key material and an invalid CA validity period are rejected before the public listener starts. |
| Torrent `rulePlacement` | Implemented | The optional plugin value selects the Torrent Blocker rule position with official default and bounded schema behavior. |
| GeoCheck runtime | Official fixed binary behind bounded execution | Docker and Native releases package official GeoCheck `v0.3.0`; the Node serializes execution and limits time and output. |
| Core lifecycle | Equivalent implementation | An early rw-core exit is reported during readiness; existing process-group cleanup remains bounded. |

Implementation and review are not release evidence by themselves. Publication
still requires the complete tests, immutable candidate, real Panel connection,
and representative proxy-traffic checks in the release guide.

## Reviewed Upstream Delta

The compatibility-relevant changes from official 3.2.2 are:

- a derived-SNI check at the TLS certificate-selection boundary;
- startup integrity checks for the decoded Panel Secret;
- `POST /node/stats/get-geocheck` and error code `A018`;
- official GeoCheck `v0.3.0` in the container;
- Torrent Blocker `rulePlacement` in plugin schema `0.7.3`; and
- explicit detection when rw-core exits before its API becomes ready.

Framework, build, presentation, and source-organization changes do not alter
the other public routes. Remnanode Lite implements observable behavior through
its existing Go ownership boundaries rather than copying NestJS or s6 internals.

## Derived SNI and Secret Integrity

The expected TLS server name is deterministically derived from the canonical
JWT public-key and CA-certificate PEM bodies using HKDF-SHA256 with info
`rw-v1`. A missing or different ClientHello SNI is rejected without presenting
the node certificate. This is an additional pre-authentication gate, not a
replacement for TLS 1.3, client-certificate verification, or bearer JWT
validation.

At startup, the complete decoded `SECRET_KEY` is checked before listener
creation. Remnanode Lite verifies that:

- the CA and node certificates parse, and the CA is currently valid;
- the CA signature verifies with its own public key;
- the node certificate signature verifies with that CA;
- the node private key parses and matches the node certificate; and
- the JWT public key parses.

These checks do not rotate, repair, or log secret material. Failure is explicit
and startup stops. As in the official startup check, the node certificate's
validity period is left to the normal TLS verification path rather than adding
a stricter Lite-only startup rule.

## GeoCheck Route and Runtime Boundary

`POST /node/stats/get-geocheck` accepts an object with optional string `ip` and
`interface` fields. An `ip` value takes precedence and must parse as IPv4 or
IPv6 after trimming. Otherwise a non-empty interface is passed through; with
neither value GeoCheck uses the default route. Normal request unknown-field
stripping, mTLS, SNI, JWT, body-size, and handler-admission rules still apply.

The Node executes the fixed `geocheck` asset with `--json --svg-base64 --quiet`
and an optional `--interface` binding. Only one run is admitted at a time. Each
run has a 45-second deadline and separate 32 MiB stdout and stderr limits. A
success must be one JSON document with non-empty `image.data`; that document is
returned in the normal `response` envelope. Invalid input, concurrent runs,
timeouts, execution errors, oversized output, invalid JSON, and a missing image
use the official `A018` application error family.

The release lock records exact archive and extracted-binary digests for Linux
`amd64` and `arm64`, the upstream commit, and the MIT license. The executable is
the unmodified official GeoCheck `v0.3.0` binary. Upstream built that fixed
binary with Go `1.26.5`; this baseline does not claim its residual toolchain
vulnerabilities are removed. The authenticated route, single-run gate,
45-second deadline, and 32 MiB output limits reduce exposure but do not rewrite
or patch the executable. Lite's existing least-privilege container and Native
service profiles do not grant `CAP_NET_RAW`; GeoCheck therefore uses its
upstream TCP fallback instead of raw ICMP or hop-by-hop probing. The report
remains valid, but may contain less network-diagnostic detail than the official
container.

## Torrent Blocker Rule Placement

Plugin schema `0.7.3` adds optional `torrentBlocker.rulePlacement` as a number
from `0` through `1000`. Omission or zero preserves the default insertion
position. A positive integer selects the requested routing-rule position,
clamped to the available rule list; non-integer values retain the default.
The placement is part of the plugin plan and active snapshot, so a changed
value participates in the same validate, apply, and commit transaction as the
rest of Torrent Blocker configuration.

Existing `includeRuleTags`, local report collection, connection blocking, and
best-effort report webhook behavior are unchanged.

## Lifecycle and Accepted Differences

If rw-core exits while startup waits for its gRPC API, Remnanode Lite's existing
process watcher already converges immediately on the same early-exit failure
instead of waiting for the whole readiness deadline. It still owns the complete
child process group and retains its bounded SIGINT/SIGKILL cleanup and
state-commit rules.

All accepted differences in the 3.2.2 baseline remain unless this document
overrides them. Remnanode Lite remains one Go process, keeps its explicit
request and resource limits, and does not reproduce the official Node.js, s6,
or console-presentation structure.

## Verification

Source and focused verification:

```bash
export REMNANODE_OFFICIAL_SOURCE=/absolute/path/to/remnawave-node-3.3.2
go run ./cmd/contract-source-check -source "$REMNANODE_OFFICIAL_SOURCE"
go test -count=1 ./internal/contract ./internal/httpserver ./internal/secret ./internal/geocheck ./internal/plugin ./internal/xray
go test -race ./internal/httpserver ./internal/geocheck ./internal/plugin ./internal/xray
```

The complete release gate must also verify both packaged architectures and the
GeoCheck asset digests, SNI rejection and acceptance with a target Panel,
GeoCheck success and bounded failure, Torrent rule ordering, early Core exit,
normal Core startup, and real proxy traffic. Host details, logs, generated
GeoCheck reports, traffic records, and server data remain outside the repository.
