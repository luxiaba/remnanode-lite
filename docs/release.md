# Releasing Remnanode Lite

[Documentation home](README.md) | [Versioning model](versioning.md)

This guide describes the maintainer workflow for publishing a source release,
binary archives, and a container image. The central rule is simple: a release
is the current `main` commit, and the container published for that release is
the image already built and attested for that same commit.

The normal path is:

```text
development on dev
        |
        v
pull request to main
        |
        v
CI + immutable sha-<commit> candidate + edge
        |
        v
maintainer verifies the candidate with a real Panel and real traffic
        |
        v
annotated v<version> tag on the current main HEAD
        |
        v
GitHub Release + exact image tag + latest
```

There is no separate documentation-only commit after candidate verification.
Runtime observations are not stored in the repository, and GitHub generates
the Release notes automatically.

## 1. Version Identities

The project version and the official Node contract version are related, but
they are not interchangeable.

| Identity | Example | Meaning |
| --- | --- | --- |
| Project version | `2.8.1-rnl.3` | This project's source, binaries, GitHub Release, and exact image tag |
| Contract version | `2.8.0` | Official Node behavior implemented and reported to the Panel |
| Git tag | `v2.8.1-rnl.3` | Immutable trigger for the Release workflow |
| Image tag | `2.8.1-rnl.3` | Exact container release, without the Git tag's `v` prefix |

Project releases use one of two formats:

- `X.Y.Z-rnl.N` is an independent Remnanode Lite iteration. It may improve an
  existing contract or start work on a future project line. The numeric prefix
  does not claim compatibility with an official version by itself.
- `X.Y.Z` is reserved for a completed same-version alignment milestone. The
  project version, contract version, pinned official source, implementation,
  tests, and real-environment behavior must all agree before this form is used.

`rnl.N` is a SemVer prerelease suffix, but publication order is not inferred
from SemVer sorting. The Release workflow decides which completed release is
promoted to `latest`.

Published Git tags and exact-version image tags are immutable. The `sha-*`
candidate tags are also immutable by policy. Only `edge` and `latest` are
moving channels:

| Image reference | Purpose |
| --- | --- |
| `sha-<40-character-main-commit>` | Reproducible candidate built from one `main` commit |
| `edge` | Most recent eligible `main` image; useful for observation, not rollback |
| `X.Y.Z` or `X.Y.Z-rnl.N` | Exact published release |
| `latest` | Release currently recommended by this project |
| `name@sha256:<digest>` | Content-addressed production pin |

## 2. Branches and Automation

The repository has two long-lived branches:

| Branch | Role |
| --- | --- |
| `dev` | Routine development, integration, and regression testing |
| `main` | Protected release branch and source of container candidates |

Changes normally reach `dev` through a topic branch and then reach `main`
through a `dev -> main` pull request. Direct changes to `main` are not part of
the release process.

The workflows divide responsibility as follows:

| Workflow | Responsibility |
| --- | --- |
| [`ci`](../.github/workflows/ci.yml) | Runs repository, Go, installer, and integration checks |
| [`container`](../.github/workflows/container.yml) | Builds container changes for pull requests and publishes the multi-architecture candidate for every `main` commit |
| [`security`](../.github/workflows/security.yml) | Runs scheduled and on-demand vulnerability checks |
| [`contract-sync`](../.github/workflows/contract-sync.yml) | Reports new official Node releases without changing this repository automatically |
| [`release`](../.github/workflows/release.yml) | Validates a `v*` tag, publishes assets, and promotes the existing candidate image |

Every push to `main` produces these references after the container workflow
succeeds:

```text
ghcr.io/luxiaba/remnanode-lite:sha-<full-main-commit>
ghcr.io/luxiaba/remnanode-lite:edge
```

The full 40-character commit identifies the candidate. A manual rerun on the
current `main` commit uses the same `sha-*` identity; it does not create a
second candidate namespace.

## 3. Prepare the Release on `dev`

Finish all source, test, workflow, deployment, and documentation changes before
merging to `main`. Update the project version and the dated `CHANGELOG.md`
entry as part of the same development work.

At minimum, check that these values agree:

- `internal/version/version.go` and every user-facing default that embeds the
  project version;
- the contract version and pinned official source, if the contract changed;
- Compose, installer, and upgrade defaults;
- tests and compatibility documentation; and
- the current `CHANGELOG.md` heading, in the form
  `## [VERSION] - YYYY-MM-DD`.

Run checks appropriate to the change before opening the pull request. Release
preparation must include the complete gate because the tag workflow will run it
again:

```bash
REMNANODE_OFFICIAL_SOURCE=/path/to/remnawave-node \
REQUIRE_GOVULNCHECK=1 \
  bash scripts/check.sh
```

An official contract upgrade is a compatibility project, not a version-string
edit. It requires a pinned official tag and commit, a review of routes,
schemas, errors, side effects, and plugin behavior, corresponding code and test
updates, and real integration with the target Panel.

## 4. Merge and Identify the Candidate

