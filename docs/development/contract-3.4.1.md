# Remnawave Node 3.4.1 Behavioral Contract Baseline

[Back to the development guide](README.md) | [Previous 3.3.2 baseline](contract-3.3.2.md)

This document records the compatibility baseline for official Remnawave Node
`3.4.1`. Earlier version documents remain immutable historical records. This
page describes the reviewed `3.3.2..3.4.1` delta, the equivalent behavior in
Remnanode Lite, and the bounded implementation differences retained by the Go
runtime.

## Pinned Evidence

- Official repository: `https://github.com/remnawave/node.git`
- Version: `3.4.1`
- Commit: `44912631321664dbd5822e9bf8d96766ccff7c93`
- Previous baseline: `3.3.2` at
  `afdfa2d837118efd95c317700e60e9429a169b48`
- Official contract package: `@remnawave/node-contract@3.4.1`
- External plugin schema package: `@remnawave/node-plugins@0.8.2`
- Official rw-core: `v26.7.28` (unchanged)
- Official GeoCheck asset: `remnawave/geocheck@v0.3.0` (unchanged)

The reviewed plugin archive is `node-plugins-0.8.2.tgz`, SHA-1
`9588288f9190b73b2ce868845d4248c98eadc25f`, with integrity value
`sha512-/klo/XH4imZ2cupLavj4++S+hHgVA8uzhVgpQdC0y9kzUtVE168d7brcYEdxB1UGw49LsER+UDYplcyTSvV5QQ==`.
Its extracted `node-plugins.schema.js` has SHA-256
`e096eba57a8ce1499a0e117bf5b9dfd7f324a9a6fc455066fcb31d5c86a91d21`
and is byte-for-byte identical to the reviewed 0.7.3 schema. The package and
Zod versions changed, but the accepted plugin configuration did not.

`internal/contract/official-source-manifest.json` pins the official source
identity and every reviewed blob. The 3.4.1 manifest contains 88 evidence
files and 25 machine-extracted public routes.

## Alignment Status

| Area | Status for this baseline | Compatibility meaning |
| --- | --- | --- |
| Public routes and responses | Implemented and source-pinned | The two retired inbound-user query routes are no longer exposed; the remaining 25 methods, paths, schemas, and response envelopes stay aligned. |
| SNI verification | Implemented as an opt-in gate | `SNI_VERIFICATION=false` is the official default. Enabling it restores the exact derived-SNI certificate-selection gate; TLS 1.3, mTLS, and JWT remain mandatory in both modes. |
| User replacement cleanup | Implemented | When `add-user` carries `prevVlessUuid`, the Node reads the user's IPs before removal and attempts to terminate those connections before adding the replacement credentials. |
| nftables runtime options | Implemented | Ingress-drop logging defaults on; reply-direction acceptance defaults off and can be enabled explicitly. |
| Plugin schema | Re-audited with no schema delta | Package 0.8.2 accepts the same plugin document as 0.7.3. |
| Framework and container refactors | Not copied | Result helpers, typed Nest configuration, Node.js, S6, ASN extraction, and native npm dependency layout do not define the Go architecture or Panel wire contract. |

Implementation and review are not release evidence by themselves. Publication
still requires the complete tests, immutable candidate, real Panel connection,
and representative proxy-traffic checks in the release guide.

## Reviewed Upstream Delta

The compatibility-relevant changes from official 3.3.2 are:

- removal of `POST /node/handler/get-inbound-users` and
  `POST /node/handler/get-inbound-users-count`;
- optional derived-SNI verification through `SNI_VERIFICATION`, off by default;
- `NFTABLES_LOGGING` with official default `true`;
- `NFTABLES_ACCEPT_REPLY_TRAFFIC` with official default `false`; and
- best-effort termination of a user's previous connections when `add-user`
  replaces credentials identified by `prevVlessUuid`.

The official error/result helper rewrite changes TypeScript typing and internal
control flow, not successful response envelopes, application error codes, or
HTTP status behavior. Remnanode Lite retains its existing explicit Go result
and ownership boundaries.

## Public Route Retirement

The 3.4.1 contract package removes both inbound-user query commands and their
route constants. Remnanode Lite therefore rejects those method/path pairs at
the same pre-handler known-route boundary used for every unknown Node route.
The underlying rw-core gRPC methods remain available internally for bounded
runtime and resource tests; they are no longer part of the Panel-to-Node HTTP
surface.

