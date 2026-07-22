# Releasing Remnanode Lite

[Documentation home](README.md) | [Versioning and image tags](versioning.md)

This guide is the maintainer procedure for publishing Remnanode Lite. A
release identifies the reviewed `main` commit selected when the annotated tag
is created. Its container is the multi-architecture candidate already built and
attested for that commit; the release workflow verifies and retags that digest
without rebuilding it.

Release assets follow a draft-first path. The workflow builds, verifies,
attests, uploads, and compares every asset while the GitHub Release is still a
draft. It publishes the draft only after the exact container tag is available,
then promotes the same digest to either `preview` or `latest`.

```text
dev -> pull request -> main -> sha-<commit> candidate
                                  |
                                  v
                        maintainer acceptance
                                  |
                                  v
                       annotated v<version> tag
                                  |
                                  v
                 verified assets in a draft Release
                                  |
                                  v
                     exact GHCR version tag
                                  |
                                  v
                       publish GitHub Release
                                  |
                   +--------------+--------------+
                   |                             |
          X.Y.Z-rnl.N preview                X.Y.Z stable
          GitHub prerelease                  full Release
          GHCR preview                       GHCR latest
```

## 1. Release Types

The tag format determines the release class. The workflow does not accept a
manual stability flag.

| Project version | Git tag | GitHub status | Exact GHCR tag | Moving GHCR channel | GitHub Latest |
| --- | --- | --- | --- | --- | --- |
| `X.Y.Z-rnl.N` | `vX.Y.Z-rnl.N` | Prerelease | `X.Y.Z-rnl.N` | `preview` | Never |
| `X.Y.Z` | `vX.Y.Z` | Full Release | `X.Y.Z` | `latest` | Yes |

`scripts/release-metadata.sh` performs this classification. An `rnl.N` release
is always preview, even if it is newer in calendar time or has passed every
automated gate. It must never update `latest`.

The existing `v2.8.0` release remains an immutable stable release and is not
rewritten. The first planned release from the self-contained Native Linux
bundle pipeline is `v2.8.0-rnl.1`. It is a preview implementing the official
Node `2.8.0` contract, so when published it will move `preview`, not `latest`.

Project `Version` and official `ContractVersion` are separate identities. A
plain stable `X.Y.Z` requires the contract version to match. A preview may use
a different numeric development line, but it must report the contract that the
code actually implements. See the [versioning policy](versioning.md) for the
full rules.

The release preflight also compares a stable version with existing stable Git
tags and rejects a lower version. This prevents an accidental `latest` rollback
even when the tag syntax and contract are otherwise valid.

## 2. Branches and Workflow Responsibilities

The repository has two long-lived branches:

| Branch | Responsibility |
| --- | --- |
| `dev` | Routine integration, regression testing, and release preparation |
| `main` | Protected release branch and source of immutable container candidates |

Normal work reaches `main` through a reviewed `dev -> main` pull request.
Direct commits to `main` are not part of the release procedure.

| Workflow | Release responsibility |
| --- | --- |
| [`ci`](../.github/workflows/ci.yml) | Go, repository, Native bootstrap, asset-lock, and Linux network-administration checks |
| [`container`](../.github/workflows/container.yml) | Pull-request image builds and the attested `linux/amd64` plus `linux/arm64` candidate for each `main` commit |
| [`security`](../.github/workflows/security.yml) | Vulnerability checks |
| [`contract-sync`](../.github/workflows/contract-sync.yml) | Detects a new official Node release and opens an issue; it never changes the contract automatically |
| [`release`](../.github/workflows/release.yml) | Validates the tag and candidate, creates release assets, publishes the GitHub Release, and promotes the correct channel |

After the container workflow succeeds for `main`, the candidate has two names:

```text
ghcr.io/luxiaba/remnanode-lite:sha-<full-40-character-commit>
ghcr.io/luxiaba/remnanode-lite:edge
```

The `sha-*` tag is immutable by project policy and is the only tag used for
release acceptance. `edge` moves with eligible `main` builds and must not be
used as release evidence.

## 3. Prepare the Version on `dev`