Open a `dev -> main` pull request and merge it after the required checks pass.
The candidate commit is the resulting `main` HEAD, not the pre-merge commit on
`dev`:

```bash
git fetch origin main
C="$(git rev-parse origin/main)"
printf '%s\n' "$C"
```

Wait for both CI and the `main` container workflow to finish. The immutable
candidate is then:

```bash
IMAGE="ghcr.io/luxiaba/remnanode-lite:sha-${C}"
docker buildx imagetools inspect "$IMAGE"
```

Do not use `edge` for release verification. It can move when another commit
reaches `main`.

Keep `main` unchanged while the candidate is being evaluated. If `main`
advances, the new HEAD is a new candidate; review the change and repeat the
relevant checks before tagging it.

## 5. Verify the Candidate in a Real Environment

Use the immutable `sha-${C}` image, or resolve it to a manifest digest and pin
that digest in the test deployment:

```bash
CANDIDATE_DIGEST="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "$IMAGE")"
printf '%s\n' "$CANDIDATE_DIGEST" \
  | grep -Eq '^sha256:[0-9a-f]{64}$'

PINNED_IMAGE="ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}"
```

Before publishing, the maintainer should confirm that this exact candidate:

- starts cleanly and remains healthy under the intended Compose limits;
- connects to the target Panel with the expected project and contract version;
- carries real proxy traffic correctly; and
- shows no unexpected restart, OOM, or lifecycle behavior during the test.

This is an operational release decision, not a repository artifact. Do not
commit host inventories, container identifiers, timestamps, IP addresses,
Panel details, logs, smoke JSON, or other runtime observations. Secrets must
never enter Git or a GitHub Release.

The Release workflow independently verifies the supply-chain identity. A
maintainer can run the same attestation check before tagging:

```bash
gh attestation verify \
  "oci://ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}" \
  --repo luxiaba/remnanode-lite \
  --cert-identity \
    https://github.com/luxiaba/remnanode-lite/.github/workflows/container.yml@refs/heads/main \
  --source-digest "$C" \
  --deny-self-hosted-runners
```

## 6. Create the Release Tag

Tag only the current remote `main` HEAD. Start from a clean, up-to-date local
checkout:

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

Run the release preflight with the pinned official source checkout:

```bash
export REMNANODE_OFFICIAL_SOURCE=/path/to/remnawave-node
export REQUIRE_GOVULNCHECK=1

RELEASE_TAG="$TAG" bash scripts/release-check.sh
```

Then create and verify an annotated tag:

```bash
git tag -a "$TAG" -m "release ${TAG}"

RELEASE_TAG="$TAG" \
REQUIRE_TAG_AT_HEAD=1 \
  bash scripts/release-check.sh

git push origin "$TAG"
```

There is no documentation-only finalization branch or commit between candidate
testing and the tag. If the preflight finds a version, changelog, code, or
documentation problem, fix it through `dev`, merge a new `main` commit, and
evaluate that new candidate before tagging.

## 7. What the Release Workflow Publishes

The tag-triggered workflow performs the following operations:

1. Confirms that the tag points to the current `main` HEAD and that the tag,
   project version, contract metadata, and dated changelog agree.
2. Reruns the release checks from the tagged source.
3. Resolves `sha-${GITHUB_SHA}` and verifies that the candidate is an OCI index
   containing exactly one runnable `linux/amd64` image and one runnable
   `linux/arm64` image, with the expected BuildKit attestation manifest for
   each platform.
4. Verifies the GitHub build attestation against this repository, the exact
   `container.yml` identity on `refs/heads/main`, and the tagged source commit.
5. Builds the downloadable release assets and their `SHA256SUMS` file.
6. Creates a GitHub Release with automatically generated Release notes.
7. Promotes the already verified candidate digest to the exact version tag and
   then to `latest`. The container is not rebuilt during release.

The published assets are:

```text
remnanode-lite_linux_amd64.tar.gz
remnanode-lite_linux_arm64.tar.gz
asn-prefixes.bin
compose.yaml
docker-compose.single-file.yaml
remnanode.env.example
SHA256SUMS
```

Exact-version publication refuses to replace a different digest. Before moving
`latest`, the workflow fetches `main` again; a stale tag cannot move the stable
channel backward. A shared registry concurrency group also prevents candidate
and release promotions from racing.

Both `X.Y.Z` and `X.Y.Z-rnl.N` are stable project releases after completing
this workflow. Experimental work remains on `dev` or in the `sha-*`/`edge`
channels and must not receive a final tag.

## 8. Verify the Published Release

After the workflow succeeds, confirm that the candidate, exact version, and
`latest` resolve to the same manifest digest:

```bash
VERSION=X.Y.Z                    # Or X.Y.Z-rnl.N
C="$(git rev-list -n 1 "v${VERSION}")"
IMAGE="ghcr.io/luxiaba/remnanode-lite"

CANDIDATE_DIGEST="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "${IMAGE}:sha-${C}")"
VERSION_DIGEST="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "${IMAGE}:${VERSION}")"
LATEST_DIGEST="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "${IMAGE}:latest")"

test "$CANDIDATE_DIGEST" = "$VERSION_DIGEST"
test "$VERSION_DIGEST" = "$LATEST_DIGEST"
```

