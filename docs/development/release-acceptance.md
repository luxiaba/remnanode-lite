# M8 Release Acceptance Evidence Protocol

[Back to development documentation](README.md) · [General release process](../release.md)

This protocol defines the machine-verifiable acceptance record for `2.8.0-rnl.1`. It does not replace real testing. Its purpose is to bind every result to one code candidate and prevent code drift after acceptance. No candidate has been frozen and no evidence has been produced yet; this document defines the protocol, not an acceptance result.

This is a versioned acceptance profile for the first release line, not a universal template that can be reused unchanged. Changes to the project version, official contract, Panel, rw-core, route count, operating systems, or resource policy require the validator and this protocol to be updated in a normal code PR before a new candidate is frozen.

## Candidate freeze

Commit all Go code, tests, scripts, workflows, deployment assets, and governance changes first. The resulting 40-character commit and tree are candidate `C`. Every `candidateCommit` must equal `C`, and no evidence run may begin before the candidate commit time. After the candidate container is built, record the immutable registry manifest digest as `candidateImageDigest`. Every subsequent container acceptance run and the final release must use that digest, not a movable tag.

Acceptance binaries must be built from a clean checkout of `C` by `scripts/build-release-binaries.sh`. The script requires exactly `go1.26.5`, disables workspaces and automatic toolchain drift, clears build options that could alter artifacts, and fixes `CGO_ENABLED=0`, architecture levels, `-trimpath`, release ldflags, and `-buildvcs=false`. The final release gate rebuilds both architectures with the same script and compares their SHA-256 digests.

After acceptance, changes are restricted to `README.md`, root `CHANGELOG.md`, `docs/development/roadmap.md`, `docs/development/acceptance/v2.8.0-rnl.1/`, and `docs/releases/v2.8.0-rnl.1.md`. The validator requires `C` to be an ancestor of final HEAD, inspects every post-candidate commit and parent, enforces this path allowlist, and rejects merge commits during finalization. Reverting an out-of-scope code change does not make the history acceptable.

On a protected branch, merge all code to `main` through a PR and use the resulting `main` commit as `C`. Create a separate acceptance-material branch from `C` and freeze `main` while acceptance is running. The final evidence PR must use squash merge so that exactly one single-parent, allowlisted finalization commit follows `C`. The validator rejects zero or multiple finalization commits, ordinary merge commits, and any change outside the allowlist.

## File layout

```text
docs/development/acceptance/v2.8.0-rnl.1/
  manifest.json
  systemd.json
  openrc.json
  panel.json
  compose.json
  resource-fault.json
```

All six files must be tracked, non-executable regular files no larger than `1 MiB`. The worktree and index must exactly match the HEAD blobs. JSON keys are case-sensitive; duplicate and unknown fields are rejected. `manifest.json` records the SHA-256 digest of each of the other five files.

## Manifest

The manifest fixes the following release boundary:

- `releaseVersion=2.8.0-rnl.1`, `releaseTag=v2.8.0-rnl.1`, and `decision=pass`.
- `candidateCommit`, `candidateTree`, and an RFC 3339 `acceptedAt` timestamp.
- `candidateImageDigest` must be the candidate multi-platform manifest digest returned by the registry, exactly `sha256:` followed by 64 lowercase hexadecimal characters. A tag, an image config digest, or a single-platform layer digest is not a substitute.
- Official Node `2.8.0@596f015a5c8f876dc9a9d61b6cb78d35bd8e379b`.
- Panel `2.8.1`.
- rw-core `v26.6.27@45cf2898ab12e97a55dd8f1f3d78d903340bdc9e`.
- Pinned asset SHA-256 values: amd64 `b3e5902d06d6282fe53cfa2fc426058b9aeaa429b2c812e20887cd47f26d08bf` and arm64 `13a251379bea366c2cf10363ad71e75734193d401f26f518bf0c25e5c8f8c931`.
- Policy: 512 MiB whole-machine memory, 448 MiB service memory, 1 CPU, 2048 MiB disk, 50,000 users, no swap, and a soak of at least 86,400 seconds.
- Evidence must contain exactly `systemd`, `openrc`, `panel`, `compose`, and `resource-fault`.

Risk severity is limited to `P1`, `P2`, or `P3`, and status to `open` or `closed`. Any `releaseBlocking=true` risk, or any open P1/P2 risk, rejects the release.

## Native platform evidence

`systemd.json` and `openrc.json` record:

- `schemaVersion`, kind, candidate identity, pass status, start and finish times, and the actual command.
- OS identifier and version, init system, architecture, kernel, memory, CPU, and disk.
- Node version output and the SHA-256 of the installed binary.
- rw-core version, commit, pinned download SHA-256, and installed binary SHA-256.
- Fresh install, repeat install, start/stop/restart, successful upgrade, failed-upgrade rollback, Panel resynchronization after reboot, capability boundaries, uninstall isolation, nftables namespace checks, and socket-kill namespace checks.
- `checks.rwCoreProcessGroupCleanup=true`, established with a wrapper and child process: a separate PGID, whole-group SIGINT/SIGKILL behavior during normal stop, and cleanup of descendants after the leader exits naturally. Automatic recovery after the Node or supervisor itself is forcibly killed is outside this check.

The environments are fixed to Ubuntu 24.04/systemd and Alpine 3.22/OpenRC. Together, the two records must cover both amd64 and arm64.

## Panel evidence

`panel.json` fixes Panel `2.8.1` and requires both `systemd` and `openrc` targets. Its `artifacts` entries record each target's architecture, Node binary SHA-256, and rw-core binary SHA-256; these identities must exactly match the corresponding native evidence. All 26 routes must pass with zero semantic mismatches, covering node registration, Xray lifecycle, statistics, user mutations, and plugin synchronization.

`checks.lifecyclePluginSerialization=true` must be demonstrated through concurrent `start/stop` and `sync/recreate` interleavings. The transport lifecycle gate must remain outermost, the Plugin operation gate and Manager ownership must never overlap in reverse order, and cancellation while waiting must terminate the request.

## Docker Compose evidence

`compose.json` proves that the final Compose template intended for small-node deployment actually ran with the final GHCR candidate. It is not a static restatement of YAML. Export `deploy/compose.single-file.yaml` from `C`, inject only the real Secret and node port, and replace `image` with `ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}`. Do not weaken any isolation or resource constraint. Neither the Secret nor rendered Compose output belongs in evidence.

The evidence must satisfy every requirement below:

- `candidateImageDigest` must exactly match the manifest. `source.path` is fixed to `deploy/compose.single-file.yaml`. The validator reads that path directly from the candidate Git object and compares its byte-level SHA-256 with `source.sha256`; it does not trust the current checkout.
- After provenance and SBOM descriptors are excluded, top-level `manifestPlatforms` must be exactly `linux/amd64` and `linux/arm64`. `runs` must contain exactly two complete records whose `environment.arch` values cover `amd64` and `arm64` once each. A manifest declaration cannot replace a real run on either architecture.
- Each run records the actual Docker Engine and Docker Compose versions. They must be non-empty; unrelated patch-version upgrades are not release blockers.
- Each run's `hostResources` must describe the actual Linux host visible to Docker: memory in `480..512 MiB`, exactly `1` CPU, total Docker-filesystem capacity in `1792..2048 MiB`, and explicit zero swap. At least `256 MiB` must remain available at the measured project peak. Every field is required, including valid zero values. These tolerances account for kernel reservations and decimal/binary disk reporting; they do not permit a 448 MiB cgroup test on a larger host to be presented as whole-machine acceptance.
- Each run's `limits` comes from Docker inspect or cgroup state, not from the Compose source: memory `469762048` bytes, memory plus swap `469762048` bytes, `nanoCPUs=1000000000`, and `pidsLimit=256`.
- Each run's `isolation` proves a read-only root filesystem, no-new-privileges, `init: true`, and `docker-init` or `tini` as PID 1 with successful orphan reaping. Capabilities must drop `ALL`; configured and effective capabilities may contain only `NET_ADMIN` and `NET_BIND_SERVICE`. Strip a conventional `CAP_` prefix from kernel-derived names before recording them. Actual mounts must include writable tmpfs instances at `/run/remnanode=4 MiB`, `/tmp=16 MiB`, and `/var/log/remnanode=28 MiB`, each with `noexec,nosuid,nodev`.
- Each run's `health` observes at least one `healthy` state and exit code `0`. Because `restart: unless-stopped` does not restart an unhealthy container by itself, health success and fault recovery are separate checks.
- Each run's `lifecycle` records baseline PIDs, a bounded stress peak, and PIDs after recovery. The peak must exceed baseline without exceeding 256; the recovered value must be below the peak, and zombie count must be zero. Exact return to baseline is not required because Go/runtime and rw-core thread counts can legitimately vary. A normal `docker compose stop` must finish through the application within 35 seconds, exit with code 0, contain no SIGKILL event, and leave zero PIDs.
- Each run's `logs` must demonstrate the `json-file` driver, `2 MiB x 2` rotation settings, and observed rotation. The active file may briefly cross the threshold at a rotation boundary, but the peak across all project container log files may not exceed `6 MiB`.
- Each run must pull, start, and confirm a rollback image as healthy. The first release may use `docker.io/remnawave/node`; later releases may use the previous stable `ghcr.io/luxiaba/remnanode-lite` image. Both runs must use the same rollback repository and multi-platform manifest digest. `rollbackImageDigest` must be valid and differ from the candidate, and the rollback image must remain present while storage peak is measured. `projectDiskPeakMiB` includes the candidate, rollback image, writable layer, Docker JSON logs, and project temporary files and may not exceed `1024 MiB`. Its bytes plus the measured available bytes at peak may not exceed total disk. Run storage acceptance only on a dedicated Docker environment with no unrelated containers or images; do not guess shared-layer ownership and do not let acceptance automation prune unknown objects.

