# Remnawave Node 3.4.3 Behavioral Contract Baseline

[Development guide](README.md) · [Previous 3.4.2 baseline](contract-3.4.2.md)

## Pinned Evidence

- Official Node: `3.4.3`, commit `2ad154f8f1016c94c1872f337268af878af61b18`.
- Previous Node: `3.4.2`, commit `ea7a835485af81ec464ebbf7acaaa7d6dd486a05`.
- Reviewed comparison: [official 3.4.2...3.4.3](https://github.com/remnawave/node/compare/3.4.2...3.4.3).
- Exactly three files changed: `docker/Dockerfile`, `package.json`, and
  `package-lock.json`.

## Official Delta

Both official Docker stages move from `node:24.20-trixie-slim` to
`node:24.22-trixie-slim` for the upstream-reported Brotli security fix
(CVE-2026-111497). The package and lockfile root versions become `3.4.3`.

The public API still has 25 routes. Controllers, command schemas, authentication,
core lifecycle, and plugin behavior are unchanged. The contract npm package
still declares `3.4.1`; plugin `0.8.3`, XTLS SDK `0.17.0`, rw-core `v26.7.28`,
and GeoCheck `v0.3.0` are unchanged. Their evidence and the inherited post-start
webhook behavior are documented in the [3.4.2 baseline](contract-3.4.2.md).

## Lite Alignment

Remnanode Lite reports project and contract version `3.4.3`, pins the new
official commit in its source oracle and workflows, and updates deployment
defaults. It uses Go and does not include the official Node.js runtime.

Separately, the scheduled security scan identified GO-2026-6809
(CVE-2026-111943) in the Go `crypto/x509` certificate verifier. The Go toolchain
and SHA-pinned Docker builder move from `1.26.9` to `1.26.10`, which includes
the fix. This does not rebuild the separately bundled upstream runtime binaries.

Verification focuses on the exact official diff, regenerated source evidence,
version consistency, documentation, the security finding, and the required
CI and immutable release-candidate checks. Runtime operations inherit the
unchanged 3.4.2 behavior and its existing integration coverage.
