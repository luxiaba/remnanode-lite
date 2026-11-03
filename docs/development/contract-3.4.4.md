# Remnawave Node 3.4.4 Behavioral Contract Baseline

[Development guide](README.md) · [Previous 3.4.3 baseline](contract-3.4.3.md)

## Pinned Evidence

- Official Node: `3.4.4`, commit `c1e40e6d27c68e8a2325419a6c193488d8952984`.
- Previous Node: `3.4.3`, commit `2ad154f8f1016c94c1872f337268af878af61b18`.
- Reviewed comparison: [official 3.4.3...3.4.4](https://github.com/remnawave/node/compare/3.4.3...3.4.4).
- The upstream Docker build updates Xray-core to `v26.10.31`; package and
  lockfile metadata also update the official Node release and npm dependencies.

## Lite Alignment

The public API remains 25 routes. The official Node controllers, schemas,
authentication, plugin behavior, and lifecycle contract are unchanged.
Remnanode Lite reports project and contract version `3.4.4`, pins the official
source commit, and updates its deployment defaults.

Lite does not use the official Node.js/npm runtime. Its relevant upstream
runtime change is the bundled Xray-core update from `v26.7.28` to `v26.10.31`
for amd64 and arm64. The exact archive and binary digests are locked in
[`release/runtime-assets.lock.json`](../../release/runtime-assets.lock.json),
and the corresponding source and notices are updated with the release.

The inherited 3.4.3 contract behavior, including post-start webhooks, remains
unchanged. Verification focuses on the exact official diff, regenerated source
evidence, Xray asset lock integrity, version consistency, and the required CI
and immutable release-candidate checks.
