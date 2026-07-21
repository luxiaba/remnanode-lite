# Versioning and Image Tags

[Back to the documentation index](README.md)

Remnanode Lite tracks four versions: the project release, the official Node
contract, the Panel used for integration testing, and the bundled rw-core. They
do not always move together, so a single "current version" would be ambiguous.

This document is the authoritative naming and image-channel policy. The operational procedure is defined in the [Release process](release.md).

## Four independent dimensions

| Dimension | Source of truth | Meaning |
| --- | --- | --- |
| Project `Version` | `internal/version/version.go` | Identity of this project's source, binaries, GitHub Release, and exact image tag |
| Official `ContractVersion` | `internal/version/contract.version` and pinned source evidence | Official Node behavior actually implemented and reported to Panel |
| Panel integration target | Release documentation and maintainer verification | Panel version used for integration verification, not a compile-time version |
| rw-core version | `Dockerfile`, installers, and Release metadata | Core version packaged and verified in the image or native installation |

`Version` and `ContractVersion` are deliberately separate. The project can begin a future development line or continue improving an already aligned contract without claiming a new official behavior baseline.

For example:

```text
Version:         2.8.1-rnl.1
ContractVersion: 2.8.0
```

This means Remnanode Lite has entered its `2.8.1` project line while the only proven and reportable official Node contract remains `2.8.0`. `ContractVersion` may change to `2.8.1` only after pinning official 2.8.1 source, reviewing the contract delta, updating the implementation, and completing verification.

## Project version formats

Formal project versions use one of two formats.

### Independent iteration: `X.Y.Z-rnl.N`

`rnl.N` is a Remnanode Lite iteration number. It is not the Nth revision of an official Release and does not by itself assert compatibility with official `X.Y.Z`.

Use it to:

- begin a project line before the corresponding official version is available;
- fix defects, improve architecture, or reduce resource use on an existing contract baseline;
- publish a verified build that contains project-specific evolution;
- distinguish this project's build from an official version with the same numeric prefix.

Within one `X.Y.Z` namespace, `N` starts at 1 and increases monotonically. A published number cannot be reused, and existing Git and exact image tags cannot be moved.

The first three components identify the project's development or Release line. They do not automatically equal `ContractVersion`; the release metadata must state the actual contract baseline separately.

### Officially aligned milestone: `X.Y.Z`

A version without `rnl.N` is reserved for a formal Release that has completed behavioral alignment with the same official version. Before publishing it:

- `ContractVersion` must be the same `X.Y.Z`;
- the official source version and immutable commit must be pinned;
- contract comparison and implementation work must be complete;
- required automated gates and real-environment verification must pass;
- the release documentation must identify Panel, rw-core, architectures, and known limitations.

A plain `X.Y.Z` tag is an immutable alignment milestone, not a moving label for later fixes. Further project-specific work may be released as `X.Y.Z-rnl.N`.

## Example timeline

These examples explain naming only; they do not claim that the listed versions exist:

```text
2.8.1-rnl.1  Begin the project 2.8.1 line while the contract may remain 2.8.0
2.8.1-rnl.2  Continue implementation or fixes
2.8.1        Complete formal alignment with official Node 2.8.1
2.8.1-rnl.3  Continue independent improvements on that contract
2.8.1-rnl.9  Publish a later verified build that latest may reference
2.8.2-rnl.1  Begin the next project line and report the contract actually implemented
```

Under Semantic Versioning, `rnl.N` is a prerelease identifier, so `2.8.1-rnl.9` sorts below plain `2.8.1` even when published later. This project does not use SemVer ordering to select the newest stable build. The Release workflow explicitly controls GitHub's Latest Release and GHCR `latest`.

## Git tags and container tags

Formal Git tags use a `v` prefix; image version tags do not:

```text
Git tag:       v2.8.1-rnl.9
Container tag: ghcr.io/luxiaba/remnanode-lite:2.8.1-rnl.9
```

Project policy makes both tags immutable, but they identify different objects:

- The formal Git tag points to the current `main` head at publication.
- The exact image tag points to the manifest digest already built and attested for that same commit.

The Release workflow does not rebuild the container. It resolves the immutable
`sha-<commit>` candidate, verifies its manifest and attestation, and promotes
that exact digest.

## Image channels

