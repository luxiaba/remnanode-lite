# Docker Compose Deployment

[Back to the documentation index](README.md)

Docker Compose is the preferred deployment method for low-memory nodes. A server needs only a permission-restricted YAML file and Docker Engine; it does not need the source tree, a Go toolchain, or a persistent log volume.

The primary workflow on this page uses the single-file Compose layout suited to fleets of independent small nodes. The repository-root `compose.yaml` plus `.env` remains available for centralized configuration or local source builds.

## Deployment model

The container has one application supervisor: `remnanode-lite` starts and reaps rw-core directly, without s6 or another resident service supervisor. Compose enables a minimal init for PID 1 duties, while the Node and rw-core share one container cgroup with:

- a `448 MiB` hard memory limit and no additional swap;
- `1 CPU` and `256 PIDs`;
- a read-only root filesystem;
- tmpfs mounts for `/run/remnanode`, `/tmp`, and `/var/log/remnanode`, capped at `48 MiB` in total;
- Docker `json-file` rotation of `2 MiB x 2` for Node logs;
- no persistent data volume. Recreating the container clears runtime configuration copies and logs, and the Panel sends the Xray configuration again.

These limits reserve host capacity for the whole-machine target of `512 MiB RAM / 1 vCPU / 2 GB disk`. They are not an SLA for every traffic pattern or plugin combination. See the [resource budget](development/resource-budget.md) for measurements and boundaries.

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
| `sha-<40-character-commit>` | Candidate built for a `main` commit | Acceptance on real servers |
| `candidate-sha-<40-character-commit>` | Independently rebuilt candidate manually dispatched from `main` | Acceptance when the automatic candidate is absent or must be rebuilt |
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

Choose the entry point for the current release stage. Before the first formal Release, or while accepting a candidate, bind the deployment file and candidate image to the same full commit. After a formal version is published, prefer the Compose asset attached to that Release and covered by its `SHA256SUMS`.

The maintained source template is [`deploy/compose.single-file.yaml`](../deploy/compose.single-file.yaml).

### Before the first Release or during candidate acceptance

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

  mkdir -p /opt/remnanode
  cd /opt/remnanode
  curl -fL \
    "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${candidate_commit}/deploy/compose.single-file.yaml" \
    -o docker-compose.yaml
  sed -i \
    "s|ghcr.io/luxiaba/remnanode-lite:latest|ghcr.io/luxiaba/remnanode-lite:${candidate_tag}|" \
    docker-compose.yaml
  chmod 600 docker-compose.yaml
)
```

Choose an existing full automatic `sha-<40-character-commit>` candidate or a manual `candidate-sha-<40-character-commit>` candidate from the [GHCR package](https://github.com/luxiaba/remnanode-lite/pkgs/container/remnanode-lite), then place the complete tag in the variable. A placeholder, abbreviated commit, or different tag fails before download. This keeps the Compose content and image source at the same commit. Once acceptance begins, also record and pin the resolved manifest digest. Do not retag it as a formal version before acceptance is complete.

### Formal Release

Download the single-file asset and checksums from the same GitHub Release:

```bash
VERSION=X.Y.Z-rnl.N # or X.Y.Z
BASE_URL="https://github.com/luxiaba/remnanode-lite/releases/download/v${VERSION}"

mkdir -p /opt/remnanode
cd /opt/remnanode
curl -fL "${BASE_URL}/docker-compose.single-file.yaml" -o docker-compose.yaml
curl -fLO "${BASE_URL}/SHA256SUMS"
grep -F ' docker-compose.single-file.yaml' SHA256SUMS \
  | sed 's|docker-compose.single-file.yaml|docker-compose.yaml|' \
  | sha256sum --check --strict
chmod 600 docker-compose.yaml
```

The production Linux procedure uses GNU `sha256sum`; the macOS `shasum` command is not part of this server deployment path.

The release workflow pins `image:` in this asset to the corresponding exact version instead of `latest`. After download, only the Node port and Secret need to be supplied. Change it to `latest` explicitly only when stable-channel tracking is intentional.

Edit these fields:

```yaml
image: ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N

environment:
  NODE_PORT: "38329"
  SECRET_KEY: "PASTE_THE_COMPLETE_BASE64_VALUE_FROM_THE_PANEL"
  LOW_MEMORY: "1"
```

The version above illustrates the format. Replace it with an exact version, `sha-*` candidate, or digest that actually exists in GHCR.

### Secret syntax

Environment variables must use a YAML mapping:

```yaml
environment:
  SECRET_KEY: "eyJ..."
```

Do not use this list form:

```yaml
environment:
  - SECRET_KEY="eyJ..."
