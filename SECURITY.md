# Security Policy

This document defines how to report vulnerabilities in Remnanode Lite, which
releases receive security support, and the trust boundaries operators must
understand. For deployment hardening and implementation details, see
[Architecture](docs/architecture.md) and [Operations](docs/operations.md).

## Reporting a Vulnerability

Report security issues through
[GitHub private vulnerability reporting](https://github.com/luxiaba/remnanode-lite/security/advisories/new).
Do not disclose any of the following in a public issue, discussion, log, or
screenshot:

- `SECRET_KEY`, JWTs, certificate authorities, node certificates, or private
  keys.
- Panel URLs, real IP addresses, hostnames, or other identifying node details.
- Unredacted requests, responses, configuration, or runtime logs.
- Complete exploitation instructions that would enable an attack before a fix
  is released.

Include the affected version or commit, deployment method, expected impact,
minimal reproduction, and any known mitigation where possible. Reproduce the
problem with fictional addresses and sanitized material. Maintainers will ask
for additional details inside the private advisory when needed.

If a secret may have been exposed, rotate it immediately. Removing the value in
a later commit does not remove it from Git history, logs, caches, registries, or
other copies.

## Supported Versions

Security support applies only to published formal releases. `edge`, `sha-*`,
and `candidate-sha-*` are candidate builds and carry no long-term security
maintenance commitment. At any point, support follows this policy:

| Version | Security support |
| --- | --- |
| Stable release referenced by `latest` | Receives security fixes |
| Previous stable release on the same release line | High-impact issues are addressed where upgrade or rollback remains practical |
| `edge`, historical candidates, and older releases | No guaranteed fixes; upgrade to a supported release |

The applicable GitHub Security Advisory and release notes may narrow or extend
support for a specific issue.

## Runtime Trust Boundaries

Remnanode Lite is a network-management node, not an ordinary unprivileged web
service:

- The public Panel-to-Node API requires TLS 1.3 or later, mutual TLS, and an
  RS256 Bearer JWT.
- Docker uses the host network namespace. `NET_ADMIN` permits the container to
  manage this project's nftables tables and destroy matching connections with
  `NETLINK_SOCK_DIAG`; `NET_BIND_SERVICE` permits listeners on privileged ports.
- The current container starts as root, drops every other capability, enables
  `no-new-privileges`, and uses a read-only root filesystem. Host networking and
  `NET_ADMIN` nevertheless remain an explicit host trust boundary.
- Run only verified images produced by this repository. Pin an exact version or
  manifest digest in production and verify its build attestation.
- The Node does not persist the complete Xray configuration received from
  Panel. Panel resynchronizes it after a restart. Runtime logs may likewise be
  kept only in bounded temporary storage.

The supported single-file production template is
[`deploy/compose.single-file.yaml`](deploy/compose.single-file.yaml). Keep its
capability, read-only filesystem, tmpfs, process, memory, CPU, healthcheck, and
log-rotation controls unless a reviewed deployment change explicitly replaces
them.

## Secret Handling

For native systemd and OpenRC deployments, store the secret in
`/etc/remnanode/secret.key` with owner and mode `root:remnanode 0640`. The Go
process reads configuration and secret files through bounded, no-symlink file
paths; it does not export the complete `node.env` as the service environment.

The single-file Compose deployment necessarily stores the secret inline. It is
therefore visible in container metadata readable through `docker inspect`.
Protect the file with:

```bash
chmod 600 docker-compose.yaml
```

Also restrict access to the Docker socket, backups, shell history, and host
administrator accounts. Before starting rw-core, the Node removes
`SECRET_KEY`, `SECRET_KEY_FILE`, `INTERNAL_REST_TOKEN`, and `REMNANODE_ENV` from
the inherited environment. It replaces managed resource paths and the internal
webhook token. That token is generated randomly at every start by default; an
explicit value is accepted only after Go configuration parsing. Other
unmanaged environment variables are still inherited, so do not inject
unrelated secrets into the Node container or native service.

Never commit `.env`, an expanded Compose file containing a real secret,
`/etc/remnanode/node.env`, `secret.key`, certificates, private keys, or raw
acceptance captures.

## Supply-Chain Controls

The repository currently applies these controls:

- GitHub Actions are pinned to complete commit SHAs.
- Go modules are verified, and the scheduled
  [security workflow](.github/workflows/security.yml) runs `govulncheck` against
  reachable Go code.
- Container base images are pinned by manifest digest.
- rw-core, geo data, and ASN sources are pinned by version or commit and checked
  against download digests.
- Release images include an SBOM, BuildKit provenance, and a GitHub build
  attestation.
- Release binaries, the Compose asset, and data assets are covered by
  `SHA256SUMS`.

These controls do not make builds byte-for-byte reproducible. Debian packages
installed by the Dockerfile are not yet pinned to a snapshot and exact package
versions. Identify a final artifact by its registry manifest digest, SBOM,
provenance, and attestation together; do not trust a tag name alone.

Repository CI is defined in [`.github/workflows/ci.yml`](.github/workflows/ci.yml).
The root [`CHANGELOG.md`](CHANGELOG.md) records user-visible release changes.
Files under `docs/archive/`, such as the
[2026 audit remediation record](docs/archive/2026-07-audit-remediation.md),
preserve historical context and must not be treated as the current security
posture or release status.

## Security Design Principles

- Authenticate and enforce decompression, body-size, JSON-decoding, and
  contract limits on external input before side effects.
- Bound processes, queues, request bodies, concurrency, external-command
  output, and shutdown duration.
- Keep a single owner for rw-core, plugin snapshots, and nftables state. A
  failed side effect must not commit a false local success state.
- Own only this project's rw-core process, internal sockets, and fixed nftables
  tables. Do not take over the host's general firewall policy.
- Treat connection destruction as a host-network operation. It scans the host
  network namespace for TCP connections matching a target IP and may close a
  connection owned by another process when that process uses the same IP.
  Production nodes should therefore be dedicated network execution
  environments. The Panel path filters local and special addresses, but the
  administrative `remnanode-lite kill-sockets` command calls the kernel adapter
  directly and does not provide that business-layer protection.
- Keep release acceptance data sanitized. Evidence and raw capture bundles must
  not contain data from which users or production environments can be
  reconstructed.

## Security Changes

Security-sensitive changes must follow [CONTRIBUTING.md](CONTRIBUTING.md), add
regression coverage, and run the checks appropriate to their boundary. The
full local gate is:

```bash
REMNANODE_OFFICIAL_SOURCE=/absolute/path/to/pinned-official-source \
REQUIRE_GOVULNCHECK=1 \
  bash scripts/check.sh
```

Passing this gate proves repository-level checks only. It does not replace
real Linux namespace tests, candidate attestation verification, Panel
integration, or any version-specific runtime and extended-acceptance checks
required by the applicable release profile.