Complete code, tests, deployment files, documentation, and translations before
merging the release candidate to `main`. Do not plan a documentation-only
commit after runtime acceptance because that would create a different source
commit and therefore a different candidate.

At minimum, update and review:

- `internal/version/version.go` and every repository default that embeds the
  project version;
- `internal/version/contract.version`, pinned official source evidence, and
  contract tests if compatibility changed;
- `release/runtime-assets.lock.json` and its generated legal or provenance
  material if a runtime asset changed;
- Compose defaults and release examples;
- a dated `CHANGELOG.md` heading in the form
  `## [VERSION] - YYYY-MM-DD`; and
- canonical English documentation followed by all maintained translations.

For a preview, choose the next unused `rnl.N` in that `X.Y.Z` line. For a
stable release, confirm that project and contract versions are identical.

Run the repository checks with the pinned official source available:

```bash
export REMNANODE_OFFICIAL_SOURCE=/path/to/pinned/remnawave-node
export REQUIRE_GOVULNCHECK=1
bash scripts/check.sh
```

The release workflow repeats the complete gate. A passing local run shortens
feedback but never replaces CI.

An official contract update is a compatibility project, not a version-string
change. It requires pinned source, a reviewed route and schema delta,
implementation changes, contract tests, and real Panel verification before
`ContractVersion` moves.

## 4. Merge and Freeze the Candidate

Merge the `dev -> main` pull request after its required checks pass. The release
candidate is the resulting remote `main` HEAD:

```bash
git fetch origin main
C="$(git rev-parse origin/main)"
printf '%s\n' "$C"
```

Wait for both CI and the `main` container workflow to succeed. Then inspect the
immutable candidate:

```bash
IMAGE=ghcr.io/luxiaba/remnanode-lite
CANDIDATE="${IMAGE}:sha-${C}"

docker buildx imagetools inspect "$CANDIDATE"
CANDIDATE_DIGEST="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "$CANDIDATE")"
printf '%s\n' "$CANDIDATE_DIGEST" \
  | grep -Eq '^sha256:[0-9a-f]{64}$'
```

Keep the digest with the release notes used for maintainer review. Keep `main`
unchanged from the final candidate check until the release workflow accepts
the pushed tag. If `main` advances first, the new HEAD is a new candidate:
review the change and repeat the relevant verification instead of tagging the
older commit. Once the workflow has accepted the tag and source identity,
later `main` changes do not invalidate that in-progress release.

## 5. Perform Maintainer Acceptance

Deploy the exact candidate tag or resolved manifest digest. Before release,
confirm that it:

- starts cleanly under the maintained resource limits;
- connects to the intended Panel and reports the expected project and contract
  versions;
- carries real proxy traffic;
- performs the release-relevant Node, rw-core, plugin, user, and statistics
  operations; and
- shows no unexpected restart, OOM, or lifecycle behavior during the test.

For a release that changes Native delivery or lifecycle behavior, also build
the Native bundle from the same clean candidate commit and test the affected
systemd or OpenRC path on the intended distribution:

```bash
bash scripts/build-native-bundle.sh dist/native amd64 arm64
```

The automated workflow cross-builds and structurally verifies both
architectures. A local or hosted runtime test should state exactly which
architecture, distribution, service manager, and lifecycle operations were
exercised. Do not turn a check on one platform into a claim about every Linux
environment.

Operational acceptance is a maintainer decision, not a versioned repository
artifact. Never commit host inventories, IP addresses, Panel details,
container identifiers, logs, smoke-test output, or secrets.

The release workflow verifies the candidate attestation again. Maintainers can
perform the same check before tagging:

```bash
gh attestation verify \
  "oci://${IMAGE}@${CANDIDATE_DIGEST}" \
  --repo luxiaba/remnanode-lite \
  --cert-identity \
    https://github.com/luxiaba/remnanode-lite/.github/workflows/container.yml@refs/heads/main \
  --source-digest "$C" \
  --deny-self-hosted-runners
```

## 6. Run the Tag Preflight

Tag only a clean checkout of the current remote `main` HEAD:

```bash
git fetch origin main --tags
git switch main
git pull --ff-only origin main

test -z "$(git status --porcelain --untracked-files=all)"
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)"

VERSION="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' \
  internal/version/version.go)"
TAG="v${VERSION}"
```

