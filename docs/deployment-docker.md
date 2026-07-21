# Docker Compose Deployment

[Back to the documentation index](README.md)

Docker Compose is the preferred deployment method for low-memory nodes. A server needs only a permission-restricted YAML file and Docker Engine; it does not need the source tree, a Go toolchain, or a persistent log volume.

The primary workflow on this page uses the single-file Compose layout suited to fleets of independent small nodes. Both supported templates can read deployment values from a same-directory `.env`; the repository-root `compose.yaml` remains available for centralized configuration or local source builds.

## Deployment model

The container has one application supervisor: `remnanode-lite` starts and reaps rw-core directly, without s6 or another resident service supervisor. Compose enables a minimal init for PID 1 duties, while the Node and rw-core share one container cgroup with:

- a `448 MiB` hard memory limit and no additional swap;
- `1 CPU` and `256 PIDs`;
- a read-only root filesystem;
- tmpfs mounts for `/run/remnanode`, `/tmp`, and `/var/log/remnanode`, capped at `48 MiB` in total;
- Docker `json-file` rotation of `2 MiB x 2` for Node logs;
- no persistent data volume. Recreating the container clears runtime configuration copies and logs, and the Panel sends the Xray configuration again.

These are strict container cgroup limits and remain required when the Docker host is larger. The `docker-production-smoke-v2` profile may run on such a host, but it verifies that the container still has exactly a `448 MiB` memory limit, a `448 MiB` combined memory-and-swap limit, `1 CPU`, and `256 PIDs`. Equal memory and combined limits leave no additional container swap allowance. Proving the separate whole-machine target of `512 MiB RAM / 1 vCPU / 2 GB disk` is deferred follow-up work; the container smoke must not be presented as that proof. These limits are not an SLA for every traffic pattern or plugin combination. See the [resource budget](development/resource-budget.md) for measurements and boundaries.

## Choose an image

The image is public on GHCR and can be pulled anonymously:

```text
ghcr.io/luxiaba/remnanode-lite
```

| Tag | Behavior | Recommended use |
| --- | --- | --- |
| `X.Y.Z-rnl.N` | Independently versioned project iteration that passed the release process | Recommended for production and precise rollback |
| `X.Y.Z` | Formal build aligned with the corresponding official version | Recommended for production |
| `latest` | Most recent stable build that passed the release process | Opt-in stable tracking; not a rollback identifier |
| `sha-<40-character-commit>` | Candidate built for a `main` commit | Discover the candidate, then resolve and pin its digest |
| `candidate-sha-<40-character-commit>` | Independently rebuilt candidate manually dispatched from `main` | Discover a manual rebuild, then resolve and pin its digest |
| `edge` | Moving candidate for current `main` | Short-term observation only |