```

In the list form, the quotes become part of the value and commonly produce:

```text
decode SECRET_KEY: illegal base64 data at input byte 0
```

A single-file deployment exposes the Secret in the Compose file and local `docker inspect` metadata. Keep the file at mode `0600`, and restrict access to the Docker socket, backups, and host administration. Before launching rw-core, the Node removes the Panel Secret from the child environment.

## Start and verify

```bash
cd /opt/remnanode
docker compose config --quiet
docker compose pull
docker compose up -d --no-build
docker compose ps
docker compose logs --tail=100 remnanode
ss -H -lnt "sport = :38329"
```

Do not run `docker compose config` without `--quiet` in automation logs; it expands and prints the inline Secret.

A `healthy` container proves that the healthcheck actively connected to the internal configuration Unix socket within two seconds, so the Node was accepting internal connections. It does not prove that:

- the Panel can reach the Node over the network;
- mTLS, JWT, or the Secret is correct;
- rw-core is online;
- proxy inbound ports sent by the Panel are reachable.

It is normal for rw-core to be offline immediately after a Node restart. The Node does not restore an old Panel configuration from disk. A later Panel health cycle calls `/node/xray/start` again. Complete verification in the Panel and test representative proxy traffic.

## Migrate from the official container

The `NODE_PORT` and complete `SECRET_KEY` used by the official `remnawave/node` image remain valid. They belong to the external Panel-to-Node contract, not to the official image's Node.js and s6 internals. Do not run both containers during migration: host networking would make them compete for the Node API and proxy inbound ports.

1. Back up the existing Compose file and record the exact official image version as the rollback target.
2. Replace the service definition with the complete single-file template from this page. Preserve at least host networking, both capabilities, resource limits, the read-only root filesystem, tmpfs mounts, and log limits.
3. Reuse the original `NODE_PORT` and Secret, but convert `environment` to a YAML mapping and pin the image to a real project version, `sha-*` candidate, or digest.
4. Pull and force-recreate the container under the same service name. Compose stops the old container before creating the replacement.

```bash
cd /opt/remnanode
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode
```

5. Confirm that the node returns online in the Panel, rw-core starts, and representative proxy traffic works. This implementation writes rw-core logs to `/var/log/remnanode/xray.out.log` and `/var/log/remnanode/xray.err.log`, not the official container's `/var/log/xray/current`.

There is no container runtime state or Xray configuration volume to migrate; the Panel sends the configuration again. To roll back, restore the backed-up Compose file and exact official image, then repeat pull and recreate. Keep the backup until the new container has completed its observation period.

## Candidate image automation

When a merge to `main` changes a container build input, the `container` workflow builds a multi-architecture manifest and produces build provenance. Only after those steps succeed does it publish the policy-immutable `sha-<commit>` tag. It moves `edge` only if the commit is still the current `main` HEAD. A candidate has no GitHub Release assets and is not a formal release. Use the earlier candidate-acceptance procedure to deploy it.

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
  --signer-workflow luxiaba/remnanode-lite/.github/workflows/container.yml
```

A tag states which version you intend to reference. A digest identifies the bytes actually deployed. A controlled fleet rollout should record the digest.

## Update and rollback

Back up the current YAML, change `image:`, then pull and recreate explicitly:

```bash
cp -p docker-compose.yaml docker-compose.yaml.previous
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode
```

To roll back, restore the previously verified YAML or change `image:` back to the previous exact version or digest, then repeat `pull` and `up`. Never implement rollback by moving an old version tag.

`latest` does not replace a running container. Tracking it still requires periodic, explicit pull and recreate operations, with the previous digest recorded before each update.

## Fleet rollout

Deploy a fleet only from one manifest digest that has completed M8 acceptance. Exact version tags are readable, but deployment records must still retain `name@sha256:...`. Do not send `latest` or `edge` directly to every node.

1. Group nodes by architecture, distribution, region, and primary traffic profile. Record the current digest, target digest, and rollback Compose file for every node.
2. Start with a small canary group covering real `amd64`, `arm64`, and representative network environments. Observe at least one traffic peak. Confirm that the Panel remains connected, rw-core resynchronizes, real proxy traffic succeeds, and there is no OOM, unexpected restart, zombie process, or sustained disk or log growth.
3. Expand in stages of approximately `5%`, `25%`, and `50%`, then deploy the remainder. Finish each observation period before continuing. Keep each batch small enough to restore its previous digest within the same maintenance window.
4. At every stage, sample container health, Panel state, proxy traffic, restart and OOM counts, memory, PIDs, disk, and Xray or nft errors. The deployment system must map nodes to digests, not only to a moving tag.
5. Stop expansion immediately if a stage shows unexplained node loss, proxy failure, repeated Xray startup failure, OOM, unexpected restart, zombies, resource-limit violations, or a clustered increase in similar errors. Roll back that batch first, then preserve logs and their digest association for diagnosis.

Rollback does not depend on moving a registry tag. Restore each node's recorded previous Compose file or digest, run `pull` and `up --force-recreate`, and confirm Panel connectivity and real traffic again. Until the issue has a clear conclusion, do not continue with untouched nodes or prune the previous image from canaries.

Each formal Release must link its completed M8 evidence manifest from its release note. If that record is absent, treat the image as an acceptance input, not authorization for an unobserved fleet rollout.

## Optional `.env` layout

To separate non-sensitive Compose structure from node parameters, download `compose.yaml`, the environment template, and checksums from the same formal GitHub Release. Do not combine a future `main` Compose file with an older image:

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

Keep `REMNANODE_IMAGE` at the exact version from that Release, or replace it with a verified manifest digest. See the [configuration reference](configuration.md) for every variable.

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
docker compose logs -f remnanode
```

Follow rw-core logs:

```bash
docker exec -it remnanode tail -n 50 -F /var/log/remnanode/xray.out.log
docker exec -it remnanode tail -n 50 -F /var/log/remnanode/xray.err.log
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

Base images, rw-core, and the ASN source are pinned by digest or checksum. Debian `apt` packages are not currently pinned to a snapshot and exact package versions, so the image is not claimed to be byte-for-byte reproducible. Identify every formal artifact by its manifest digest together with its SBOM, provenance, and attestation.