Run the release-specific preflight before creating the tag:

```bash
export REMNANODE_OFFICIAL_SOURCE=/path/to/pinned/remnawave-node
export REQUIRE_GOVULNCHECK=1
RELEASE_TAG="$TAG" bash scripts/release-check.sh
```

This requires a clean tree and verifies the version format, contract identity,
dated changelog entry, strict translation freshness, Native bootstrap,
repository checks, official source evidence, and vulnerability gate.

Create an annotated tag and inspect it before pushing:

```bash
git tag -a "$TAG" -m "Remnanode Lite ${VERSION}"
test "$(git cat-file -t "$TAG")" = tag
test "$(git rev-list -n 1 "$TAG")" = "$(git rev-parse origin/main)"
git show --no-patch "$TAG"
git push origin "$TAG"
```

When publishing the first Native preview, `VERSION` is `2.8.0-rnl.1` and `TAG`
is `v2.8.0-rnl.1`. Do not recreate, move, or otherwise alter the existing
`v2.8.0` tag.

Pushing the tag starts the release workflow. Keep `main` frozen until its
initial source-identity check succeeds. Exact tags must then remain immutable;
the workflow re-reads the remote annotated tag before creating the draft,
publishing the Release, and promoting the channel. Protect `v*` against update
and deletion in the GitHub repository rules. If a source defect is discovered
after the push, correct it on `main` and choose a new version rather than moving
the tag to another commit.

## 7. What the Release Workflow Verifies

The tag workflow first establishes source and candidate identity:

1. The tag commit must still be the reviewed `origin/main` HEAD when the
   workflow performs its initial identity check. After that acceptance,
   subsequent `main` changes do not invalidate the in-progress release.
2. The tag must be annotated and match `Version` exactly.
3. The pinned official source and the complete release gate must pass.
4. The tag is classified as stable or preview from its syntax.
5. The `sha-<commit>` candidate must exist and resolve to a valid manifest
   digest.
6. The candidate index must contain exactly one runnable `linux/amd64` image,
   one runnable `linux/arm64` image, and the corresponding attestations.
7. The OCI attestation must identify the container workflow on `main` and the
   tagged source commit.
8. The Linux network-administration integration tests run in isolated
   namespaces.
9. The remote tag must remain an annotated tag resolving to the same commit at
   every external publication boundary.

The release workflow never accepts `edge` as a substitute and never rebuilds
the container. The exact version and moving channel are additional names for
the accepted candidate digest.

## 8. Release Assets

The workflow builds and uploads these assets for both stable and preview
releases:

| Asset | Purpose |
| --- | --- |
| `install.sh` | POSIX bootstrap for an exact Native release or local bundle |
| `remnanode-lite_<version>_linux_amd64.tar.gz` | Self-contained Native Linux bundle for amd64 |
| `remnanode-lite_<version>_linux_arm64.tar.gz` | Self-contained Native Linux bundle for arm64 |
| `compose.yaml` | Repository Compose deployment file |
| `docker-compose.single-file.yaml` | Single-file Compose template pinned to the exact release image |
| `remnanode.env.example` | Environment template pinned to the exact release image |
| `SHA256SUMS` | Checksums for every other uploaded release asset |

Each Native archive contains one complete generation, including:

- `remnanode-lite` and `rnlctl` binaries;
- the locked rw-core binary, GeoIP, GeoSite, and generated ASN database;
- systemd and OpenRC service material;
- `release-manifest.json` and `runtime-assets.lock.json`;
- an SPDX SBOM, third-party notices, licenses, and source offer; and
- the bundle-local installer.

Runtime assets are part of the bundle. There is no separate ASN database
release asset and no release-time download from an unpinned moving source.
Docker and Native bundles are built from the same
`release/runtime-assets.lock.json`.

`cmd/release-tool` verifies each archive against the expected architecture,
project version, contract version, source revision, file manifest, asset lock,
and embedded checksums. The workflow also validates `SHA256SUMS` before upload.

## 9. Draft, Publish, and Promote

Publication happens in a fixed order.

