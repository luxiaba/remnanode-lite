# Security Policy

Use this policy to report a vulnerability, check which releases receive
security fixes, and understand the privileges a node requires. For deployment
hardening and implementation details, see
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

Only published releases receive security support. `edge` and `sha-*` are
candidate builds without a long-term maintenance commitment. An `rnl.N`
version is a prerelease even when it is newer than the current stable release.
The standing policy is:

| Version | Security support |
| --- | --- |
| Stable release referenced by `latest` | Receives security fixes |
| Previous stable release on the same release line | High-impact issues are addressed where upgrade or rollback remains practical |
| Published `rnl.N` release referenced by `preview` | Best-effort fixes until a replacement preview or stable release is available |
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
- Native services run as the non-login `remnanode` user with only
  `CAP_NET_ADMIN` and `CAP_NET_BIND_SERVICE`. systemd applies a capability
  bounding set and sandboxing; experimental OpenRC support applies and verifies
  its resource cgroup before startup.
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

For Native systemd and OpenRC deployments, store the Secret in
`/etc/remnanode-lite/secret.key` with owner and mode `root:remnanode 0640`. The Go
process reads configuration and secret files through bounded, no-symlink file
paths; it does not export the complete `node.env` as the service environment.

Docker receives the Secret as an environment value whether it comes from a
same-directory `.env` or is written directly in the Compose mapping. It is
therefore visible in container metadata readable through `docker inspect`.
Protect both files when present:

```bash
chmod 600 docker-compose.yaml
[ ! -f .env ] || chmod 600 .env
```

Also restrict access to the Docker socket, backups, shell history, and host
administrator accounts. Before starting rw-core, the Node removes
`SECRET_KEY`, `SECRET_KEY_FILE`, `INTERNAL_REST_TOKEN`, and `REMNANODE_ENV` from
the inherited environment. It replaces managed resource paths and the internal
webhook token. That token is generated randomly at every start by default; an
explicit value is accepted only after Go configuration parsing. Other
unmanaged environment variables are still inherited, so do not inject
unrelated secrets into the Node container or native service.

Never commit `.env`, an expanded Compose file containing a real Secret,
`/etc/remnanode-lite/node.env`, `secret.key`, certificates, private keys, host
inventories, or raw runtime captures.

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
- Native `amd64` and `arm64` bundles contain a strict file manifest, an SPDX
  SBOM, exact runtime provenance, license notices, and a separate installer.
- Every published release asset is covered by `SHA256SUMS` and a GitHub build
  attestation. Native online install and upgrade resolve exact versions only.
- `rnlctl` keeps at most two verified generations, commits lifecycle changes
  through a durable transaction journal, and repairs from a verified cached
  bundle rather than downloading an unversioned replacement.

These controls do not make builds byte-for-byte reproducible. The Debian
packages installed by the Dockerfile are not yet pinned to a snapshot and exact
versions. For a final image, check the registry manifest digest together with
its SBOM, provenance, and attestation instead of trusting the tag alone.

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
- Keep a single owner for rw-core, plugin snapshots, nftables state, and Native
  generation selection. A
  failed side effect must not commit a false local success state.
- Own only this project's rw-core process, internal sockets, and fixed nftables
  tables. Do not take over the host's general firewall policy.
- Treat connection destruction as a host-network operation. It scans the host
  namespace for connected TCP sockets whose local or remote address equals the
  target IP, without filtering by PID, and may close another process's socket.
  Production nodes should therefore be dedicated to this workload. Requests
  from Panel filter local and special addresses; the administrative
  `remnanode-lite kill-sockets` command calls the kernel adapter directly and
  bypasses that application-level filter.
- Keep production observations outside the repository. Sanitize any diagnostic
  detail shared through a private advisory so users or production environments
  cannot be reconstructed from it.

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
integration, or risk-driven runtime checks.
