# Remnawave Node 3.4.2 Behavioral Contract Baseline

[Development guide](README.md) · [Previous 3.4.1 baseline](contract-3.4.1.md)

This baseline records the reviewed official `3.4.1..3.4.2` delta. Historical
contract documents remain unchanged. The public API still has 25 routes.

## Pinned Evidence

- Official Node: `3.4.2`, commit `ea7a835485af81ec464ebbf7acaaa7d6dd486a05`.
- Previous Node: `3.4.1`, commit `44912631321664dbd5822e9bf8d96766ccff7c93`.
- The checked-in `libs/contract/package.json` still declares
  `@remnawave/node-contract@3.4.1`; its version alone does not capture the
  changed cipher enum. The immutable Git objects define this baseline.
- Plugin package: `@remnawave/node-plugins@0.8.3`.
- XTLS SDK: `@remnawave/xtls-sdk@0.17.0`.
- Bundled rw-core `v26.7.28` and GeoCheck `v0.3.0` are unchanged.

The plugin archive has SHA-1 `ffe6e9bc719bce8bdbaf4042723762503f149953` and
integrity `sha512-GtJv0vOoAarwj4dmVndkrOgA9QzqxiX4Qm4AgMKPk+MmLOQhcOfZSXjGrLstekNkuXgvTZY2KnLBzzR1qGcYKA==`.
Its `package/build/backend/models/node-plugins.schema.js` SHA-256 is
`4b6f8cff65d4ca1cfa192d6e512396ae207d849045759b64538c1734091b2836`.

The SDK archive has SHA-1 `2942f6c3c6880c8f99ba312dec7e2c9af840d2fd` and
integrity `sha512-D6pTJ2CqchuttUbF/b1RDU5+8HSv+FURYGqxyrJ2f7qYf5br8WokSiE7uveeYPn5OwG75HQh4ZFCRsWlA1Fs2A==`.
The used Handler/Stats RPC paths and field numbers remain compatible; the
all-user IP worker-array refactor does not change the response semantics.
The minimal Go wire adapter remains independent of the SDK's generated types.

## Post-start Webhook

Plugin configuration accepts this optional section:

```json
{
  "postStart": {
    "enabled": true,
    "webhook": {
      "enabled": true,
      "url": "https://example.com/core-events"
    }
  }
}
```

`postStart.enabled` defaults to false. `webhook` is optional; when present,
both its boolean `enabled` and HTTP(S) `url` are required, even when disabled.
The feature works without nftables. Clearing or disabling the plugin clears
the effective notification settings without restarting an already running core.

After an actual core start passes gRPC readiness and commits running state,
the Node sends a background POST with `scope=service`,
`event=service.core_started`, a UTC millisecond timestamp, and the validated
`internals.metadata` fields (`name`, `uuid`, `id`, `tags`, `countryCode`). When
metadata is absent the field is `{}`. Unknown metadata fields are stripped.
Unchanged successful configuration synchronization and failed starts emit no
event. Forced restarts and changed configurations emit an event only on success.

HTTP delivery is best-effort with a five-second timeout. It does not change the
successful start response. The existing plugin worker owns outbound delivery;
post-start and Torrent events share its 64-item queue and four HTTP slots.
URL and body are captured before admission so later plugin changes cannot
redirect a queued notification. Shutdown cancels and joins outstanding delivery.

Lite retains explicit resource differences: URLs have the existing 512-byte
plugin string limit; metadata has an 8 KiB pre-encoding budget including tag
framing. Oversized metadata or unavailable queue/delivery capacity skips the
notification with a bounded diagnostic, while the core remains started. URL
credentials, query tokens, and metadata are not included in failure logs.

## Shadowsocks Cipher Retirement

`POST /node/handler/add-user` no longer accepts Shadowsocks `cipherType=9`
(`NONE`). Allowed values are `-1`, `0`, `5`, `6`, `7`, and `8`. Both the request
validator and the independent executable contract reject the retired value
before any gRPC or hash-state mutation. Other protocol variants are unchanged.

## Delivery and Verification

The Go toolchain and Docker builder advance to `1.26.9` to address the
standard-library findings blocking dependency PRs. Runtime assets remain at
the reviewed official baseline; this toolchain change does not rebuild or
remove vulnerabilities from the independently bundled rw-core or GeoCheck.

Verification includes the pinned-source oracle, request and response tests,
post-start lifecycle/queue/cancellation regressions, race tests, Linux network
namespace checks, and real-core resource tests. Release acceptance still
requires the immutable candidate, a compatible Panel, and representative proxy
traffic as specified in the [release guide](../release.md).