### 9.1 Attest Every Asset

Every file in the release staging directory, including `SHA256SUMS`, receives
a GitHub artifact attestation tied to the tag workflow and source commit. The
workflow immediately verifies those attestations before creating the draft.

### 9.2 Create and Verify the Draft

The workflow creates a draft GitHub Release with generated notes and the
correct prerelease flag. If a draft for the same tag and source commit already
exists, a rerun may replace its assets. A published Release can never be
replaced this way.

Before continuing, the workflow compares the draft's complete asset list with
the local build. Every name, SHA-256 digest, and byte size must match. The
Release remains invisible to normal consumers while this check runs.

### 9.3 Publish the Exact Container Tag

After the draft is complete, the workflow promotes the accepted candidate
digest to the exact version tag. The immutable promotion helper accepts an
already existing exact tag only when it resolves to the same digest; it refuses
to overwrite a different image.

### 9.4 Publish the GitHub Release

The verified draft is published with one of two states:

- `X.Y.Z-rnl.N` becomes a GitHub prerelease with `make_latest=false`;
- `X.Y.Z` becomes a full GitHub Release with `make_latest=true`.

The workflow verifies the published state. In particular, a preview must not
resolve through GitHub's Latest Release endpoint.

### 9.5 Promote the GHCR Channel

Only after the GitHub Release is published does the workflow move a channel:

- preview release: accepted digest -> `preview`;
- stable release: accepted digest -> `latest`.

The channel promotion does not rebuild or copy platform images. It publishes a
new manifest reference to the exact accepted digest.

## 10. Verify the Published Release

Set the exact release identity:

```bash
VERSION=2.8.0-rnl.1
TAG="v${VERSION}"
C="$(git rev-list -n 1 "$TAG")"
CHANNEL=preview
IMAGE=ghcr.io/luxiaba/remnanode-lite
```

For a stable release, use its plain version and `CHANNEL=latest`.

Check GitHub's published state:

```bash
gh api "repos/luxiaba/remnanode-lite/releases/tags/${TAG}" \
  --jq '{tag_name, draft, prerelease, target_commitish}'
gh api repos/luxiaba/remnanode-lite/releases/latest --jq .tag_name
```

The release must not be a draft. `prerelease` must be `true` for `rnl.N` and
`false` for a plain version. GitHub Latest must equal a new stable tag and must
not equal a preview tag.

Download and verify every asset:

```bash
work="$(mktemp -d)"
gh release download "$TAG" --repo luxiaba/remnanode-lite --dir "$work"
(
  cd "$work"
  sha256sum --check --strict SHA256SUMS
)

for asset in "$work"/*; do
  gh attestation verify "$asset" \
    --repo luxiaba/remnanode-lite \
    --cert-identity \
      "https://github.com/luxiaba/remnanode-lite/.github/workflows/release.yml@refs/tags/${TAG}" \
    --source-digest "$C" \
    --deny-self-hosted-runners
done
rm -rf "$work"
```

Confirm that the candidate, exact tag, and correct channel resolve to one
manifest digest:

```bash
CANDIDATE_DIGEST="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "${IMAGE}:sha-${C}")"
EXACT_DIGEST="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "${IMAGE}:${VERSION}")"
CHANNEL_DIGEST="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "${IMAGE}:${CHANNEL}")"

test "$CANDIDATE_DIGEST" = "$EXACT_DIGEST"
test "$EXACT_DIGEST" = "$CHANNEL_DIGEST"
```

Finally, verify the container attestation against the release commit:

```bash
gh attestation verify "oci://${IMAGE}@${EXACT_DIGEST}" \
  --repo luxiaba/remnanode-lite \
  --cert-identity \
    https://github.com/luxiaba/remnanode-lite/.github/workflows/container.yml@refs/heads/main \
  --source-digest "$C" \
  --deny-self-hosted-runners
```

## 11. Failure and Recovery

Do not delete or move an exact Git tag or exact image tag to recover a release.
The safe action depends on how far the workflow progressed.