Inspect the exact image to confirm both supported platforms:

```bash
docker buildx imagetools inspect "${IMAGE}:${VERSION}"
```

Verify the attestation against the release commit:

```bash
gh attestation verify "oci://${IMAGE}@${VERSION_DIGEST}" \
  --repo luxiaba/remnanode-lite \
  --cert-identity \
    https://github.com/luxiaba/remnanode-lite/.github/workflows/container.yml@refs/heads/main \
  --source-digest "$C" \
  --deny-self-hosted-runners
```

Release downloads can be checked with the published checksum file:

```bash
BASE_URL="https://github.com/luxiaba/remnanode-lite/releases/download/v${VERSION}"
mkdir -p "/tmp/remnanode-lite-${VERSION}"
cd "/tmp/remnanode-lite-${VERSION}"

curl -fLO "$BASE_URL/SHA256SUMS"
curl -fLO "$BASE_URL/remnanode-lite_linux_amd64.tar.gz"
curl -fLO "$BASE_URL/remnanode-lite_linux_arm64.tar.gz"
curl -fLO "$BASE_URL/asn-prefixes.bin"
curl -fLO "$BASE_URL/compose.yaml"
curl -fLO "$BASE_URL/docker-compose.single-file.yaml"
curl -fLO "$BASE_URL/remnanode.env.example"
sha256sum --check SHA256SUMS
```

## 9. Failures and Retries

Prefer **Re-run failed jobs** for transient GitHub, registry, or network
failures. Publication is designed to be idempotent: an existing exact tag is
accepted only when it already points to the expected digest, and existing
Release assets are not overwritten.

| Failure point | Expected state | Recovery |
| --- | --- | --- |
| Preflight, candidate, OCI, or attestation check | No new final image | Correct the underlying source or candidate through `dev`; use a new `main` commit |
| GitHub Release asset publication | The Release may be incomplete; exact image is not yet promoted | Re-run failed jobs if the failure was transient |
| Exact-version promotion | Release assets may exist; `latest` is unchanged | Re-run and promote the same verified digest |
| `latest` promotion or GitHub Latest flag | Exact release remains usable | Re-run the promotion job after checking the current `main` HEAD |

Once a tag has been pushed, do not move, overwrite, delete-and-reuse, or force
push it. A non-transient fix that changes the repository requires a new project
version and a new candidate. Never mutate an exact image tag to hide a failed
or incorrect release.

## 10. Rollback

Production deployments should record an exact version or manifest digest and
retain the previous one. To roll back a container node, restore that reference
and the matching deployment configuration, then recreate the service:

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
```

Do not use `latest` as a rollback identity. It is a moving recommendation and
does not update a running container automatically. Roll out fleet changes in
batches and keep a known-good exact version or digest available. See
[`deployment-docker.md`](deployment-docker.md) for day-to-day container
operations.

Native systemd and OpenRC installations can select a published tag explicitly:

```bash
sudo RNL_TAG=vX.Y.Z bash upgrade.sh --yes
```

## 11. Following Official Node Releases

The `contract-sync` workflow checks for new official `remnawave/node` releases
and opens an issue when it finds one. It never changes code, contract metadata,
project versions, Git tags, or images automatically.

For a new official version:

1. Record the official tag and pin its full commit.
2. Audit route, schema, error, side-effect, plugin, and runtime differences.
3. Update the contract evidence, implementation, and automated tests.
4. Verify the result against the intended Panel and real proxy traffic.
5. Change the contract version only after the new baseline is implemented.
6. Publish a plain `X.Y.Z` only when same-version alignment is complete;
   otherwise use an honest `X.Y.Z-rnl.N` project version.

An official release begins compatibility work. It does not automatically
select this project's next version or publish anything on its behalf.

## 12. Maintainer Checklist

Before pushing the final tag, confirm that:

- [ ] the project version uses an allowed `X.Y.Z` or `X.Y.Z-rnl.N` format;
- [ ] a plain version equals the implemented and pinned contract version;
- [ ] version metadata, tests, deployment defaults, and the dated changelog
      agree;
- [ ] the `dev -> main` pull request is merged and required CI passed;
- [ ] `sha-<current-main-commit>` exists and contains `linux/amd64` and
      `linux/arm64` images;
- [ ] the exact candidate connected to the target Panel and carried real
      traffic without unexpected lifecycle or resource failures;
- [ ] no runtime test data, infrastructure identifiers, or secrets were added
      to the repository;
- [ ] `scripts/release-check.sh` passes from a clean current `main` checkout;
- [ ] `v${VERSION}` is annotated, points to the current `main` HEAD, and has
      never been published before; and
- [ ] after publication, the candidate, exact version, and `latest` resolve to
      the same attested digest and the release assets pass `SHA256SUMS`.
