# v2.8.0 Release Acceptance Evidence Protocol

[Back to development documentation](README.md) | [General release process](../release.md)

This document defines schema version 2 and the
`docker-production-smoke-v1` acceptance profile used to accept `v2.8.0`. It is
a version-specific release gate, not a reusable
claim that every packaged platform or deployment mode has completed production
runtime validation.

The profile keeps the release decision bound to one frozen source candidate,
one attested multi-architecture image digest, and one real low-memory Docker
smoke on native amd64/x86_64. Its runtime observations are attested by the
Release Owner. The validator can check JSON structure, Git ancestry, hashes,
artifact closure, measured thresholds, and the operator signoff, but it cannot
independently prove that a reported Panel session or traffic observation
occurred. The evidence is an auditable, candidate-bound record, not an
unforgeable proof.

## Candidate freeze

Commit all Go, test, script, workflow, deployment, and governance changes before
freezing the 40-character candidate commit `C` on `main`. Record its Git tree
and the immutable OCI manifest digest returned for the candidate image. All
smoke timestamps must be at or after the commit time of `C`.

The candidate image pipeline must build `linux/amd64` and `linux/arm64` in
one manifest and produce its SBOM, provenance, and GitHub build attestation.
This is the build and supply-chain requirement for both architectures. Only
`linux/amd64` receives blocking production runtime validation in this
profile.

Build the release Node binaries from a clean `C` with
`scripts/build-release-binaries.sh`. Record both binary SHA-256 values in the
manifest. The final release gate rebuilds both architectures and compares their
hashes with those candidate values; it does not execute the arm64 binary on the
amd64 release runner.

After the smoke, only the root README files, `CHANGELOG.md`, the English and
Chinese roadmaps, the two acceptance JSON files, and
`docs/releases/v2.8.0.md` may change. The validator requires exactly one
single-parent finalization commit after `C` and rejects any path outside that
allowlist.

## File layout

```text
docs/development/acceptance/v2.8.0/
  manifest.json
  docker-smoke.json
```

Both files must be tracked, non-executable regular files no larger than
`1 MiB`. The worktree and index must match the blobs in `HEAD`. JSON keys are
case-sensitive; duplicate and unknown fields are rejected.

## Manifest

`manifest.json` fixes the release and artifact identity:

- `schemaVersion=2` and
  `acceptanceProfile=docker-production-smoke-v1`.
- `releaseVersion=2.8.0`, `releaseTag=v2.8.0`, and `decision=pass`.
- The candidate commit, tree, OCI manifest digest, and RFC3339 acceptance time.
  `acceptedAt` must not be earlier than the finish time of the smoke evidence.
- SHA-256 values for the amd64 and arm64 Node binaries built from `C`.
- Official Node `2.8.0@596f015a5c8f876dc9a9d61b6cb78d35bd8e379b`.
- Panel `2.8.1`.
- rw-core `v26.6.27@45cf2898ab12e97a55dd8f1f3d78d903340bdc9e`
  and the audited amd64/arm64 asset SHA-256 values.
- The exact deferred-validation list below.
- One passing evidence reference of kind `docker-production-smoke`, whose path
  is `docs/development/acceptance/v2.8.0/docker-smoke.json` and whose SHA-256
  covers the complete file bytes.

The deferred list is ordered and exact:

```json
[
  "arm64-production-runtime",
  "native-systemd-install",
  "native-openrc-install",
  "50000-user-load",
  "24h-soak",
  "fault-and-rollback-injection"
]
```

These items are explicit limitations of the `v2.8.0` profile and are not
release blockers. They must not be described as having passed. The dated M6
50,000-user and M7 native-init engineering measurements remain useful
historical baselines, but they are not runtime evidence for `C`.

Risks use severity `P1`, `P2`, or `P3`, status `open` or `closed`, and a
required `releaseBlocking` boolean. A release-blocking risk or an open P1/P2
risk fails validation. Open, non-blocking P3 entries may describe the deferred
scope and its mitigation.

The normative manifest shape is:

```json
{
  "schemaVersion": 2,
  "acceptanceProfile": "docker-production-smoke-v1",
  "releaseVersion": "2.8.0",
  "releaseTag": "v2.8.0",
  "candidateCommit": "<40-lowercase-hex>",
  "candidateTree": "<40-lowercase-hex>",
  "candidateImageDigest": "sha256:<64-lowercase-hex>",
  "candidateNodeSha256": {
    "amd64": "<64-lowercase-hex>",
    "arm64": "<64-lowercase-hex>"
  },
  "acceptedAt": "<RFC3339>",
  "decision": "pass",
  "officialNode": {
    "version": "2.8.0",
    "commit": "596f015a5c8f876dc9a9d61b6cb78d35bd8e379b"
  },
  "panelTarget": {
    "version": "2.8.1"
  },
  "rwCore": {
    "version": "v26.6.27",
    "commit": "45cf2898ab12e97a55dd8f1f3d78d903340bdc9e",
    "sha256": {
      "amd64": "b3e5902d06d6282fe53cfa2fc426058b9aeaa429b2c812e20887cd47f26d08bf",
      "arm64": "13a251379bea366c2cf10363ad71e75734193d401f26f518bf0c25e5c8f8c931"
    }
  },
  "deferredValidation": [
    "arm64-production-runtime",
    "native-systemd-install",
    "native-openrc-install",
    "50000-user-load",
    "24h-soak",
    "fault-and-rollback-injection"
  ],
  "evidence": [
    {
      "kind": "docker-production-smoke",
      "path": "docs/development/acceptance/v2.8.0/docker-smoke.json",
      "sha256": "<64-lowercase-hex>",
      "status": "pass"
    }
  ],
  "risks": []
}
```

## Docker production smoke

Run `deploy/compose.single-file.yaml` from `C` on a real native
amd64/x86_64 Linux host. Change only the complete Panel Secret, node port, and
the image reference. The image must be pinned as
`ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}`. Do not relax the
template's resource, capability, filesystem, init, healthcheck, or logging
settings.

The final inspection must show that the same container has run for at least
600 seconds: the evidence `startedAt` must exactly equal the container's
Docker `.State.StartedAt`, and `finishedAt - startedAt` must be at least 600
seconds. A previous run with
`health=none`, a movable image tag, a non-canonical Compose file, or a
different candidate does not satisfy this profile.

`docker-smoke.json` records:

- Common evidence fields: schema, kind, candidate, pass status, the container
  start and final-inspection times, and the actual non-sensitive final
  inspection command.
- The candidate manifest digest and digest-pinned image reference.
- `deploy/compose.single-file.yaml` plus the SHA-256 of that file read from the
  candidate Git object, not the current checkout.
- A manifest platform set containing exactly `linux/amd64` and
  `linux/arm64`.
- Native `amd64` / `x86_64`, kernel, Docker Engine, and Docker Compose
  identities.
- A real host with 480..512 MiB memory, one CPU, 1792..2048 MiB total disk, and
  zero swap.
- Exact Node version output and the amd64 candidate binary SHA-256.
- The 64-character Docker container ID, its digest-pinned `.Config.Image`, and
  its exact `.State.StartedAt`; a running container named `remnanode`, healthy
  with healthcheck exit code zero and at least one consecutive success, zero
  OOM kills, and zero restarts.
- The actual Docker configuration closure: host network, `unless-stopped`,
  read-only rootfs, no-new-privileges, enabled init with `docker-init` or
  `tini` as PID 1, exact dropped/added capabilities, all three tmpfs mounts
  with sizes/modes/options, exact healthcheck, exact `json-file` options,
  nofile soft/hard limits, and the 35-second stop grace period.
- Actual limits of 448 MiB memory, 448 MiB memory plus swap, one CPU, and 256
  PIDs.
- Positive memory current/peak and PID current/peak observations, with current
  no greater than peak and peak no greater than the configured limit.
- Successful low-memory mode, ASN database loading, internal socket readiness,
  and listener readiness checks.
- Panel `2.8.1` connected and real proxy traffic passed.
- A SHA-256 for the retained, sanitized raw collection bundle.
- Release Owner signoff: operator `luxiaba`, role `release-owner`, decision
  `accept`.

The normative evidence shape is:

```json
{
  "schemaVersion": 2,
  "kind": "docker-production-smoke",
  "candidateCommit": "<same C as manifest>",
  "status": "pass",
  "startedAt": "<exact container .State.StartedAt RFC3339 value>",
  "finishedAt": "<RFC3339 at least 600 seconds after startedAt>",
  "command": ["docker", "inspect", "remnanode"],
  "candidateImageDigest": "sha256:<same digest as manifest>",
  "imageReference": "ghcr.io/luxiaba/remnanode-lite@sha256:<same digest>",
  "source": {
    "path": "deploy/compose.single-file.yaml",
    "sha256": "<SHA-256 of C:path>"
  },
  "manifestPlatforms": ["linux/amd64", "linux/arm64"],
  "environment": {
    "arch": "amd64",
    "unameMachine": "x86_64",
    "kernel": "<non-empty>",
    "dockerEngineVersion": "<non-empty>",
    "dockerComposeVersion": "<non-empty>"
  },
  "host": {
    "memoryTotalBytes": 536870912,
    "cpuCount": 1,
    "diskTotalBytes": 2147483648,
    "swapTotalBytes": 0
  },
  "node": {
    "versionOutput": "remnanode-lite 2.8.0 (contract 2.8.0)",
    "binarySha256": "<manifest candidateNodeSha256.amd64>"
  },
  "container": {
    "id": "<64-lowercase-hex Docker container ID>",
    "name": "remnanode",
    "imageReference": "ghcr.io/luxiaba/remnanode-lite@sha256:<same digest>",
    "startedAt": "<exactly equal to top-level startedAt>",
    "status": "running",
    "healthStatus": "healthy",
    "healthCheckExitCode": 0,
    "consecutiveHealthSuccesses": 1,
    "oomKilled": false,
    "restartCount": 0,
    "networkMode": "host",
    "restartPolicy": "unless-stopped",
    "readOnlyRootfs": true,
    "noNewPrivileges": true,
    "initEnabled": true,
    "initProcess": "docker-init",
    "capDrop": ["ALL"],
    "capAdd": ["NET_ADMIN", "NET_BIND_SERVICE"],
    "tmpfs": [
      {
        "target": "/run/remnanode",
        "sizeBytes": 4194304,
        "mode": "0700",
        "writable": true,
        "noexec": true,
        "nosuid": true,
        "nodev": true
      },
      {
        "target": "/tmp",
        "sizeBytes": 16777216,
        "mode": "1777",
        "writable": true,
        "noexec": true,
        "nosuid": true,
        "nodev": true
      },
      {
        "target": "/var/log/remnanode",
        "sizeBytes": 29360128,
        "mode": "0750",
        "writable": true,
        "noexec": true,
        "nosuid": true,
        "nodev": true
      }
    ],
    "healthcheck": {
      "test": ["CMD", "/usr/local/bin/remnanode-lite", "healthcheck"],
      "intervalSeconds": 30,
      "timeoutSeconds": 5,
      "startPeriodSeconds": 10,
      "retries": 3
    },
    "logging": {
      "driver": "json-file",
      "options": {"max-size": "2m", "max-file": "2"}
    },
    "nofile": {"soft": 1048576, "hard": 1048576},
    "stopGracePeriodSeconds": 35,
    "memoryLimitBytes": 469762048,
    "memorySwapLimitBytes": 469762048,
    "nanoCPUs": 1000000000,
    "pidsLimit": 256
  },
  "resources": {
    "memoryCurrentBytes": 1,
    "memoryPeakBytes": 1,
    "pidsCurrent": 1,
    "pidsPeak": 1
  },
  "checks": {
    "lowMemoryEnabled": true,
    "asnDatabaseLoaded": true,
    "internalSocketReady": true,
    "listenerReady": true
  },
  "panel": {
    "version": "2.8.1",
    "connected": true,
    "realTrafficPassed": true
  },
  "rawBundleSha256": "<64-lowercase-hex>",
  "signoff": {
    "operator": "luxiaba",
    "role": "release-owner",
    "decision": "accept"
  }
}
```

Record the container fields from the final Docker state; do not copy expected
values from the Compose source. Normalize Docker's nanosecond healthcheck
durations to seconds, parse `.HostConfig.Tmpfs` into the exact mount entries
above, and retain only a sanitized, allowlisted projection of the inspected
fields in the raw bundle. Never retain the complete `docker inspect` document
or `.Config.Env`, because an inline deployment exposes `SECRET_KEY` there.
Read `initProcess` from PID 1 inside the container. The container ID,
digest-pinned `.Config.Image`, and `.State.StartedAt` jointly identify the
container observed through the complete 600-second window.

## Evidence and data boundaries

The command array must not contain control characters or empty arguments. Its
case-insensitive argument text must also omit every validator-blocked fragment:
`secret`, `token`, `jwt`, `authorization`, `password`, `api-key`,
`apikey`, `panel-url`, `panel_url`, and `://`. This restriction applies
even to flag names and redacted placeholders. Inject the Panel Secret through a
restricted file descriptor or a local `0600` file.

The committed JSON and retained raw bundle must not contain a Secret Key, JWT,
CA, client certificate, private key, IP address, hostname, Panel URL, raw
request or response, or reversible user data. The bundle digest allows a
reviewer to bind a sanitized collection to the signoff; it does not make the
operator's account cryptographically self-proving. Build the bundle from
explicitly selected fields; redacting a complete inspect dump after collection
is not an acceptable substitute.

## Validation

```bash
go run ./cmd/release-evidence-check \
  -manifest docs/development/acceptance/v2.8.0/manifest.json \
  -tag v2.8.0
```

`scripts/release-check.sh` calls the same validator, rebuilds the candidate
release binaries, runs the full repository gate, and verifies final tag
placement and release-note requirements. Until a new candidate has completed
the canonical health-enabled smoke and genuine evidence is committed, failure
of the release gate is expected.