By project policy, exact versions, `sha-*`, and `candidate-sha-*` are not intentionally moved, but registry tags are not technically immutable. Use a `name@sha256:...` manifest digest for the strongest pin. Before the first formal Release, `latest` and exact version tags do not exist. Select a real candidate from the [GHCR package](https://github.com/luxiaba/remnanode-lite/pkgs/container/remnanode-lite) and record its manifest digest.

See the [version model](versioning.md) for naming and promotion rules.

## Prerequisites

- Linux `amd64` or `arm64`.
- Docker Engine with Compose v2, invoked as `docker compose`.
- A node created in the Panel and its complete `SECRET_KEY`.
- The Node port in the Panel matches `NODE_PORT`.
- The host firewall permits the Panel to reach the Node API port and permits inbound proxy ports required by the deployed configuration.

Compose uses `network_mode: host`; do not add `ports:`. The container holds `NET_ADMIN`, so it can manage the project's private nftables table and close connections in the host network namespace. Run only trusted images.

## Single-file deployment

For production, use the Compose file attached to the same Release as the image. The file is covered by that Release's `SHA256SUMS` and already points to the exact version.

Download the single-file asset and checksums from the same GitHub Release:

```bash
VERSION=X.Y.Z-rnl.N # or X.Y.Z
BASE_URL="https://github.com/luxiaba/remnanode-lite/releases/download/v${VERSION}"

mkdir -p /opt/remnanode-lite
cd /opt/remnanode-lite
curl -fL "${BASE_URL}/docker-compose.single-file.yaml" -o docker-compose.yaml
curl -fLO "${BASE_URL}/SHA256SUMS"
grep -F ' docker-compose.single-file.yaml' SHA256SUMS \
  | sed 's|docker-compose.single-file.yaml|docker-compose.yaml|' \
  | sha256sum --check --strict
chmod 600 docker-compose.yaml
```

This command uses GNU `sha256sum`, which is available on the supported Linux hosts. After verification, set the image, Node port, and Secret in a `.env` file beside `docker-compose.yaml`:

```env
REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N
NODE_PORT=38329
SECRET_KEY=PASTE_THE_COMPLETE_BASE64_VALUE_FROM_THE_PANEL
LOW_MEMORY=1
```

```bash
chmod 600 .env
```

When commands are run from this directory, Compose automatically reads that `.env` for interpolation; an exported shell variable with the same name takes precedence. The template's explicit `environment` mapping determines which values enter the container, so unrelated keys in `.env` are not injected. Variables with safe defaults may be omitted. The version above illustrates the format: replace it with an exact version, `sha-*` candidate, or digest that actually exists in GHCR. Use `latest` only if you deliberately want to follow the stable channel.

### Testing a candidate

Before the first formal Release, or when testing a new candidate, download the Compose template from the same commit as the image:

```bash
(
  set -euo pipefail
  candidate_tag=REPLACE_WITH_FULL_SHA_OR_CANDIDATE_SHA_TAG
  case "$candidate_tag" in
    sha-*) candidate_commit="${candidate_tag#sha-}" ;;
    candidate-sha-*) candidate_commit="${candidate_tag#candidate-sha-}" ;;
    *) echo "candidate tag must be sha-<commit> or candidate-sha-<commit>" >&2; exit 1 ;;
  esac
  printf '%s\n' "$candidate_commit" | grep -Eq '^[0-9a-f]{40}$'

  mkdir -p /opt/remnanode-lite
  cd /opt/remnanode-lite
  curl -fL \
    "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${candidate_commit}/deploy/compose.single-file.yaml" \
    -o docker-compose.yaml
  cat >.env <<EOF
REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite:${candidate_tag}
NODE_PORT=38329
SECRET_KEY=PASTE_THE_COMPLETE_BASE64_VALUE_FROM_THE_PANEL
LOW_MEMORY=1
EOF
  chmod 600 docker-compose.yaml .env
)
```

Choose a full `sha-<40-character-commit>` or `candidate-sha-<40-character-commit>` tag from the [GHCR package](https://github.com/luxiaba/remnanode-lite/pkgs/container/remnanode-lite). Replace the placeholder port and Secret before startup. For formal acceptance, resolve the tag to its manifest digest and set `REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>` in `.env`. Candidate tags are test builds and must not be presented as released versions.

### Environment and Secret syntax

Both the single-file and repository-root templates use interpolation in a YAML mapping:

```yaml
environment:
  NODE_PORT: "${NODE_PORT:-38329}"
  SECRET_KEY: "${SECRET_KEY:?set SECRET_KEY in .env or the shell}"
```

`.env` is an interpolation source, not an instruction to copy the whole file into the container. Only keys explicitly named in this mapping are passed through. The shell has precedence over `.env`, so check and unset stale exported values when an edit appears to have no effect.

Do not use this list form:

```yaml
environment:
  - SECRET_KEY="eyJ..."
```

In the list form, the quotes become part of the value and commonly produce:

```text
decode SECRET_KEY: illegal base64 data at input byte 0
```

After interpolation, the effective Secret is visible in local `docker inspect` metadata regardless of whether it came from `.env`, the shell, or an inline mapping. Keep every file containing the Secret at mode `0600`, and restrict access to the Docker socket, backups, and host administration. Before launching rw-core, the Node removes the Panel Secret from the child environment.

## Start and verify

```bash
cd /opt/remnanode-lite
docker compose config --quiet
docker compose pull
docker compose up -d --no-build
docker compose ps
docker compose logs --tail=100 remnanode-lite
ss -H -lnt "sport = :38329"
```

Do not run `docker compose config` without `--quiet` in automation logs; it expands and prints the effective Secret.

A `healthy` container means the Node accepted a connection on its internal Unix socket. You still need to check the Panel and real traffic, because this healthcheck does not cover:

- the Panel can reach the Node over the network;
- mTLS, JWT, or the Secret is correct;
- rw-core is online;
- proxy inbound ports sent by the Panel are reachable.

It is normal for rw-core to be offline immediately after a Node restart. The Node does not restore an old Panel configuration from disk. A later Panel health cycle calls `/node/xray/start` again. Complete verification in the Panel and test representative proxy traffic.

## Migrate from the official or legacy container

The `NODE_PORT` and complete `SECRET_KEY` used by the official `remnawave/node` image remain valid. They belong to the external Panel-to-Node contract, not to the official image's Node.js and s6 internals. The same procedure applies to an older Remnanode Lite template whose service and container were named `remnanode`. Do not run `remnanode` and `remnanode-lite` together: host networking would make them compete for the Node API and proxy inbound ports.

New examples use `/opt/remnanode-lite` to keep the deployment directory distinct from the official container. An existing custom directory does not need to move; run every Compose command from the directory that actually contains that deployment's Compose file and optional `.env`.

1. Back up the existing Compose file and record the exact official image version as the rollback target.
2. While the old Compose definition is still available, stop it with `docker compose down`. Then replace the service definition with the complete single-file template from this page. Preserve host networking, both capabilities, resource limits, the read-only root filesystem, tmpfs mounts, and log limits.
3. Reuse the original `NODE_PORT` and Secret through the explicit `environment` mapping, and pin the image to a real project version, `sha-*` candidate, or digest.
4. Run `down --remove-orphans` for the new Compose project. If a container named `remnanode` still exists because it belonged to another Compose project, inspect it, confirm that it is the old Node, and remove it explicitly before starting the replacement.

```bash
cd /opt/remnanode-lite
docker compose down --remove-orphans
docker container inspect remnanode \
  --format 'name={{.Name}} image={{.Config.Image}}' 2>/dev/null || true
```

If the inspection prints a container, verify that its name and image identify the old Node. Only after that manual check, remove it with this separate command:

```bash
docker rm -f remnanode
```

Once `remnanode` is absent, start and verify the replacement:

```bash
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode-lite
```

5. Confirm that the node returns online in the Panel, rw-core starts, and representative proxy traffic works. This implementation writes rw-core logs to `/var/log/remnanode/xray.out.log` and `/var/log/remnanode/xray.err.log`, not the official container's `/var/log/xray/current`.

There is no container runtime state or Xray configuration volume to migrate; the Panel sends the configuration again. To roll back, first bring down `remnanode-lite`, then restore the backed-up Compose file and exact official image. Never leave both container names running. Keep the backup until the new container has completed its observation period.

## Release candidates

When a container input changes on `main`, the `container` workflow builds `linux/amd64` and `linux/arm64` images, publishes a multi-architecture manifest, and records build provenance. Once those steps pass, it publishes the `sha-<commit>` tag and updates `edge` if that commit is still the head of `main`. These checks identify how an image was built; they do not prove that it runs correctly.

The `docker-production-smoke-v2` profile requires a 600-second smoke test on a real native `linux/amd64` / `x86_64` host, using the canonical Compose file and a manifest-digest pin. The host may have more CPU, memory, disk, or host swap than the whole-machine target; the evidence instead verifies the exact container cgroup limits, health, memory and PID observations, Panel connectivity, rw-core startup, real proxy traffic, OOM state, and restart count. Whole-host `512 MiB RAM / 1 vCPU / 2 GB disk / zero swap` validation, `arm64-production-runtime`, native systemd and OpenRC installation, the 50,000-user load test, 24-hour soak, and fault and rollback injection remain follow-up work rather than `v2.8.0` release blockers. The complete requirements and evidence format are in the [release acceptance protocol](development/release-acceptance.md#docker-production-smoke).

A candidate has no GitHub Release assets and is not a formal Release. Build attestations cover the build chain, while runtime records describe what an operator observed; neither should be presented as proof of the other.

## Pin a digest and verify provenance

After pulling an image, record its registry digest:

```bash
VERSION=X.Y.Z-rnl.N # or X.Y.Z
IMAGE="ghcr.io/luxiaba/remnanode-lite:${VERSION}"

DIGEST_REF="$(docker image inspect \
  --format '{{range .RepoDigests}}{{println .}}{{end}}' \
  "$IMAGE" | head -n 1)"
printf '%s\n' "$DIGEST_REF" \
  | grep -Eq '^ghcr\.io/luxiaba/remnanode-lite@sha256:[0-9a-f]{64}$'
```

Use the complete output in Compose:

```yaml
image: ghcr.io/luxiaba/remnanode-lite@sha256:...
```

With GitHub CLI installed, verify provenance produced by this repository:

```bash
gh attestation verify \
  "oci://${DIGEST_REF}" \
  --repo luxiaba/remnanode-lite \
  --cert-identity https://github.com/luxiaba/remnanode-lite/.github/workflows/container.yml@refs/heads/main \
  --deny-self-hosted-runners
```

A tag states which version you intend to reference. A digest identifies the bytes actually deployed. A controlled fleet rollout should record the digest.

## Update and rollback

Back up the current Compose inputs. Change `REMNANODE_IMAGE` in `.env`, or `image:` when it is intentionally inline, then pull and recreate explicitly:

```bash
cp -p docker-compose.yaml docker-compose.yaml.previous
[ ! -f .env ] || cp -p .env .env.previous
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode-lite
```

To roll back, restore the previously verified Compose file and `.env`, or change the active image setting back to the previous exact version or digest, then repeat `pull` and `up`. Never implement rollback by moving an old version tag.

`latest` does not replace a running container. Tracking it still requires periodic, explicit pull and recreate operations, with the previous digest recorded before each update.

## Fleet rollout

Use one verified manifest digest throughout a fleet rollout and keep the previous digest for rollback. Exact version tags are easier to read, but deployment records should still retain `name@sha256:...`. Do not send `latest` or `edge` directly to every node.

1. Group nodes by architecture, distribution, region, and primary traffic profile. Record the current digest, target digest, and rollback Compose file for every node.
2. Start with a small canary group that represents the fleet's networks and architectures. Observe at least one traffic peak. Confirm Panel connectivity, rw-core synchronization, real proxy traffic, memory, restarts, processes, disk, and logs.
3. Expand in stages of approximately `5%`, `25%`, and `50%`, then deploy the remainder. Finish each observation period before continuing. Keep each batch small enough to restore its previous digest within the same maintenance window.
4. At every stage, sample container health, Panel state, proxy traffic, restart and OOM counts, memory, PIDs, disk, and Xray or nft errors. Track the digest deployed to each node.
5. Stop if a stage shows unexplained node loss, proxy failure, repeated Xray startup failure, OOM, unexpected restarts, stuck processes, resource-limit violations, or a cluster of similar errors. Roll back that batch before investigating further, and keep its logs tied to the deployed digest.

Rollback does not depend on moving a registry tag. Restore each node's recorded previous Compose file or digest, run `pull` and `up --force-recreate`, and confirm Panel connectivity and real traffic again. Until the issue has a clear conclusion, do not continue with untouched nodes or prune the previous image from canaries.

Release acceptance does not replace a staged production rollout. Keep the acceptance record linked from the Release notes, then observe each fleet stage on its own.

## Repository-root Compose and `.env`

Both supported templates automatically interpolate their explicitly declared variables from a same-directory `.env`. The single-file workflow above can therefore use `.env` without changing templates. To use the repository-root layout, download `compose.yaml`, the environment template, and checksums from the same formal GitHub Release. Do not combine a future `main` Compose file with an older image:

```bash
VERSION=X.Y.Z-rnl.N # or X.Y.Z
BASE_URL="https://github.com/luxiaba/remnanode-lite/releases/download/v${VERSION}"

curl -fLO "${BASE_URL}/compose.yaml"
curl -fLO "${BASE_URL}/remnanode.env.example"
curl -fLO "${BASE_URL}/SHA256SUMS"
grep -E ' (compose.yaml|remnanode.env.example)$' SHA256SUMS \
  | sha256sum --check --strict
mv remnanode.env.example .env
chmod 600 .env
```

Set at least:

```env
REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N
NODE_PORT=38329
SECRET_KEY=PASTE_THE_COMPLETE_VALUE
LOW_MEMORY=1
```

Keep `REMNANODE_IMAGE` at the exact version from that Release, or replace it with a verified manifest digest. Compose reads `.env` automatically when invoked from this directory, shell variables take precedence, and only values named in the Compose `environment` mapping enter the container. See the [configuration reference](configuration.md) for every variable.

## Local source build

Build from source only for development, audit work, or an emergency in which the registry is unavailable:

```bash
git clone https://github.com/luxiaba/remnanode-lite.git
cd remnanode-lite
cp .env.example .env
chmod 600 .env
# Edit .env

docker compose -f compose.yaml -f compose.build.yaml build --pull
docker compose -f compose.yaml -f compose.build.yaml up -d --no-build
```

Do not build on a production node with only 2 GB of disk. The Go toolchain, base layers, and BuildKit cache can substantially exceed the runtime disk budget.

## Logs and disk

Follow Node process logs:

```bash
docker compose logs -f remnanode-lite
```

Follow rw-core logs:

```bash
docker exec -it remnanode-lite tail -n 50 -F /var/log/remnanode/xray.out.log
docker exec -it remnanode-lite tail -n 50 -F /var/log/remnanode/xray.err.log
```

Each rw-core stream rotates at `4 MiB` and retains one `.1` file inside the `28 MiB` tmpfs; recreating the container clears it. Docker limits Node `json-file` logs to approximately `2 MiB x 2`. The project does not require persistent logs. Any long-term collection must fit within the host's own disk budget.

Inspect disk use and remove unused images:

```bash
docker system df
docker image ls ghcr.io/luxiaba/remnanode-lite
docker image prune
```

Before pruning, record a verified previous version tag or manifest digest and confirm that its image remains local. Always retain at least that one explicit rollback image. By default, `docker image prune` removes only dangling images. Do not use broad pruning options that could remove the only rollback version. See the [operations guide](operations.md) for routine commands and troubleshooting.

## Image contents and traceability

The current image contains:

- a statically linked `remnanode-lite`;
- rw-core `v26.6.27`, pinned by version and asset digest;
- the corresponding `geoip.dat` and `geosite.dat`;
- a compact ASN database built from a pinned `ipverse/as-ip-blocks` commit;
- a Debian bookworm slim runtime with CA certificates and nftables dependencies.

Base images, rw-core, and the ASN source are pinned by digest or checksum. Debian `apt` packages are not pinned to a package snapshot, so two builds are not guaranteed to be byte-for-byte identical. Use the manifest digest, SBOM, provenance, and attestation together when identifying a formal artifact.
