# Versioning and Image Tags

[Back to the documentation index](README.md) | [Release process](release.md)

Remnanode Lite separates project identity from compatibility claims. A build
can evolve independently while continuing to implement an older, verified
official Node contract. Release names and moving image channels are separate as
well: an exact version identifies one release, while `preview` and `latest`
select release classes.

This document defines those identities and channels. The release workflow is
the executable source of truth for publication. Its `release-tool metadata`
command derives the stable-versus-preview class from the source version.

## Independent Version Dimensions

| Dimension | Source of truth | Meaning |
| --- | --- | --- |
| Project `Version` | `internal/version/version.go` | Identity embedded in this project's source, binaries, release assets, and exact container tag |
| Official `ContractVersion` | `internal/version/contract.version`, `internal/version/version.go`, and pinned contract evidence | Official Node behavior implemented and reported to the Panel |
| Panel integration target | Maintainer release verification | Panel version used for real integration checks; it is not compiled into the release identity |
| rw-core and runtime assets | `release/runtime-assets.lock.json` | Exact core, GeoIP, GeoSite, ASN, source, license, and checksum inputs packaged for Docker and Native Linux |

`Version` and `ContractVersion` move independently. For example:

```text
Version:         2.8.0
ContractVersion: 2.8.0
```

This is the stable Remnanode Lite release line aligned with the verified
official Node `2.8.0` contract. Its Native Linux distribution is attached only
when the corresponding GitHub Release is published. A future `rnl.N` suffix
describes this project's release; it is not a revision published by the
official project.

Changing `ContractVersion` requires pinned official source, a reviewed contract
delta, corresponding implementation and test changes, and completed
compatibility verification. Changing only `Version` never expands the claimed
contract.

## Release Classes

Every formal project version uses exactly one of two formats.

### Stable: `X.Y.Z`

A plain version is a stable release aligned with the same official Node
contract. The repository checks require `Version` and `ContractVersion` to
match for this form.

A stable release is published as:

- a `vX.Y.Z` Git tag created when the draft GitHub Release is published;
- a normal, non-prerelease GitHub Release;
- an exact GHCR tag named `X.Y.Z`; and
- the source of the moving GHCR `latest` tag.

The GitHub Release is also marked Latest. A plain version is therefore both an
immutable alignment point and the release selected by the stable channel when
it is published successfully.

### Preview: `X.Y.Z-rnl.N`

An `rnl.N` version is a Remnanode Lite preview. It can be used to develop ahead
of an official release or to improve code, architecture, delivery, and resource
behavior while keeping an existing contract baseline.

A preview is published as:

- a `vX.Y.Z-rnl.N` Git tag created when the draft GitHub Release is published;
- a GitHub prerelease;
- an exact GHCR tag named `X.Y.Z-rnl.N`; and
- the source of the moving GHCR `preview` tag.

A preview never updates GHCR `latest` and never becomes GitHub's Latest
Release. It may have passed the full automated release workflow, but it remains
a preview until a plain stable version is published.

Within one `X.Y.Z` line, `N` starts at 1 and increases monotonically. Published
numbers and exact tags are never reused. The numeric `X.Y.Z` prefix identifies
the project development line; it does not imply that the same official
contract has already been implemented.

### Current Version Line

| Release | Contract | Class | Status |
| --- | --- | --- | --- |
| `2.8.0` | `2.8.0` | Stable | Current contract-aligned release line; Native bundles exist only for published Releases |

Semantic Versioning orders an `X.Y.Z-rnl.N` preview before its `X.Y.Z` stable
counterpart. Do not infer publication order or channel selection from SemVer
sorting. The release workflow assigns `preview` or `latest` explicitly from the
tag format.

## Git Tags and Exact Image Tags

Formal Git tags include a `v` prefix. Container tags do not:

```text
Git tag:       vX.Y.Z
Container tag: ghcr.io/luxiaba/remnanode-lite:X.Y.Z
```

Both are immutable release identities, but they identify different objects:

- the GitHub Release tag identifies the source commit accepted from `main`; and
- the exact container tag identifies the already built and attested
  multi-architecture manifest for that commit.

The release workflow does not rebuild the container. It verifies the
`sha-<commit>` candidate produced from `main`, then gives that same manifest
digest the exact release tag.

Do not create or push `v*` tags from a workstation. Creating the draft does not
create a tag; publishing it creates the tag at the accepted `main` commit. With
GitHub Release immutability enabled, that publication also locks the tag and
assets. A tag ruleset must allow this Releases API creation. For an unpublished
version, the workflow refuses an existing tag rather than adopting it.

Registry tags are names, not content addresses. Use
`ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>` when a deployment must
remain fixed even if a tag is changed accidentally.

## Container References

| Reference | Mutability | Meaning | Intended use |
| --- | --- | --- | --- |
| `sha-<40-character-commit>` | Immutable by policy | Attested candidate built from one `main` commit | Release verification, reproduction, and diagnosis |
| `edge` | Moving | Most recent eligible `main` candidate | Mainline observation only |
| `X.Y.Z-rnl.N` | Immutable by policy | One published preview | Controlled preview deployment and exact rollback |
| `preview` | Moving | Preview most recently promoted by the release workflow | Opt-in preview tracking and evaluation |
| `X.Y.Z` | Immutable by policy | One published stable release | Recommended production deployment and exact rollback |
| `latest` | Moving | Stable release most recently promoted by the release workflow | Opt-in stable tracking |
| `name@sha256:...` | Content addressed | One registry manifest digest | Strongest deployment and verification pin |

