# Releasing Remnanode Lite

[Documentation index](README.md) | [Versioning and image tags](versioning.md)

Remnanode Lite uses a build-once release flow. A merge to `main` produces the
container image and Native Linux assets that may later become a Release. The
release workflow verifies those exact candidates and publishes them; it never
rebuilds them.

GitHub Releases are draft-first and immutable after publication. Creating a
draft does not create its Git tag. Publishing the verified draft creates the
`v<version>` tag at the accepted `main` commit, then GitHub locks the tag and
assets and creates a release attestation.

```text
dev -> pull request -> main
                         |
                  CI + candidate workflow
                         |
          +--------------+----------------+
          |                               |
  sha-<commit> image       Native assets + release index
          |                               |
          +--------------+----------------+
                         |
             maintainer acceptance
                         |
                manual release workflow
                         |
              draft Release + verified assets
                         |
                   exact image tag
                         |
        publish: create v<version> + lock Release
                         |
               latest or preview channel
```

## Release Classes

The version decides the release class. There is no separate stability switch.

| Version | GitHub Release | Exact GHCR tag | Moving channel | GitHub Latest |
| --- | --- | --- | --- | --- |
| `X.Y.Z` | Stable | `X.Y.Z` | `latest` | Yes |
| `X.Y.Z-rnl.N` | Prerelease | `X.Y.Z-rnl.N` | `preview` | No |

`rnl.N` is a Remnanode Lite revision. It is independent of the official Node
version and may represent work ahead of an official release or improvements to
an existing contract line. See the [versioning policy](versioning.md) for the
relationship between `Version` and `ContractVersion`.

## Repository Setup

The publication guarantees rely on both repository files and GitHub settings.
Keep these settings in place:

- `main` is the protected release branch; normal changes arrive through a pull
  request from `dev`.
- The default `GITHUB_TOKEN` permission is read-only. Each job raises only the
  permissions it needs.
- Actions are pinned to full commit SHAs. Dependabot maintains those pins on
  `dev`.
- The `release` environment accepts deployments from `main` only.
- **Release immutability** is enabled under **Settings -> General -> Releases**.
- A tag ruleset must allow the Releases API to create `v*` tags when a draft is
  published, while denying ordinary users and workstations the ability to
  create, update, or delete those tags. Configure its bypass policy so only the
  protected release automation can use that path. GitHub release immutability
  protects the tag after publication.

Do not create or push release tags from a workstation. The release workflow is
the only supported publication entry point. For an unpublished version, it
fails closed if a `v*` tag already exists instead of adopting it.

## 1. Prepare the Version

Complete the code, tests, deployment files, documentation, and changelog on
`dev`. A release candidate should not need a documentation-only commit after
runtime acceptance because any new commit creates a different candidate.

Check at least the following:

- `internal/version/version.go` contains the intended project version.
- `internal/version/contract.version` reflects the contract actually
  implemented by the code.
- Stable `X.Y.Z` versions match the contract version; previews may carry an
  older verified contract.
- `CHANGELOG.md` contains a dated `## [VERSION] - YYYY-MM-DD` entry.
- Compose defaults and Native documentation name the same version.
- Runtime asset changes are pinned in `release/runtime-assets.lock.json`.

Run the complete local gate when the pinned official source is available:

```bash
export REMNANODE_OFFICIAL_SOURCE=/path/to/pinned/remnawave-node
export REQUIRE_GOVULNCHECK=1
bash scripts/check.sh
```

Local checks shorten feedback. GitHub CI remains the publication record.

## 2. Merge and Wait for Candidates

Merge the reviewed `dev -> main` pull request. Two workflows must finish for
the resulting `main` commit:

- `ci` runs the Go, repository, Native bootstrap, and Linux network tests.
- `candidate` builds and attests the multi-architecture image, builds and
  verifies both Native bundles, binds the accepted OCI index digest in
  `release-index.json`, and stores the complete attested release asset set as a
  workflow artifact.

The candidate image is:

```text
ghcr.io/luxiaba/remnanode-lite:sha-<full-40-character-main-commit>
```

`edge` may point to the same image, but it is a moving observation channel and
must not be used as release evidence. Native candidate artifacts are retained
for 30 days. Re-run the candidate workflow on `main` if they expire. The rerun
verifies and reuses the existing `sha-<commit>` image instead of rebuilding it,
then reproduces the Native bundles from the same source and locked inputs.

`release-index.json` is a small, checksummed Release asset that records the
accepted version, source commit, GHCR repository, and OCI index digest. It is
attested with the rest of the candidate package. Recovery uses this immutable
Release asset rather than treating a registry tag as the historical digest
record.

## 3. Perform Maintainer Acceptance

Deploy the exact `sha-<commit>` image or its resolved manifest digest. Confirm
the behavior relevant to the release, including:

- clean startup and healthy status under the maintained limits;
- connection to the intended Panel with the expected project and contract
  versions;
- real proxy traffic through rw-core;
- release-relevant user, plugin, statistics, and lifecycle operations; and
- no unexpected restart, OOM, or shutdown behavior.

When Native delivery changed, test the candidate bundle on the affected
systemd or OpenRC platform as well. State exactly which architecture and
distribution were exercised; one host does not prove every Linux target.

Acceptance records are operational data. Do not commit host inventories,
addresses, Panel details, secrets, logs, container identifiers, or smoke-test
output.

## 4. Run the Release Workflow

Open **Actions -> release -> Run workflow**, choose `main`, and enter the exact
source version. For example:

```text
version: 2.8.0
```

The CLI equivalent is:

```bash
gh workflow run release.yml \
  --repo luxiaba/remnanode-lite \
  --ref main \
  -f version=2.8.0
```

