# Remnawave Node 3.2.2 Behavioral Contract Baseline

[Back to the development guide](README.md) | [Previous 3.0.0 baseline](contract-3.0.0.md)

This document records the compatibility baseline for official Remnawave Node
`3.2.2`. The previous version documents remain immutable historical records.
This page describes the reviewed `3.0.0..3.2.2` delta, the equivalent behavior
implemented by Remnanode Lite, and the implementation boundaries that remain
deliberately different from the official Node.js and s6 layout.

## Pinned Evidence

- Official repository: `https://github.com/remnawave/node.git`
- Version: `3.2.2`
- Commit: `2c532c4e33bf5864e9867a7bdc36245cc1057eb1`
- Previous baseline: `3.0.0` at
  `46fc5d2d736ff60f6c6a9a56e2661acb95d3f559`
- Official contract package: `@remnawave/node-contract@3.2.0`
- External plugin schema: `@remnawave/node-plugins@0.6.3`

The reviewed plugin archive is `node-plugins-0.6.3.tgz`, SHA-1
`9562fe8a6d90ec646023211ee7487cbede91fcdc`, with integrity value
`sha512-WBuY6PeSe8Sm/3mPWHPACDjOPrLE/bHwzQZiUYwF8L+Ww3q8f+5gVdRHZY+V+c+pm5ozhxRxrzyphgKg3jb7hw==`.
The plugin schema is external to the Node Git repository and must be reviewed
separately as described in
[External Plugin Schema Evidence](testing.md#external-plugin-schema-evidence).

`internal/contract/official-source-manifest.json` pins the official source
identity and every reviewed blob. The 3.2.2 manifest contains 75 evidence files
and the same 26 machine-extracted public routes as 3.0.0. The extractor has one
new, exact fail-closed allowance for the computed imports expression in the
official `IntegrationsModule`; a different source path, callee, or expression
is rejected.

## Alignment Status

| Area | Status for this baseline | Compatibility meaning |
| --- | --- | --- |
| Public routes and responses | Verified unchanged | All 26 public methods, paths, response envelopes, and response fields remain aligned. |
| Start optional internals | Implemented | `metadata` and `integrations` are accepted and validated with the official optional/null boundaries. |
| Integration execution | No stock implementation exists | Official 3.2.2 ships no `*.integration.ts` provider; Remnanode Lite accepts the transport fields but does not add a separate extension runtime. |
| Torrent report webhook | Implemented and independently reviewed | Blocking and local collection remain authoritative even when external delivery fails. |
| Core lifecycle and version reporting | Equivalent implementation | The Go process owner already provides bounded SIGINT/SIGKILL escalation and executable-based version probing. |
| Panel-selected Core and GeoData | Implemented and independently reviewed | Downloads use a derived persistent cache while bundled, release-locked assets remain the fallback. |

Implementation and code review are not release evidence by themselves. This
baseline still requires the combined tests, immutable candidate, real Panel
connection, and representative proxy-traffic checks described below.

## Reviewed Upstream Delta

Official 3.2.2 contains 24 commits after 3.0.0. The compatibility-relevant
changes are:

- optional `internals.metadata` and `internals.integrations` fields on
  `POST /node/xray/start`;
- an Integration module framework, with no concrete Integration bundled in the
  stock 3.2.2 source tree;
- optional custom Core and GeoData downloads described by
  `xrayConfig.geodata`;
- `torrentBlocker.webhookUrl` in `@remnawave/node-plugins@0.6.3`;
- executable-based Core version detection, including prerelease versions;
- a longer supervised stop window with SIGKILL escalation.

The remaining changes update build tooling, framework packages, and source
organization. They do not change the public HTTP surface. There are still 26
public `/node` routes and two internal Core-facing routes. No response schema,
HTTP method, route path, authentication rule, or success envelope changed.

## Start Request Internals

Panel may now send the following additional data:

```json
{
  "internals": {
    "metadata": {
      "name": "node-1",
      "uuid": "66baa45a-c6a2-44f8-80ac-2095dcfc4b6a",
      "id": 42,
      "tags": ["edge"],
      "countryCode": "NL"
    },
    "integrations": {
      "example": {
        "enabled": true
      }
    },
    "forceRestart": false,
    "hashes": {
      "emptyConfig": "empty-hash",
      "inbounds": []
    }
  },
  "xrayConfig": {}
}
```

Both new fields are optional. An omitted field is accepted, while explicit
JSON `null` is rejected. When `metadata` is present, all five shown fields are
required: `name`, `uuid`, and `countryCode` are strings, `id` is a number, and
`tags` is an array of strings. The official schema does not apply UUID, country
code, integer, positivity, or non-empty constraints to these values. Unknown
metadata fields are stripped with normal Zod object semantics.

`integrations` must be an object with string keys. Its values may be any JSON
value, including nested objects, arrays, scalars, and `null`. Unknown fields on
the request and `internals` objects remain accepted and stripped. Existing
`forceRestart`, hash, and opaque `xrayConfig` behavior is unchanged.

Remnanode Lite parses and validates both fields at the HTTP boundary. It then
discards them when mapping the request into the Go Xray lifecycle command. This
matches the observable behavior of the stock official 3.2.2 build because its
Integration descriptor list is empty: synchronization returns success without
reading either field. This is not a forward promise for future official
Integrations. A release that ships a concrete provider requires a new behavior
review and an explicit implementation decision.

## Torrent Report Webhook

The 0.6.3 plugin schema adds an optional, syntactically valid URL at
`torrentBlocker.webhookUrl`. When Torrent Blocker accepts a Core report, the
official implementation first applies its normal local block/report behavior
and then starts a best-effort HTTP `POST` of the report as JSON. Delivery has a
five-second timeout. HTTP status, response content, and delivery errors do not
change the local outcome, and there is no retry or durable outbound queue.

The Go implementation preserves the same ownership boundary. The existing
bounded internal Core-webhook queue still serializes local nftables and report
state. After that local operation commits, a configured external delivery is
attempted with `Content-Type: application/json` and a five-second timeout.
Failure is ignored for the local result. Shutdown cancels an in-flight delivery
and does not admit new work. Focused, race, and complete plugin tests passed;
the complete release gate remains required before publication.

## Lifecycle and Core Version Equivalence

Official 3.2.2 increases the s6 down wait from five to ten seconds and configures
the supervisor to escalate a stuck Core to SIGKILL. Remnanode Lite does not use
s6: it owns rw-core directly in a dedicated process group, sends SIGINT, waits
up to five seconds, then sends SIGKILL and waits up to another five seconds. It
also verifies whole-group cleanup before releasing process ownership. The
observable requirement is equivalent even though the supervisor mechanism is
not copied.

Official Node no longer trusts `XRAY_CORE_VERSION` as its runtime source of
truth. It executes the selected `/usr/local/bin/rw-core version` at startup and
after a successful Core start, retaining valid SemVer including prerelease
data. The Go manager likewise probes its selected executable with a bounded
output and timeout after successful readiness, caches the result atomically
with the lifecycle commit, and performs throttled background recovery after a
failed probe. An explicit configured version remains an accepted operational
override, not the default production source of truth.

The official bundled Core remains `v26.7.28`. This baseline therefore does not
change `release/runtime-assets.lock.json` merely to follow the Node contract.

## Panel-Selected Core and GeoData

Official 3.2.2 interprets two independent optional sections under
`xrayConfig.geodata`:

```json
{
  "xrayConfig": {
    "geodata": {
      "core": {
        "url": "https://downloads.example/rw-core",
        "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
      },
      "assets": [
        {
          "url": "https://downloads.example/geoip-custom.dat",
          "file": "geoip-custom.dat"
        }
      ]
    }
  }
}
```

Remnanode Lite implements these directives through the derived cache at
`/var/lib/remnanode-lite/panel-runtime/{assets,cores}`. Core and asset sections
are validated independently. Downloads require HTTPS across the complete
redirect chain, use 15-second total and five-second idle timeouts, enforce a
128 MiB limit, and publish completed files atomically.

Core files are content-addressed by SHA-256. A candidate is activated only
after its configured digest and executable `version` SemVer pass validation.
An invalid or failed candidate leaves the current usable Core in place and the
bundled release-locked Core remains the final fallback. GeoData reuses an
existing non-empty regular file. A failed missing-asset download creates the
official-compatible empty stub and continues; default GeoIP and GeoSite paths
are overlaid with the bundled files so the locked copies remain available.

Docker keeps its root filesystem read-only and persists this cache in a named
volume. Native installations use the existing service-owned
`/var/lib/remnanode-lite` hierarchy. The derived cache is intentionally outside
the installed generation, `release/runtime-assets.lock.json`, and the `rnlctl`
transaction journal: it is Panel-selected runtime state, not a release asset or
part of a release's reproducible identity.

## Accepted Implementation Differences

All accepted differences in the 3.0.0 baseline remain unless this document
overrides them. In particular, Remnanode Lite remains one Go process, does not
copy NestJS/CQRS/s6 internals, and preserves its bounded request, configuration,
queue, and process-ownership model.

The empty Integration framework is represented by accepting its public request
fields, not by adding speculative dynamic loading. Dynamic Core and GeoData are
implemented as a bounded derived cache instead of mutating packaged paths. This
keeps the maintained read-only container and Native generation model while
preserving the Panel-visible selection and fallback behavior.

## Verification

Source-level verification:

```bash
export REMNANODE_OFFICIAL_SOURCE=/absolute/path/to/remnawave-node-3.2.2
go run ./cmd/contract-source-check -source "$REMNANODE_OFFICIAL_SOURCE"
go test -count=1 ./internal/contract ./internal/nodeapi ./internal/plugin ./internal/xray
go test -race ./internal/contract/... ./internal/nodeapi ./internal/plugin ./internal/xray
```

The complete release gate must additionally exercise plugin webhook delivery
and failure, Core digest/version rejection, successful custom-Core selection,
GeoData cache reuse and stub fallback, cache persistence in the read-only
container profile, and bundled fallback without network access.

Release acceptance still requires the immutable `sha-<commit>` candidate to
connect to a compatible real Panel, start a usable Core, and carry real proxy
traffic. Host details, logs, traffic records, downloaded runtime payloads, and
server data remain outside the repository.