| Reference | Mutability | Source | Intended use |
| --- | --- | --- | --- |
| `sha-<40-character-commit>` | Refuses a different value after first publication | Candidate for a specific `main` commit and attested manifest digest | Verification, exact reproduction, and diagnosis |
| `edge` | Moving | Latest eligible `main` container build | Mainline observation, never a stability promise |
| `X.Y.Z-rnl.N` | Immutable by policy | Corresponding independent project Release | Exact deployment and rollback of a verified project version |
| `X.Y.Z` | Immutable by policy | Corresponding officially aligned Release | Exact deployment and rollback of an alignment milestone |
| `latest` | Moving | Most recent formal Release that completed the stable workflow | Opt-in tracking of the recommended stable build |
| `name@sha256:...` | Content addressed | Registry manifest digest | Strongest production pin and supply-chain verification |

A manual container workflow run on `main` uses the same `sha-<commit>` identity.
It must not replace an existing tag with a different manifest digest.

### Meaning of `latest`

`latest` means the build this project currently recommends after completing the required stable Release workflow. It may point to a plain `X.Y.Z` alignment milestone or to a later `X.Y.Z-rnl.N` project Release.

Consequently:

- `latest` does not mean "identical to the newest official Node";
- `latest` never points to `edge` and is not updated by an ordinary `main` push;
- rebuilding or repairing an older Release must not move `latest` backward;
- only the formal Release workflow may move it, and only after all promotion guards succeed;
- changing `latest` does not replace a running container automatically.

Creating a formal tag means the maintainer intends that version to enter the stable channel. A build that is not ready to become the recommended stable Release must remain in the `sha-*` candidate channel.

A server configured with `latest` still requires an explicit update:

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

## Choosing a deployment reference

### Fixed production version

Use an exact project version or manifest digest:

```text
ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N
ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>
```

Both support reviewed changes, staged rollout, and prompt rollback. An exact version is readable and sufficient for most nodes. A digest is appropriate when the deployment must remain fixed even if a registry tag is moved accidentally.

### Opt-in stable tracking

Use:

```text
ghcr.io/luxiaba/remnanode-lite:latest
```

This is suitable only when operators review the release changes and deliberately pull and verify each update. `latest` is an update channel, not a rollback identity. Keep the previous exact version or digest for rollback.

### Candidate verification

Verification starts with `sha-<40-character-commit>`. Resolve the tag to a
manifest digest before testing, use deployment files from the same commit, and
keep that digest fixed through promotion. Do not test `edge` as the release
candidate; a later `main` build can move it.

## Publication and stability

Changing `Version`, merging into `main`, or building a candidate image does not publish a formal Release. Publication requires at least:

1. consistent project, contract, and dependency metadata;
2. the prescribed code checks and maintainer verification of the current `main` candidate with a real Panel and real traffic;
3. a formal Git tag that is immutable under project policy;
4. a GitHub Release with binary assets and promotion of the accepted digest to the exact GHCR version;
5. verification of the candidate image digest and build attestation against its source commit;
6. release metadata with the actual compatibility scope and known risks;
7. successful promotion of that digest to GHCR `latest` and marking the corresponding GitHub Release as Latest.

A version string in the repository may be only a development target. Check the
Git tag, GitHub Release, and exact GHCR tag to see whether it has been published.
The source currently says `2.8.0`, but that string alone is not a release.

## Synchronizing an official version

When official Node publishes a new Release, automation only detects the change and opens an Issue. It does not modify `ContractVersion`, source code, or image tags. Synchronization requires:

1. Pin the official version and immutable commit.
2. Audit route, schema, error, side-effect, and plugin-dependency changes.
3. Update versioned contract evidence and tests.
4. Adjust the Go implementation and complete code regression testing.
5. Verify the candidate with the target Panel, rw-core, and Linux environments.
6. Update `ContractVersion` according to the verified result.
7. Choose a plain aligned version or an appropriate `rnl.N` project version for publication.

An early project line cannot skip steps 2 through 6 and report a contract it has not implemented.

## Version output and Release metadata

The binary prints both identities:

```text
remnanode-lite <Version> (contract <ContractVersion>)
```

The repository's changelog and release metadata should make these facts clear:

- project version and Git tag;
- release commit and image manifest digest;
- `ContractVersion` and the pinned official source commit;
- Panel version used for verification;
- packaged rw-core version and asset digests;
- `amd64` and `arm64` support status;
- resource-verification scope;
- known differences and rollback procedure;
- image manifest digest and verification command.

GitHub generates the Release notes from merged changes. The project does not
maintain a separate per-version Release note file in the repository, and it
does not commit host inventories, smoke JSON, logs, or other runtime data.

The `NODE_CONTRACT_VERSION` override is for controlled diagnostics and emergency
compatibility tests only. It changes none of the implemented behavior, source
evidence, binary identity, or Release claims and must never be used to claim
compatibility that the code has not earned.