| Failure point | Expected external state | Recovery |
| --- | --- | --- |
| Candidate lookup or preflight | No draft or release created | Ensure CI and the container workflow succeeded, then rerun the failed release workflow for the same tag |
| Asset build, attestation, or upload | No draft, or a draft tied to the same commit | Rerun the workflow; matching draft assets may be replaced and are checked again |
| Exact image promotion | Verified draft may exist; exact image may or may not exist | Rerun; immutable promotion accepts the existing tag only if its digest is already correct |
| GitHub publication | Exact image and draft may exist | Rerun the failed job while the Release is still a draft |
| GHCR channel promotion | GitHub Release and exact image are already published | Use the release workflow's manual channel reconciliation for that published tag |
| Defect found after publication | Exact release remains published | Fix `main`, choose a new version, and complete a new release |

Manual dispatch is a reconciliation path, not a second release path:

```bash
gh workflow run release.yml \
  --repo luxiaba/remnanode-lite \
  -f release_tag=v2.8.0-rnl.1
```

Reconciliation checks that:

- the tag is annotated and identifies the published Release;
- the GitHub prerelease and Latest state match the tag class;
- the exact image equals the original `sha-<commit>` candidate;
- the candidate attestation matches the source commit; and
- the destination channel is `preview` for `rnl.N` or `latest` for a plain
  version.

It then repairs only that GHCR channel reference. It does not rebuild assets,
replace a published Release, change an exact image, or repair incorrect GitHub
release metadata. Correct any GitHub metadata issue first, then run
reconciliation.

If a workflow fails because the release source itself is wrong, do not keep
retrying it. Correct the defect on `dev`, merge a new `main` candidate, advance
the project version, and publish that new identity.

## 12. Deployment Rollback After a Release

Release immutability and deployment rollback are separate concerns. Do not move
an exact release tag backward or silently replace its assets.

- Docker deployments roll back to the previously recorded exact version or
  manifest digest, then pull and recreate the container.
- Native deployments use `rnlctl rollback` to select the retained previous
  generation. A deliberate downgrade beyond that retained generation uses an
  exact verified release bundle.
- A project-wide corrective release receives a new version and goes through the
  complete workflow.

Avoid repairing a bad stable release by manually moving `latest` to an
unreviewed image. Publish a corrected stable version so the changelog,
artifacts, exact image, attestations, and rollback identity remain coherent.

## 13. Following a New Official Node Version

The scheduled contract workflow opens an issue when the official Node publishes
a version different from the pinned baseline. That issue starts investigation;
it does not authorize an automatic version bump.

Before changing `ContractVersion`:

1. pin the official tag and immutable commit;
2. regenerate and review contract evidence;
3. compare routes, request and response schemas, errors, plugin behavior, and
   side effects;
4. update the Go implementation and tests;
5. run the full repository and release gates;
6. verify the candidate with the target Panel and real traffic; and
7. document the actual compatibility scope and known differences.

Development may use a new `X.Y.Z-rnl.N` project line before this work is
complete, but the binary must continue to report the older verified contract.
Use a plain `X.Y.Z` release only after same-version alignment is complete.

## 14. Maintainer Checklist

- [ ] The version format selects the intended stable or preview class.
- [ ] `Version`, `ContractVersion`, Compose defaults, tests, changelog, and
      documentation agree.
- [ ] Canonical English documentation and maintained translations are current.
- [ ] The official source evidence is pinned and verified.
- [ ] CI, container, Native, network-administration, security, and release
      preflight checks pass.
- [ ] The accepted `sha-<commit>` candidate is the current `main` HEAD.
- [ ] Real Panel and traffic verification used that exact candidate digest.
- [ ] Native lifecycle changes were tested on the claimed distributions and
      architectures.
- [ ] The annotated release tag points to the current remote `main` HEAD.
- [ ] Draft assets match the build by name, digest, and size.
- [ ] Every release asset and the container manifest has a valid attestation.
- [ ] The exact image tag matches the accepted candidate digest.
- [ ] A preview is a GitHub prerelease and does not change GitHub Latest or
      GHCR `latest`.
- [ ] A stable release is a full GitHub Release and advances GHCR `latest`.
- [ ] The published release's `preview` or `latest` channel resolves to the
      exact release digest.
- [ ] The previous exact deployment reference is retained for rollback.
