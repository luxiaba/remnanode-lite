# Versioning and Image Tags

[Back to the documentation index](README.md)

Remnanode Lite has its own release cadence, an official Node contract baseline, a Panel integration target, and an rw-core runtime version. These dimensions change independently and must not be collapsed into one ambiguous "current version."

This document is the authoritative naming and image-channel policy. The operational procedure is defined in the [Release process](release.md).

## Four independent dimensions

| Dimension | Source of truth | Meaning |
| --- | --- | --- |
| Project `Version` | `internal/version/version.go` | Identity of this project's source, binaries, GitHub Release, and exact image tag |
| Official `ContractVersion` | `internal/version/contract.version` and pinned source evidence | Official Node behavior actually implemented and reported to Panel |
| Panel acceptance target | Acceptance records for the version | Panel version used for integration verification, not a compile-time version |
| rw-core version | `Dockerfile`, installers, and Release records | Core version packaged and verified in the image or native installation |

`Version` and `ContractVersion` are deliberately separate. The project can begin a future development line or continue improving an already aligned contract without claiming a new official behavior baseline.

For example:

```text
Version:         2.8.1-rnl.1
ContractVersion: 2.8.0
```

This means Remnanode Lite has entered its `2.8.1` project line while the only proven and reportable official Node contract remains `2.8.0`. `ContractVersion` may change to `2.8.1` only after pinning official 2.8.1 source, reviewing the contract delta, updating the implementation, and completing acceptance.

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

The first three components identify the project's development or Release line. They do not automatically equal `ContractVersion`; every Release note must state the actual contract baseline separately.

### Officially aligned milestone: `X.Y.Z`

A version without `rnl.N` is reserved for a formal Release that has completed behavioral alignment with the same official version. Before publishing it:

- `ContractVersion` must be the same `X.Y.Z`;
- the official source version and immutable commit must be pinned;
- contract comparison and implementation work must be complete;
- required automated gates and real-environment acceptance must pass;
- the Release note must identify Panel, rw-core, architectures, and known limitations.

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

Neither may be rewritten under project policy, but they identify different objects:

- The formal Git tag points to final Release-record commit `F`, which is the current `main` head at publication.
- The exact image tag points to the accepted manifest digest built from frozen candidate commit `C`.

The validator requires `F` to contain exactly one allowed Release-document commit after `C`. The Release workflow refuses tags from `dev`, a temporary branch, or a stale `main` commit, and it does not rebuild a second container from `F`. The acceptance manifest and Release note bind the image digest to `C`; the Git tag resolves `F`.

## Image channels

| Reference | Mutability | Source | Intended use |
| --- | --- | --- | --- |
| `sha-<40-character-commit>` | Refuses a different value after first publication | Automatic candidate for a specific `main` commit and attested manifest digest | Acceptance, exact reproduction, and diagnosis |
| `candidate-sha-<40-character-commit>` | Refuses a different value after first publication | Independent candidate from a manual workflow run on `main` | Acceptance when the automatic candidate is missing or a rebuild is required |
| `edge` | Moving | Latest eligible `main` container build | Mainline observation, never a stability promise |
| `X.Y.Z-rnl.N` | Immutable by policy | Corresponding independent project Release | Exact deployment and rollback of a verified project version |
| `X.Y.Z` | Immutable by policy | Corresponding officially aligned Release | Exact deployment and rollback of an alignment milestone |
| `latest` | Moving | Most recent formal Release that completed the stable workflow | Opt-in tracking of the recommended stable build |
| `name@sha256:...` | Content addressed | Registry manifest digest | Strongest production pin and supply-chain verification |

A manual run on `main` publishes only `candidate-sha-<commit>` and never overwrites an automatic `sha-<commit>`. Both may represent builds from the same source commit, but a manual rebuild is not guaranteed to have the same manifest digest as an earlier build. The tag is only a discovery alias. The accepted commit and the exact manifest digest are the canonical identities used for acceptance and promotion.

### Meaning of `latest`

`latest` means the build this project currently recommends after completing the required stable Release workflow. It may point to a plain `X.Y.Z` alignment milestone or to a later `X.Y.Z-rnl.N` project Release.

Consequently:

- `latest` does not mean "identical to the newest official Node";
- `latest` never points to `edge` and is not updated by an ordinary `main` push;
- rebuilding or repairing an older Release must not move `latest` backward;
- only the formal Release workflow may move it, and only after all promotion guards succeed;
- changing `latest` does not replace a running container automatically.

Creating a formal tag means the maintainer intends that version to enter the stable channel. A build that is not ready to become the recommended stable Release must remain in the `sha-*` or `candidate-sha-*` candidate channel.

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

This is suitable only when operators read the Release note and deliberately pull and verify each update. `latest` is an update channel, not a rollback identity. Keep the previous exact version or digest for rollback.

### Candidate acceptance

Automatic server acceptance normally starts from a complete `sha-<40-character-commit>` tag. Use `candidate-sha-<40-character-commit>` for a manual rebuild. Before testing, resolve the selected alias to its manifest digest. Run the deployment, record evidence, and perform formal promotion against that same digest, while obtaining the deployment file from the same commit. Do not record acceptance against `edge`, because its meaning changes with a later `main` build.

## Publication and stability

Changing `Version`, merging into `main`, or building a candidate image does not publish a formal Release. Publication requires at least:

1. consistent project, contract, and dependency metadata;
2. the prescribed code and environment acceptance for candidate commit `C`, its binaries, and its manifest digest;
3. a formal Git tag that is immutable under project policy;
4. a GitHub Release with binary assets and promotion of the accepted digest to the exact GHCR version;
5. verification of the candidate image digest and build attestation against its source commit;
6. a Release note with the actual compatibility scope and known risks;
7. successful promotion of that digest to GHCR `latest` and marking the corresponding GitHub Release as Latest.

A version string in the repository may describe a development target. Use actual Git tags, GitHub Releases, and exact GHCR tags to determine what has been published. The current source version is `2.8.0-rnl.1`, and its first formal Release has not yet been published.

## Synchronizing an official version

When official Node publishes a new Release, automation only detects the change and opens an Issue. It does not modify `ContractVersion`, source code, or image tags. Synchronization requires:

1. Pin the official version and immutable commit.
2. Audit route, schema, error, side-effect, and plugin-dependency changes.
3. Update versioned contract evidence and tests.
4. Adjust the Go implementation and complete code regression testing.
5. Run acceptance with the target Panel, rw-core, and Linux environments.
6. Update `ContractVersion` according to the verified result.
7. Choose a plain aligned version or an appropriate `rnl.N` project version for publication.

An early project line cannot skip steps 2 through 6 and report a contract it has not implemented.

## Version output and Release records

The binary prints both identities:

```text
remnanode-lite <Version> (contract <ContractVersion>)
```

Every formal Release note must record at least:

- project version and Git tag;
- candidate commit `C` and accepted image manifest digest;
- final Release commit `F` by reference to the formal Git tag, without attempting an impossible self-reference inside `F`;
- `ContractVersion` and the pinned official source commit;
- Panel version used for verification;
- packaged rw-core version and asset digests;
- `amd64` and `arm64` support status;
- resource-acceptance scope;
- known differences and rollback procedure;
- image manifest digest and verification command.

The runtime `NODE_CONTRACT_VERSION` override exists only for controlled diagnostics or emergency compatibility experiments. It does not change the implemented behavior, source evidence, binary identity, or Release claim and must never be used to manufacture a compatibility statement.
