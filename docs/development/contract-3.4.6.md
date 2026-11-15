# Remnawave Node 3.4.6 Behavioral Contract Baseline

[Development guide](README.md) · [Previous 3.4.4 baseline](contract-3.4.4.md)

## Pinned Evidence

- Official Node: `3.4.6`, commit `52d0f1c00c23bf99912687cb3b75faffb2d3bffb`.
- Previous Node: `3.4.4`, commit `c1e40e6d27c68e8a2325419a6c193488d8952984`.
- Reviewed comparisons: [official 3.4.4...3.4.5](https://github.com/remnawave/node/compare/3.4.4...3.4.5) and [3.4.5...3.4.6](https://github.com/remnawave/node/compare/3.4.5...3.4.6).

Two official releases are covered because `3.4.6` changes only dependency
metadata. `3.4.5` carries every behavioral change in this baseline.

## Official 3.4.5 Changes

`3.4.5` touches nine files. The public `/node` surface is unchanged: the route
constants, command schemas, response models, and error codes in
`libs/contract` are byte-identical to `3.4.4`, and the controllers still declare
the same 25 externally visible routes.

### Bundled runtime

The upstream Docker build moves Xray-core from `v26.10.31` to `v26.11.9`. The
exact archives, extracted binaries, and their digests are locked in
[`release/runtime-assets.lock.json`](../../release/runtime-assets.lock.json).

### Plugin schema

`@remnawave/node-plugins` moves from `0.8.3` to `0.9.0`. The compiled schema is
structurally identical: after removing documentation strings, the two schema
files are byte-identical. The only change is one `markdownDescription` string
for the `postStart` section. No validation rule, field, or bound moved, so
`internal/plugin/schema_validate.go` needs no change.

### New environment variables

| Variable | Default | Lite status |
| --- | --- | --- |
| `NFTABLES_TABLE_NAME` | `remnanode` | Not implemented |
| `SUPPRESS_DUPLICATE_INSTANCE_WARNING` | `false` | Not implemented |

`NFTABLES_TABLE_NAME` lets several official Nodes share one network namespace
by giving each its own nftables table. It does not let one Node serve several
Panels: a Node still authenticates with a single Panel Secret and still replaces
its whole Xray configuration on every start. Lite keeps the fixed `remnanode`
and `remnanode6` tables and the single-instance model documented in the
[architecture guide](../architecture.md); the default value is identical, so
omitting the variable does not change observable behavior.

`SUPPRESS_DUPLICATE_INSTANCE_WARNING` only silences a log line. Lite rejects a
second instance at startup through the `flock` on the internal socket directory,
which is a stronger and already documented guarantee.

### `routing.script` — documented deviation

Official `3.4.5` stopped injecting the API routing rule when the Panel-supplied
configuration contains `routing.script`:

```ts
rules: [
    ...(routing?.script ? [] : [XRAY_ROUTING_RULES_MODEL]),
    ...(routing?.rules ?? []).filter((rule) => rule.outboundTag !== 'REMNAWAVE_API'),
],
```

That rule is what routes `REMNAWAVE_API_INBOUND` traffic to the `REMNAWAVE_API`
outbound. Without it, rw-core's gRPC API has no matching route and the Node
loses every Handler and Stats operation while still reporting a healthy start.

Lite keeps the rule when `routing.script` is present and logs a warning naming
the deviation. The rationale:

- A Panel-authored script is not guaranteed to cover the Node's own API
  inbound, and the failure is silent and severe.
- The extra rule only affects the Node's internal gRPC channel. It does not
  change proxy routing for users, so the Panel observes no difference.
- The project treats documented, observable safety deviations as acceptable
  when they cannot corrupt the external contract.

`TestGenerateAPIConfigKeepsAPIRuleWhenRoutingScriptPresent` pins this behavior.

## Official 3.4.6 Changes

`3.4.6` changes `package.json` and `package-lock.json` only. No file under
`src/`, `libs/`, or `docker/` changes, so the contract, route surface, plugin
schema version, and bundled Xray-core version are identical to `3.4.5`. The
dependency moves are `lmdb` 3.5.10, `nestjs-zod` 5.6.0, `@rspack/*` 2.3.2,
`nf3` 0.4.3, `oxfmt` 0.78.0, `oxlint` 1.93.0, and
`ts-checker-rspack-plugin` 1.8.0. None of them is used by this Go
implementation.

## Lite Alignment

Remnanode Lite reports project and contract version `3.4.6`, pins the official
source commit, rebuilds the source-evidence manifest, and updates the bundled
Xray-core runtime.

Verification for this alignment:

- `cmd/contract-source-check` rebuilds and verifies all 94 evidence blobs at the
  pinned commit.
- `internal/contract` route and schema tests still pass unchanged, confirming
  the 25-route surface.
- `cmd/release-tool` tests validate the new runtime lock, the third-party
  notices table, and the source offer against the locked provenance.