An ordinary push to `main` can update `edge`, but it cannot update `preview` or
`latest`. Only the release workflow promotes those channels after publishing a
corresponding release.

### Stable and Preview Channels Never Overlap

The two moving channels are intentionally disjoint:

- `latest` resolves only to a plain `X.Y.Z` stable release;
- `preview` resolves only to an `X.Y.Z-rnl.N` prerelease;
- a preview cannot advance, replace, or repair `latest`; and
- a stable release cannot advance `preview`.

Neither channel updates a running container automatically. Docker checks the
tag only after an explicit pull and recreates the container only after an
explicit Compose operation.

## Choosing a Docker Reference

For normal production deployments, use an exact stable version:

```text
ghcr.io/luxiaba/remnanode-lite:X.Y.Z
```

For the strongest pin, record and deploy its manifest digest:

```text
ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>
```

Use an exact preview only when the preview status and changes are acceptable:

```text
ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N
```

`preview` is convenient for short-lived evaluation, but an exact preview tag or
digest is preferable for fleet testing because it cannot move between nodes.
Keep the previous exact reference for rollback.

`latest` is an opt-in stable update channel, not a rollback identity. Even when
tracking it, review the release, record the resolved digest, and update
explicitly:

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

Use `sha-<commit>` to verify the candidate that may become a release. Do not use
`edge` for release acceptance because another `main` build can move it during
testing.

The candidate workflow also records the accepted content address in an
attested `release-index.json` asset. Publication requires that record to match
the verified `sha-<commit>` candidate. Once the Release is immutable, recovery
uses the recorded digest directly instead of assuming a registry tag is a
durable identity.

## Native Linux Uses Exact Versions Only

Native installation and upgrade resolve complete, versioned release bundles.
They deliberately do not follow moving channels. The bootstrap installer
accepts an exact version such as:

```bash
sudo sh install.sh --version "<published-version>"
```

The administration CLI follows the same rule for an online upgrade:

```bash
sudo rnlctl upgrade --to <exact-version>
```

`latest`, `preview`, `edge`, and `sha-*` are not valid Native version inputs.
Exact selection ensures that the archive name, `SHA256SUMS`, release manifest,
embedded version, and source revision can be checked as one release identity.

## What Counts as Published

A version string in source, a `main` candidate, or a Git tag alone is not a
complete publication.

A published preview has all of the following:

1. a `vX.Y.Z-rnl.N` tag created when its draft Release is published on the
   accepted `main` commit;
2. a published GitHub prerelease with verified assets, including its attested
   `release-index.json`;
3. an exact `X.Y.Z-rnl.N` GHCR tag matching the digest in that index; and
4. successful promotion of that digest to `preview`.

A published stable release has the corresponding plain `vX.Y.Z` tag, normal
GitHub Release, exact `X.Y.Z` image, GitHub Latest designation, and GHCR
`latest` promotion.

Both classes use the same code, compatibility, asset, provenance, and
attestation gates. The difference is the release status and channel, not a
weaker build path for previews.

Check Git tags, GitHub Releases, and exact GHCR tags before presenting a planned
version or asset URL as available.

## Following Official Node Releases

The scheduled contract workflow reports a new official release by opening an
issue. It does not change `ContractVersion`, source code, project versions, or
container tags.

Synchronizing a new official contract requires:

1. pinning the official version and immutable source commit;
2. auditing routes, schemas, errors, side effects, and plugin dependencies;
3. updating contract evidence and tests;
4. aligning the Go implementation;
5. verifying the candidate with the target Panel, rw-core, and Linux
   environments; and
6. changing `ContractVersion` only after the verified behavior is complete.

Choose the project version separately. A preview can start before alignment is
finished, but it must continue to report the contract it actually implements.
A plain stable version is valid only when its project and contract versions
match.

## Version Output and Release Metadata

The Node and `rnlctl` report both project and contract identities:

```text
remnanode-lite <Version> (contract <ContractVersion>)
rnlctl <Version> (contract <ContractVersion>)
```

Release records should also identify:

- the release class and promoted channel;
- the project version, Git tag, and source commit;
- the official contract version and pinned source commit;
- the accepted container manifest digest, its attestation, and the
  `release-index.json` record that binds it to the source revision;
- the Panel and runtime scope used for maintainer verification;
- the locked rw-core and runtime asset versions;
- the `amd64` and `arm64` publication status;
- known differences, risks, and rollback reference; and
- Native bundle checksums and asset attestations.

GitHub generates release notes from merged changes. Host inventories, Panel
details, logs, secrets, and other runtime observations are not release assets
and must not be committed.

`NODE_CONTRACT_VERSION` is limited to controlled diagnostics and emergency
compatibility tests. It does not change implemented behavior, pinned evidence,
binary identity, or release claims.