This reduces the registered route inventory from 27 to 25. Request budgets,
handler admission, source evidence, response-shape tests, and the differential
probe inventory all use that same reviewed route set.

## Optional Derived SNI

The official 3.3.2 line required the derived server name for every TLS
ClientHello. Official 3.4.1 makes that additional gate optional and disables it
by default. Remnanode Lite follows the same behavior:

- with `SNI_VERIFICATION=false`, the Node presents its configured certificate
  normally and still requires a Panel client certificate plus a valid bearer
  JWT;
- with `SNI_VERIFICATION=true`, the Node presents the certificate only when the
  ClientHello server name exactly matches the value derived from the canonical
  CA and JWT public-key PEM bodies; and
- TLS remains limited to TLS 1.3 in both modes.

The switch changes compatibility at certificate selection only. It does not
disable Secret integrity validation, mTLS, JWT validation, known-route checks,
or request bounds.

## User Replacement Connection Cleanup

When `add-user` includes `prevVlessUuid`, the official Node reads the user's
current IP list before removing old inbound credentials, publishes a connection
drop after removal, and then adds the replacement credentials. The connection
drop is best-effort and does not change the successful HTTP response.

Remnanode Lite performs the same observable sequence under its existing Xray
process lease and serialized mutation gate:

1. read IP statistics without resetting them;
2. remove old user/hash state from the selected inbounds;
3. attempt one bounded, deduplicated socket-destruction batch; and
4. add replacement credentials and commit only successful inbound mutations.

If old-user removal fails, Lite retains its existing retry-safe behavior and
does not drop connections or add the replacement. If the optional IP lookup or
socket destruction fails while the request remains live, it logs a bounded
diagnostic and continues the credential replacement, matching the official
best-effort cleanup outcome.

## nftables Runtime Options

`NFTABLES_LOGGING=true` adds a kernel log expression immediately before each
ingress and Torrent Blocker drop. Egress address and port filters do not log.
Set it to `false` on a busy host when blocked traffic would create unacceptable
kernel-log or journald load.

`NFTABLES_ACCEPT_REPLY_TRAFFIC=true` inserts `ct direction reply accept` before
the ingress block-set rules in both IPv4 and IPv6 input/forward chains. This
preserves replies to connections initiated by the host while leaving inbound
original-direction and egress filtering unchanged. It also makes conntrack
availability a table-creation dependency. The official default remains
`false`, preserving the previous stateless ingress behavior unless an operator
opts in.

The options are read once at process startup and take effect whenever the
owned tables are initialized or recreated. They do not modify firewalld or any
table outside the Remnanode Lite-owned IPv4 and IPv6 tables.

Lite retains separate TCP/UDP egress-port rules and does not copy the npm
native addon's named packet counters. No Panel route reads those counters; the
allow, drop, logging, reply-direction, IPv4, and IPv6 behavior remains aligned
without importing the official implementation's internal table layout.

## Retained Runtime Assets and Accepted Differences

Official 3.4.1 still packages rw-core `v26.7.28` and GeoCheck `v0.3.0`, so the
Lite runtime asset lock does not change. The official container also updates
Node.js, S6 Overlay, ASN decompression, and native npm dependencies. Those are
delivery details of the TypeScript implementation and are not copied into the
Go binary or its independently pinned Native/Docker assets.

All accepted differences in the 3.3.2 baseline remain unless this document
overrides them. Remnanode Lite remains one Go process, keeps explicit request
and resource limits, and retains its transactional Xray/plugin ownership.

## Verification

Source and focused verification:

```bash
export REMNANODE_OFFICIAL_SOURCE=/absolute/path/to/remnawave-node-3.4.1
go run ./cmd/contract-source-check -source "$REMNANODE_OFFICIAL_SOURCE"
go test -count=1 ./internal/contract ./internal/httpserver ./internal/config ./internal/nodehandler ./internal/plugin
go test -race ./internal/httpserver ./internal/nodehandler ./internal/plugin
```

The complete release gate must also verify the Linux nftables namespace tests
with both optional rule shapes, exact-SNI enabled and disabled against the
target Panel, user credential replacement with live traffic, normal rw-core
startup, and real proxy traffic through the immutable candidate. Host details,
logs, traffic records, and server data remain outside the repository.