The field shape below is normative. Replace angle-bracket placeholders with measured values:

```json
{
  "schemaVersion": 1,
  "kind": "compose",
  "candidateCommit": "<40-lowercase-hex>",
  "status": "pass",
  "startedAt": "<RFC3339>",
  "finishedAt": "<RFC3339>",
  "command": ["<repeatable-compose-acceptance-runner>"],
  "candidateImageDigest": "sha256:<64-lowercase-hex>",
  "source": {
    "path": "deploy/compose.single-file.yaml",
    "sha256": "<64-lowercase-hex>"
  },
  "manifestPlatforms": ["linux/amd64", "linux/arm64"],
  "runs": [
    {
      "environment": {
        "dockerEngineVersion": "<actual-amd64-version>",
        "dockerComposeVersion": "<actual-amd64-version>",
        "arch": "amd64"
      },
      "hostResources": {
        "memoryTotalBytes": 524288000,
        "cpuCount": 1,
        "diskTotalBytes": 2097152000,
        "diskAvailableAtPeakBytes": 536870912,
        "swapTotalBytes": 0
      },
      "limits": {
        "memoryLimitBytes": 469762048,
        "memorySwapLimitBytes": 469762048,
        "nanoCPUs": 1000000000,
        "pidsLimit": 256
      },
      "isolation": {
        "readOnlyRootfs": true,
        "noNewPrivileges": true,
        "initEnabled": true,
        "initPid": 1,
        "initProcess": "docker-init",
        "orphanReapingPassed": true,
        "capDrop": ["ALL"],
        "capAdd": ["NET_ADMIN", "NET_BIND_SERVICE"],
        "effectiveCapabilities": ["NET_ADMIN", "NET_BIND_SERVICE"],
        "tmpfs": [
          {"target": "/run/remnanode", "sizeBytes": 4194304, "writable": true, "noexec": true, "nosuid": true, "nodev": true},
          {"target": "/tmp", "sizeBytes": 16777216, "writable": true, "noexec": true, "nosuid": true, "nodev": true},
          {"target": "/var/log/remnanode", "sizeBytes": 29360128, "writable": true, "noexec": true, "nosuid": true, "nodev": true}
        ]
      },
      "health": {"status": "healthy", "checkExitCode": 0, "consecutiveSuccesses": 3},
      "lifecycle": {
        "gracefulStop": true,
        "forcedKill": false,
        "exitCode": 0,
        "pidsBaseline": 8,
        "pidsPeak": 15,
        "pidsAfterRecovery": 8,
        "pidsAfterStop": 0,
        "zombiesAfterRecovery": 0
      },
      "logs": {"driver": "json-file", "maxSizeBytes": 2097152, "maxFiles": 2, "rotationObserved": true, "peakBytes": 3145728},
      "storage": {
        "rollbackImageRepository": "docker.io/remnawave/node",
        "rollbackImageDigest": "sha256:<different-64-lowercase-hex>",
        "rollbackImagePulled": true,
        "rollbackImageStarted": true,
        "rollbackImageHealthy": true,
        "rollbackImagePresentAtPeak": true,
        "projectDiskPeakMiB": 350
      }
    },
    {
      "environment": {
        "dockerEngineVersion": "<actual-arm64-version>",
        "dockerComposeVersion": "<actual-arm64-version>",
        "arch": "arm64"
      },
      "hostResources": {
        "memoryTotalBytes": 524288000,
        "cpuCount": 1,
        "diskTotalBytes": 2097152000,
        "diskAvailableAtPeakBytes": 536870912,
        "swapTotalBytes": 0
      },
      "limits": {
        "memoryLimitBytes": 469762048,
        "memorySwapLimitBytes": 469762048,
        "nanoCPUs": 1000000000,
        "pidsLimit": 256
      },
      "isolation": {
        "readOnlyRootfs": true,
        "noNewPrivileges": true,
        "initEnabled": true,
        "initPid": 1,
        "initProcess": "docker-init",
        "orphanReapingPassed": true,
        "capDrop": ["ALL"],
        "capAdd": ["NET_ADMIN", "NET_BIND_SERVICE"],
        "effectiveCapabilities": ["NET_ADMIN", "NET_BIND_SERVICE"],
        "tmpfs": [
          {"target": "/run/remnanode", "sizeBytes": 4194304, "writable": true, "noexec": true, "nosuid": true, "nodev": true},
          {"target": "/tmp", "sizeBytes": 16777216, "writable": true, "noexec": true, "nosuid": true, "nodev": true},
          {"target": "/var/log/remnanode", "sizeBytes": 29360128, "writable": true, "noexec": true, "nosuid": true, "nodev": true}
        ]
      },
      "health": {"status": "healthy", "checkExitCode": 0, "consecutiveSuccesses": 3},
      "lifecycle": {
        "gracefulStop": true,
        "forcedKill": false,
        "exitCode": 0,
        "pidsBaseline": 8,
        "pidsPeak": 15,
        "pidsAfterRecovery": 8,
        "pidsAfterStop": 0,
        "zombiesAfterRecovery": 0
      },
      "logs": {"driver": "json-file", "maxSizeBytes": 2097152, "maxFiles": 2, "rotationObserved": true, "peakBytes": 3145728},
      "storage": {
        "rollbackImageRepository": "docker.io/remnawave/node",
        "rollbackImageDigest": "sha256:<different-64-lowercase-hex>",
        "rollbackImagePulled": true,
        "rollbackImageStarted": true,
        "rollbackImageHealthy": true,
        "rollbackImagePresentAtPeak": true,
        "projectDiskPeakMiB": 350
      }
    }
  ]
}
```