The workflow performs these operations in order:

1. Confirms the requested version matches the source and that the dispatch
   commit is still the remote `main` HEAD.
2. Finds successful `ci` and `candidate` runs for that exact commit.
3. Downloads the previously built Release assets and verifies their canonical
   file set, checksums, Native bundle manifests, SBOMs, source revision, and
   attestations.
4. Resolves `sha-<commit>`, verifies the two runnable image manifests, their
   attestation manifests, and GitHub provenance, then confirms that its digest
   is the digest recorded by `release-index.json`.
5. Creates or updates a draft GitHub Release without creating the Git tag.
6. Compares every uploaded draft asset with the local digest and size, and
   requires the unpublished `v<version>` tag to remain absent.
7. Reconfirms that the accepted commit is still the remote `main` HEAD and
   that no `v<version>` tag appeared during draft verification.
8. Gives the accepted image digest its immutable exact version tag. No image
   build occurs here, and an existing exact tag is accepted only when its
   digest is identical.
9. Publishes the draft with the correct stable or prerelease status. This is
   the step that creates the `v<version>` tag. The exact image is already
   available before a stable Release can become GitHub Latest.
10. Requires GitHub release immutability and verifies the tag target, Release
    attestation, every local asset including `release-index.json`, and the
    stable Latest pointer when relevant. Identity and asset errors fail
    immediately; only GitHub publication propagation (immutability, Latest,
    and attestations) is retried.
11. Reconfirms that the exact image tag still resolves to the accepted digest,
    then moves that digest to `latest` for a stable release or `preview` for a
    prerelease.

Only the jobs that publish the Release or registry tags receive write access.
Candidate validation remains read-only.

## 5. Verify the Published Result

The Release page must show **Immutable**. You can verify it from a recent
GitHub CLI:

```bash
VERSION="<published-version>"
gh release verify "v${VERSION}" --repo luxiaba/remnanode-lite
```

The expected stable references are:

```text
Git tag:       vX.Y.Z
GitHub Release vX.Y.Z
GHCR exact:    ghcr.io/luxiaba/remnanode-lite:X.Y.Z
GHCR channel:  ghcr.io/luxiaba/remnanode-lite:latest
```

For a preview, replace the version with `X.Y.Z-rnl.N`; the moving channel is
`preview`, and GitHub must not mark it Latest.

To confirm that exact and moving tags resolve to the same manifest:

```bash
docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' \
  ghcr.io/luxiaba/remnanode-lite:X.Y.Z

docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' \
  ghcr.io/luxiaba/remnanode-lite:latest
```

## Failure and Retry Rules

The workflow is deliberately ordered so that irreversible publication happens
late.

| Failure point | External state | Correct response |
| --- | --- | --- |
| Source, CI, candidate, package, or provenance verification | No Release created | Fix the cause or wait for the required workflow, then run release again |
| Draft creation or asset upload | A draft may exist; the release tag does not exist | Re-run the same version only while `main` still points to the accepted commit; the workflow updates and re-verifies the draft |
| Exact image promotion | A verified draft may exist; the Git release tag does not exist | Re-run the same version only while `main` still points to the accepted commit. A matching exact image tag is a no-op; a conflicting digest fails closed. If publication never occurred and `main` advanced, do not reuse that version for a new candidate; choose a new version. Retention cleanup must never delete a digest that a Release or deployment may reference |
| Publication or GitHub propagation (immutability, Latest, or Release attestation) | The Release and tag may already be public; the exact image tag already exists | Inspect the Release first. If it is still a draft and `main` is unchanged, rerun `release`. If it is public, never delete or rewrite it; continue with `reconcile-release` after GitHub finishes propagation |
| `latest` or `preview` promotion | Immutable Release and exact image are valid; a moving channel may be incomplete | Run `reconcile-release` for the published tag. It restores an eligible current channel only; an older Release remains successful after its exact tag is confirmed |

The `reconcile-release` workflow derives the source commit from an immutable
published Release, downloads and verifies its attested `release-index.json`,
then verifies the recorded OCI digest and its provenance before creating or
confirming the exact image tag. It does not infer a historical digest from a
`sha-<commit>` registry tag. It restores `latest` or `preview` only when that
Release still owns its channel; an older Release completes successfully after
exact-tag recovery. It never rebuilds an image and never overwrites a
conflicting exact tag.

A draft is tied to its accepted commit. If `main` advances before the draft is
published, the workflow intentionally refuses to resume it. Delete only that
unpublished draft, accept the candidate for the new `main` commit, then dispatch
the release again. Never delete, retarget, or recreate a published Release or
its tag.

If a published release is wrong, fix the source and publish a new version. Do
not reuse a tag or replace assets.

## Rollback

Keep the previous exact image tag or digest in deployment records. Rollback is
an explicit Compose update:

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

Native installations retain one verified previous generation and can use
`rnlctl rollback`. Moving `latest` or `preview` backward is not the rollback
mechanism.

## Maintainer Checklist

- [ ] Version, contract, changelog, deployment examples, and documentation agree.
- [ ] The `dev -> main` pull request passed required checks and was reviewed.
- [ ] `ci` and `candidate` succeeded for the current `main` commit.
- [ ] The exact `sha-<commit>` image passed real Panel and traffic acceptance.
- [ ] Native behavior was tested when Native delivery changed.
- [ ] The release workflow is dispatched from `main` with the exact source version.
- [ ] The published Release shows **Immutable** and `gh release verify` succeeds.
- [ ] The exact image tag resolves to the digest recorded by the immutable
  Release's `release-index.json`.
- [ ] Stable advanced `latest`, or preview advanced `preview`, but never both.
- [ ] Previous exact deployment references remain available for rollback.