Prefer objective collection sources over manual transcription:

```bash
git show "${C}:deploy/compose.single-file.yaml" | sha256sum
docker version --format '{{.Server.Version}}'
docker compose version --short
docker buildx imagetools inspect "ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}"
docker inspect remnanode
docker events --filter container=remnanode
```

Use the same versioned, repeatable runner on both architectures. It should read cgroup v1/v2 state, the container PID namespace, `/proc`, mount options, effective capabilities, Docker health and events, Docker log files, and the filesystem containing DockerRootDir. The orphan probe must confirm that a short-lived child is adopted and reaped by PID 1. PID pressure should create only a visible bounded peak, never approach 256. Graceful-stop evaluation must combine events, exit code, OOM state, and post-stop PID/cgroup state. Log-rotation load written to the main process's stdout must use fixed non-sensitive filler and must never replay real requests or logs.

The `command` array records the actual runner and non-sensitive arguments. Inject Secrets only through a restricted file descriptor or a local `0600` temporary file; never place them in commands, evidence, terminal output, or raw collection bundles. The validator can prove schema, candidate binding, and threshold consistency, but it cannot prove that a claimed command actually ran. The release owner must therefore retain the allowlisted raw collection bundles and SHA-256 digests from both dedicated acceptance hosts, and a second reviewer must verify them before evidence is committed. `["true"]`, manually copied booleans, or reading only the Compose source does not constitute M8 evidence.

## Resource and fault evidence

`resource-fault.json` uses the same Node and rw-core artifact identities as the native records and records 50,000 users, soak duration, cgroup peak, OOM kills, and project disk peak. The gate requires:

- At least 86,400 seconds of soak time.
- Peak memory no greater than 448 MiB.
- Zero OOM kills, project disk no greater than 2048 MiB, and no swap.
- Passing core-kill recovery including process-group descendant cleanup, Node restart, Panel disconnect, nftables failure and retry, bounded log-fault storms, and failed-upgrade rollback.

## Data boundaries

Record only allowlisted metrics, commands, and digests. Never commit the Secret Key, JWT, CA, client certificate, private key, IP addresses, hostnames, Panel URL, raw requests or responses, or reversible user data. Redact sensitive command arguments before recording them.

## Validation

```bash
go run ./cmd/release-evidence-check \
  -manifest docs/development/acceptance/v2.8.0-rnl.1/manifest.json \
  -tag v2.8.0-rnl.1
```

`scripts/release-check.sh` calls the same validator and then checks the release note, version, full repository gate, supply chain, and tag placement. Until genuine evidence exists, failure of the release gate is expected.
