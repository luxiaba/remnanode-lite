# Release and Version Maintenance Guide

[Documentation home](README.md) | [Versioning model](versioning.md)

This is the maintainer's release guide. It covers the path from routine
development to a frozen candidate, real-environment acceptance, a GitHub
Release, and the final GHCR tags. The same process applies to every version.

See [`versioning.md`](versioning.md) for the normative version and image-channel
rules. This guide explains how to turn those rules into a verifiable release.

The process has three primary goals:

1. Every final release is traceable to one specific commit on the protected
   `main` branch.
2. Acceptance records bind the candidate commit, binary digests, and the exact
   container manifest digest that was tested.
3. `latest` always refers to the most recently completed release whose
   candidate attestation was verified. Re-running an older release must never
   move `latest` backwards.

## 1. Version Model

The project version and the official Node contract version are separate
concepts.

| Name | Example | Meaning |
| --- | --- | --- |
| Project version | `2.8.1-rnl.9` | Identity of this remnanode-lite build and Release |
| Contract version | `2.8.0` | Official Node contract baseline currently proven by the code and reported to the Panel by default |
| Git tag | `v2.8.1-rnl.9` | Triggers the final release workflow and becomes immutable after publication |
| Image tag | `2.8.1-rnl.9` | Exactly matches the project version, without the Git tag's `v` prefix |

Final releases may use only these formats:

- `X.Y.Z-rnl.N`: an independent project iteration. `N` has no direct
  relationship to the official release sequence. The project may start work on
  a future version early or continue improving an already aligned official
  version.
- `X.Y.Z`: an official-alignment release. This form is permitted only after the
  current contract, pinned official source, implementation, tests, and
  real-environment acceptance have all been aligned with `X.Y.Z`.

For a plain `X.Y.Z` release, the project version must equal the contract
version. The first three components of `X.Y.Z-rnl.N` do not claim official
compatibility; the actual compatibility range is always defined by the
contract version and the compatibility matrix in the Release notes.

The following progressions are all valid:

```text
Project version     Contract version     Meaning
2.8.1-rnl.1         2.8.0                Begin the 2.8.1 project line early while still reporting the 2.8.0 contract
2.8.1               2.8.1                Plain release after official 2.8.1 alignment is complete
2.8.1-rnl.9         2.8.1                Continue project-specific improvements on an aligned version
2.8.2-rnl.1         2.8.1                Begin the next project development line early
```

In SemVer syntax, `-rnl.N` is a prerelease suffix, so SemVer sorts
`2.8.1-rnl.9` before `2.8.1`. This project does not use SemVer ordering to
determine release chronology or the target of `latest`. The complete release
gates determine stability, and the actual release history determines order.

### 1.1 Tag Immutability

- A published final Git tag, `v${VERSION}`, must never be moved, overwritten,
  or reused.
- Published exact-version GHCR tags and `sha-*` tags must never be deliberately
  overwritten.
- `edge` and `latest` are explicitly mutable aliases and must not be used as
  rollback identities.
- Builds that have not completed final acceptance may use only `edge`, `sha-*`,
  or `candidate-sha-*`; they must not receive a final `v*` Git tag.
- `latest` is this project's stable image channel. It does not refer to the
  `latest` tag of the official `remnawave/node` image.

## 2. Branch and Automation Boundaries

The repository maintains two long-lived branches:

| Branch | Responsibility | Normal entry path |
| --- | --- | --- |
| `dev` | Routine development, integration, and regression testing | Pull request from a short-lived topic branch after CI passes |
| `main` | Source of release candidates and final releases | Pull request from `dev` after CI passes |

The GitHub Actions workflows have distinct responsibilities:

| Workflow | Trigger | Output or responsibility |
| --- | --- | --- |
| [`ci`](../.github/workflows/ci.yml) | Pushes to `dev`/`main` and relevant pull requests | Go, repository, installer, and Linux network-management gates, summarized by `ci / gate` |
| [`container`](../.github/workflows/container.yml) | Pushes or pull requests to `dev`/`main` that change container inputs | Builds only on `dev` and pull requests; on `main`, builds and attests by digest before publishing `sha-<commit>` and `edge` |
| [`security`](../.github/workflows/security.yml) | Scheduled or manual | Scans for reachable Go vulnerabilities |
| [`contract-sync`](../.github/workflows/contract-sync.yml) | Scheduled or manual | Checks the pinned official contract and opens an issue when a new official version appears; never changes code automatically |
| [`release`](../.github/workflows/release.yml) | Push of a `v*` tag | Runs final gates, verifies candidate attestation, creates the GitHub Release, and promotes exact and `latest` GHCR tags |

`ci` and `container` are not duplicate pipelines. The former verifies the code
and repository; the latter verifies or publishes the container. Branch
protection should always require `ci / gate`, which cannot disappear because of
path filtering. The path-filtered `container` workflow is unsuitable as the
only required check.

## 3. Routine Development

All features, fixes, dependency changes, workflows, deployment assets, and
long-lived documentation changes first enter `dev` through a topic branch:

```bash
git switch dev
git pull --ff-only origin dev
git switch -c chore/prepare-next-release

# Make the changes and run checks appropriate to their risk.
bash scripts/check-go.sh

git status --short
git diff --check
git add <explicit-file-list>
git diff --cached --check
git commit -m "type(scope): describe the change"
git push -u origin chore/prepare-next-release

# Open a chore/prepare-next-release -> dev pull request on GitHub.
# Merge it after CI succeeds and the review is complete.
```

Use this pull-request path even when one person maintains the repository. It
keeps the change, CI result, and review context together on `dev`; direct pushes
are not part of the normal release path.

Before release, update all version metadata on `dev`. Define the intended
project version as:

```bash
VERSION=X.Y.Z-rnl.N   # Or X.Y.Z after official alignment is complete.
TAG="v${VERSION}"
```

The version update must cover the application version, default tags in the
install and upgrade scripts, default container images, contract-probe identity,
the root [`CHANGELOG.md`](../CHANGELOG.md), and related tests. Merely entering a
new `rnl` project line must not silently change the contract version or the
official source pin.

An official contract upgrade is a separate compatibility project. At minimum,
it requires:

- pinning the new official Node tag and full commit;
- updating the contract version and version reported to the Panel;
- re-auditing routes, schemas, errors, and side effects;
- updating the implementation and automated tests; and
- updating compatibility documentation, the real Panel target, and the
  acceptance scope.

## 4. Merge the Code Candidate into `main`

After regression testing on `dev`, open a `dev -> main` pull request. The code
pull request may use any normal merge method allowed by the repository. Define
candidate commit `C` as the final commit on `main` after that pull request has
been merged, not the pre-merge commit from `dev`.

```bash
git fetch origin dev main
git switch main
git pull --ff-only origin main

C="$(git rev-parse HEAD)"
git rev-parse "${C}^{commit}"
git rev-parse "${C}^{tree}"
```

Freeze `main` at this point. The freeze covers Go code, tests, scripts,
workflows, Dockerfiles, Compose files, service definitions, and all
documentation outside the release-finalization allowlist. If `main` changes
beyond that allowlist during acceptance, the original evidence becomes
invalid. Treat the new `main` commit as `C` and repeat the affected acceptance
work.

Do not create the final `v${VERSION}` tag yet. The full 40-character commit is
sufficient to identify a candidate. If a local marker is genuinely useful, it
may include the short commit SHA, but it must not be treated or pushed as a
Release tag.

## 5. Accept the Candidate Image

When the candidate changes a container input, the `container` workflow on
`main` first builds a multi-architecture manifest without a user-facing tag. It
produces BuildKit SBOM/provenance and a GitHub build attestation before
publishing:

```text
ghcr.io/luxiaba/remnanode-lite:edge
ghcr.io/luxiaba/remnanode-lite:sha-${C}
```

The workflow refuses to move `sha-${C}` after its first publication. It updates
`edge` only while `C` remains the current `main` HEAD. Use `sha-${C}` to locate
the automatic candidate, then treat the manifest digest returned by the
registry as the canonical image identity for acceptance. Download the Compose
and deployment assets from the same `C` so the image and its configuration
cannot come from different commits.

```bash
IMAGE="ghcr.io/luxiaba/remnanode-lite:sha-${C}"
docker pull "$IMAGE"
docker buildx imagetools inspect "$IMAGE"

CANDIDATE_DIGEST="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "$IMAGE")"
printf '%s\n' "$CANDIDATE_DIGEST" \
  | grep -Eq '^sha256:[0-9a-f]{64}$'

gh attestation verify \
  "oci://ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}" \
  --repo luxiaba/remnanode-lite \
  --cert-identity https://github.com/luxiaba/remnanode-lite/.github/workflows/container.yml@refs/heads/main \
  --source-digest "$C" \
  --deny-self-hosted-runners
```

A manual candidate run from `main` publishes the policy-immutable
`candidate-sha-${C}` tag without overwriting an automatic `sha-${C}`. The two
tags may resolve to different digests even though they share a source commit.
Either image may enter acceptance, but its full commit and manifest digest must
remain fixed through publication. The final release verifies that digest and
its attestation directly, regardless of which tag first exposed it.

The `docker-production-smoke-v2` profile must use
[`deploy/compose.single-file.yaml`](../deploy/compose.single-file.yaml) from
`C`, with its image reference replaced by
`ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}`. The evidence records both
the file SHA-256 of that template in the candidate Git object and the digest
that actually ran. Running only `docker compose config`, or testing another
build behind the same tag, cannot substitute for acceptance of the final
candidate. See [Docker production smoke](development/release-acceptance.md#docker-production-smoke)
for the complete schema and collection rules.

## 6. Freeze the Candidate and Run Real-Environment Acceptance

Final acceptance must target the same `C`. Build native binaries from a clean
worktree with `scripts/build-release-binaries.sh`. The official Git repository
must contain the pinned commit for the current contract baseline. The source
oracle reads that commit object directly and does not trust its checkout,
index, or `HEAD`:

```bash
export REMNANODE_OFFICIAL_SOURCE=/path/to/pinned-remnawave-node
export REQUIRE_GOVULNCHECK=1

bash scripts/check.sh
git status --short
```

The authoritative scope is defined in
[`development/release-acceptance.md`](development/release-acceptance.md).
Schema version 2 uses the version-specific
`docker-production-smoke-v2` profile. Its blocking scope is:

- the protected-branch GitHub CI gate for `C`;
- a candidate image manifest built for `linux/amd64` and `linux/arm64`,
  with SBOM, provenance, and GitHub build attestation;
- both architecture-specific release binaries built from `C` and identified
  by SHA-256;
- one candidate-bound, digest-pinned run of the canonical single-file Compose
  template on a recorded real native amd64/x86_64 host;
- the actual host memory, CPU, disk, and swap inventory as evidence, without a
  whole-host size admission limit;
- a running and healthy container with exactly 448 MiB memory, 448 MiB
  memory-plus-swap, 1 CPU, and 256 PIDs, zero OOM kills and restarts, and
  positive memory/PID observations; and
- Panel 2.8.1 online with real proxy traffic, plus Release Owner signoff.

The operator-attested runtime record is auditable and bound to the candidate,
but the validator cannot independently prove that the reported Panel session or
traffic observation occurred. It is not an unforgeable proof.

The profile records these exact deferred, non-blocking validations:
`whole-host-512mib-runtime`, `arm64-production-runtime`, `native-systemd-install`,
`native-openrc-install`, `50000-user-load`, `24h-soak`, and
`fault-and-rollback-injection`. A Release note must disclose them and must not
present the dated M6/M7 engineering baselines as candidate runtime acceptance.

Store acceptance material under the directory for the current project version:

```text
docs/development/acceptance/v${VERSION}/
  manifest.json
  docker-smoke.json
```

`cmd/release-evidence-check` pins schema version 2, the acceptance profile,
version, official commit, Panel, rw-core, deferred list, smoke thresholds, and
signoff identity. A later project or contract version must update and test its
profile in a normal code pull request before `C` is frozen. Do not relax the
profile in the tag workflow or reuse evidence from an older release.

`manifest.json` records `C`, its tree, `CANDIDATE_DIGEST`, both Node binary
hashes, the project/tag/contract identities, deferred validation, risks, and the
SHA-256 of the complete `docker-smoke.json` file. The smoke record binds the
canonical Compose blob in `C` to the same image digest, the amd64 Node binary,
the observed limits and resource use, Panel and traffic results, the sanitized
raw-bundle digest, and operator signoff.

Do not put `F` in the manifest; it does not exist yet. Never commit a Secret
Key, JWT, CA, certificate, private key, IP address, hostname, Panel URL, raw
response body, or data that could identify a user.

## 7. Commit Release Material under Protected `main`

After candidate acceptance succeeds, only release-finalization material may be
changed. The current allowlist is:

```text
README.md
README.zh-CN.md
README.ru.md
CHANGELOG.md
docs/development/roadmap.md
docs/i18n/zh-CN/development/roadmap.md
docs/development/acceptance/v${VERSION}/manifest.json
docs/development/acceptance/v${VERSION}/docker-smoke.json
docs/releases/v${VERSION}.md
```

Complete all other long-lived documentation before acceptance. Changes after
`C` to architecture, configuration, deployment documentation, or this release
guide invalidate the candidate.

Create a dedicated documentation branch from `C`:

```bash
git switch --detach "$C"
git switch -c "release/v${VERSION}-docs"

# Add evidence and Release notes, update CHANGELOG, and move the canonical
# README/roadmap plus their maintained translations to released status.
git add README.md README.zh-CN.md README.ru.md CHANGELOG.md \
  docs/development/roadmap.md docs/i18n/zh-CN/development/roadmap.md \
  "docs/development/acceptance/v${VERSION}" \
  "docs/releases/v${VERSION}.md"
git diff --cached --check
git commit -m "docs(release): record v${VERSION} acceptance"
git push -u origin "release/v${VERSION}-docs"
```

Open a pull request from this branch to `main` and merge it with **squash
merge**. Other merge methods do not satisfy the release gate.

The evidence validator examines every commit in `C..HEAD` and rejects:

- any merge commit with two or more parents;
- changes outside the allowlist;
- code drift that was later reverted; and
- evidence that disagrees with `HEAD`, the Git index, or the worktree.

Therefore, do not use a regular merge commit for the release-material pull
request. `main` must still be at `C` when it is merged. If another commit has
entered `main`, do not rebase and continue using the old evidence; reassess the
changes and freeze a new candidate.

After the squash merge, call the final release commit `F`:

```bash
git fetch origin main
git switch main
git pull --ff-only origin main

F="$(git rev-parse HEAD)"
git merge-base --is-ancestor "$C" "$F"
git diff --name-only "$C..$F"
```

The final tag points to `F`, not candidate code commit `C`. The validator proves
that `F` differs from `C` only by permitted release material.

## 8. Release Note Requirements

Every final version must provide:

```text
docs/releases/v${VERSION}.md
```

Its first line must be:

```markdown
# v${VERSION}
```

The Release notes must contain at least these sections:

```markdown
## Compatibility
## Acceptance Results
## Known Risks
## Installation and Upgrade
```

The compatibility section must state the project version, contract version,
pinned official commit, target Panel, rw-core, and packaged architectures
separately from runtime-validated architectures. Acceptance results must name
`docker-production-smoke-v2`, include candidate commit `C`,
`candidateImageDigest`, and the exact relative link required by the gate:

```markdown
[Acceptance manifest](../development/acceptance/v${VERSION}/manifest.json)
```

A Release note cannot contain its own commit `F`: that SHA does not exist until
the note has been committed. After publication, resolve `F` from
`git rev-list -n 1 v${VERSION}` or from the GitHub Release target. The Known
Risks section must list every deferred check instead of saying only "none." Use
one line per token in this machine-checkable form:

```markdown
- `whole-host-512mib-runtime`: deferred; not validated by `docker-production-smoke-v2`.
```

Apply the same form to `arm64-production-runtime`, `native-systemd-install`,
`native-openrc-install`, `50000-user-load`, `24h-soak`, and
`fault-and-rollback-injection`. The section must also contain these exact lines:

```text
The smoke validates the canonical container limits on the recorded host; whole-host 512 MiB / 1 vCPU / 2 GB runtime remains deferred.
```

```text
Runtime evidence is operator-attested and is not an unforgeable proof.
```

The file must not contain placeholders such as `TODO`, `TBD`, `Unreleased`, or
"in progress."

## 9. Final Gate and Tag

Run the final checks from a clean worktree at the latest `main`:

```bash
git fetch origin main --tags
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)"

VERSION="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' internal/version/version.go)"
TAG="v${VERSION}"

RELEASE_TAG="$TAG" \
REMNANODE_OFFICIAL_SOURCE="$REMNANODE_OFFICIAL_SOURCE" \
REQUIRE_GOVULNCHECK=1 \
  bash scripts/release-check.sh
```

After confirming the version, evidence, and final commit, create an annotated
tag:

```bash
git tag -a "$TAG" -m "release ${TAG}"

RELEASE_TAG="$TAG" \
REMNANODE_OFFICIAL_SOURCE="$REMNANODE_OFFICIAL_SOURCE" \
REQUIRE_GOVULNCHECK=1 \
REQUIRE_TAG_AT_HEAD=1 \
  bash scripts/release-check.sh

git push origin "$TAG"
```

If the workflow fails for a non-transient reason after the tag is pushed, never
force-move the original tag. A fix that changes source code requires a new
project version and a new candidate cycle.

Keep `main` frozen from the moment `C` is selected until `latest` promotion and
post-release verification finish. If `main` advances while the final workflow
is running, the exact release remains an auditable artifact, but promotion
refuses to make it `latest`. To recommend the newer mainline state, prepare a
subsequent version from the new `main` HEAD; do not bypass the HEAD guard.

## 10. Tag-Triggered Release Automation

When [`.github/workflows/release.yml`](../.github/workflows/release.yml) receives
`v${VERSION}`, it runs these steps in order:

1. Verify that the tagged commit is the current `origin/main` HEAD, then rerun
   version, evidence, code, supply-chain, and Linux namespace gates.
2. Read `C` and the accepted digest from the acceptance manifest. Confirm that
   the digest still exists and strictly verify its attestation repository,
   signing workflow, source commit, and `refs/heads/main` source. The digest
   does not have to be discoverable through one particular candidate alias.
3. Build linux/amd64 and linux/arm64 binary archives, the compact ASN database,
   standard Compose, single-file Compose, the environment template, and
   `SHA256SUMS`. The evidence validator compares the Node binary digest for both
   architectures. Both Compose assets retain the `remnanode-lite` service,
   container, and hostname and the same explicit `.env` interpolation mapping.
4. Create the GitHub Release from `docs/releases/v${VERSION}.md`, but do not yet
   mark it as GitHub's Latest Release. Existing assets with the same names are
   not overwritten.
5. Do not rebuild the container. Publish the accepted and attested
   `CANDIDATE_DIGEST` under the policy-immutable exact version:

   ```text
   ghcr.io/luxiaba/remnanode-lite:${VERSION}
   ```

6. Only after exact-version publication succeeds, and only while the tagged
   commit remains the current `origin/main` HEAD, promote the same attested
   digest to GHCR `latest` and mark the corresponding GitHub Release as Latest:

   ```text
   ghcr.io/luxiaba/remnanode-lite:latest
   ```

The image provenance and OCI revision refer to candidate commit `C`. The Git tag
and GitHub Release refer to `F`, which adds only release material. The
acceptance manifest and Release notes record `C` and the digest; the Git tag
identifies `F`. The exact-version image is the accepted image from `C`, not a
second image rebuilt from `F`.

Exact-version publication creates the tag when absent and otherwise requires
the same digest, so a rerun cannot replace its content. Promoting `latest` moves
only that floating tag. Before doing so, the promotion job fetches
`origin/main` again and checks its HEAD; an old tag cannot update GHCR `latest`
or GitHub's Latest Release. A repository-wide concurrency group prevents two
releases from racing to update the registry.

Both plain `X.Y.Z` and `X.Y.Z-rnl.N` are stable Releases after passing the same
final gates, and both are eligible for automatic promotion to `latest`. Do not
publish experimental builds by weakening the GitHub Release prerelease flag;
keep them in the candidate image channels.

## 11. Post-Release Verification

Let `C` be the candidate commit and `F` the final material commit:

```bash
VERSION=X.Y.Z-rnl.N   # Or X.Y.Z.
C=REPLACE_WITH_40_CHAR_CANDIDATE_COMMIT
F="$(git rev-list -n 1 "v${VERSION}")"
CANDIDATE_DIGEST=sha256:REPLACE_WITH_64_HEX_DIGEST
IMAGE="ghcr.io/luxiaba/remnanode-lite:${VERSION}"
CANDIDATE_IMAGE="ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}"
LATEST_IMAGE="ghcr.io/luxiaba/remnanode-lite:latest"
```

Inspect the multi-architecture manifests:

```bash
docker buildx imagetools inspect "$IMAGE"
docker buildx imagetools inspect "$CANDIDATE_IMAGE"
```

The output must contain both `linux/amd64` and `linux/arm64`. The exact version
and the candidate reference in the acceptance manifest must resolve to the same
manifest digest. The normal automatic candidate tag, `sha-${C}`, or the manual
candidate tag actually used for acceptance should also resolve to that digest
while it remains available.

Verify the GitHub attestation:

```bash
gh attestation verify \
  "oci://ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}" \
  --repo luxiaba/remnanode-lite \
  --cert-identity https://github.com/luxiaba/remnanode-lite/.github/workflows/container.yml@refs/heads/main \
  --source-digest "$C" \
  --deny-self-hosted-runners
```

If this release was promoted to the stable channel, compare `latest`:

```bash
docker buildx imagetools inspect "$LATEST_IMAGE"
```

`latest`, the exact version, and the accepted candidate digest must all resolve
to the same manifest digest. If final tag commit `F` stopped being the `main`
HEAD during publication, it is expected that `latest` remains on the previous
stable release.

Verify the GitHub Release assets:

```bash
BASE_URL="https://github.com/luxiaba/remnanode-lite/releases/download/v${VERSION}"
mkdir -p "/tmp/remnanode-release-${VERSION}"
cd "/tmp/remnanode-release-${VERSION}"

curl -fLO "$BASE_URL/SHA256SUMS"
curl -fLO "$BASE_URL/remnanode-lite_linux_amd64.tar.gz"
curl -fLO "$BASE_URL/remnanode-lite_linux_arm64.tar.gz"
curl -fLO "$BASE_URL/asn-prefixes.bin"
curl -fLO "$BASE_URL/compose.yaml"
curl -fLO "$BASE_URL/docker-compose.single-file.yaml"
curl -fLO "$BASE_URL/remnanode.env.example"
sha256sum --check SHA256SUMS
```

`SHA256SUMS` checks that the downloads match the files produced by the workflow.
The GitHub attestation covers the container build only; it does not attest the
binary archives unless the release workflow later adds file-level
attestations.

The two Compose assets must resolve to the same `remnanode-lite` service when
given the same `.env`. Compose interpolates only the explicitly mapped runtime
settings, shell values override `.env`, and an unset or empty `SECRET_KEY` must
make configuration expansion fail before container creation.

## 12. Partial Failures and Recovery

The release workflow creates external state in stages. After a failure, first
identify which objects already exist, then choose the narrowest safe retry.

| Failure point | Existing state | Recovery | State of `latest` |
| --- | --- | --- | --- |
| Gate, candidate digest, or attestation verification fails | No new Release or final image | Correct the evidence or candidate state; source changes require a new candidate and version | Unchanged |
| GitHub Release succeeds but exact-version publication fails | Release assets may already exist | Re-run only the failed job; neither existing Release assets nor the target tag may be overwritten | Unchanged |
| Exact version succeeds but GHCR `latest` promotion fails | Exact version, candidate proof, and Release assets are complete | Re-run only the promotion job and promote the same accepted digest | Unchanged until promotion succeeds |
| GHCR `latest` is promoted but the GitHub Latest flag fails | GHCR stable channel is updated, but the GitHub Release is not yet marked Latest | Re-run the promotion job; the same-digest GHCR operation is idempotent, then apply the GitHub flag | GHCR is updated; the GitHub UI temporarily lags |
| An unfinished job from an older run is retried | The historical Release or image may already exist in part | Repair only that exact version; promotion must fetch and check the current `main` HEAD again | Must not move backwards |

Prefer GitHub Actions' **Re-run failed jobs** action. A full rerun preserves
existing Release assets and permits the exact version only to remain on the
same digest. If existing state disagrees with the acceptance manifest, the
workflow must fail; do not delete or overwrite evidence to conceal the
conflict. Record the relationship among the GitHub Release, GHCR manifest,
attestation, and workflow run before deciding whether a new project version is
required.

If `latest` points to the wrong digest because of a workflow defect or manual
operation, handle it as a release incident. Record the incorrect and previous
stable digests, restore `latest` through a controlled promotion, and then fix
the workflow. Never mutate a published exact-version tag to hide the incident.

## 13. Rollback

For container deployments, roll back to the previous stable exact tag or
manifest digest:

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
```

Restore the matching Compose file and configuration at the same time. Do not
move an old version tag or force `latest` to an arbitrary historical build as a
substitute for node-level rollback.

`latest` never replaces a running container automatically. Nodes that choose
to follow the stable channel still have to run `docker compose pull` and
recreate the service. Roll out fleet updates in batches and retain the previous
exact version or digest as a rollback point. See
[`deployment-docker.md`](deployment-docker.md) for complete container
operations.

Native systemd/OpenRC deployments may roll back only to tags actually published
by this project:

```bash
sudo RNL_TAG=vX.Y.Z-rnl.N bash upgrade.sh --yes
```

The upgrader verifies Release checksums and binary versions, then restores the
binary, service, support files, configuration, and runtime state according to
its transaction rules.

## 14. Synchronizing with Official Releases

The `contract-sync` workflow periodically checks the latest official
`remnawave/node` Release. When it detects a change, it opens a synchronization
issue only. It never changes the contract, project version, code, tags, or
images automatically.

For a new official version:

1. Record the official tag and full commit.
2. Audit differences in routes, schemas, errors, plugins, and runtime behavior.
3. Update contract evidence, implementation, and tests.
4. Select an appropriate project version. It may be a new `rnl` version; an
   immediate plain official version is not required.
5. Update the contract version to the verified baseline only after completing
   real-environment acceptance.
6. Publish a plain `X.Y.Z` only after same-version official alignment is
   complete.

The project's maintenance plan still determines its next version. A new
official Release starts compatibility work; it does not choose the next
`rnl.N` automatically.

## 15. Final Checklist

Before pushing a final tag, confirm every item:

- [ ] `VERSION` uses an allowed plain or `rnl` format.
- [ ] A plain version equals the contract version; an `rnl` version's actual
      contract is explicit in the documentation.
- [ ] Version metadata, scripts, Compose files, probes, `CHANGELOG.md`, and tests
      agree.
- [ ] The code pull request has entered protected `main`, and candidate `C` is
      the post-merge `main` commit.
- [ ] Both `ci` and the candidate container workflow passed for `C`.
- [ ] `manifest.json` and `docker-smoke.json` bind the same `C`, candidate
      tree, multi-architecture manifest digest, candidate binary hashes, and
      canonical Compose blob.
- [ ] `docker-production-smoke-v2` passed on a recorded real native
      amd64/x86_64 host for at least 600 seconds with exact 448 MiB memory,
      448 MiB memory-plus-swap, 1 CPU, and 256 PID container limits; the
      container remained healthy with zero OOM kills/restarts, valid memory/PID
      observations, Panel 2.8.1 online, and real proxy traffic.
- [ ] The exact deferred list is disclosed as non-blocking and unvalidated:
      whole-host 512 MiB runtime, arm64 runtime, native systemd/OpenRC,
      50,000-user load, 24-hour soak, and fault/rollback injection.
- [ ] Release Owner signoff and the sanitized raw-bundle digest are recorded
      without presenting operator-attested evidence as unforgeable proof.
- [ ] The release-material pull request changes only the finalization allowlist
      and is squashed to exactly one single-parent commit.
- [ ] README no longer contains a pre-release notice, and the roadmap marks this
      version's M8 milestone complete.
- [ ] Final commit `F` is the current `origin/main` HEAD.
- [ ] `scripts/release-check.sh` passes from a clean worktree.
- [ ] `v${VERSION}` is an annotated tag pointing to `F` and has never been
      published before.
- [ ] After the tag push, the GitHub Release, exact image, candidate attestation
      verification, and `latest` promotion all succeed.
- [ ] The exact version and `latest` equal the acceptance digest, the candidate
      alias used in acceptance still resolves to that digest, and both amd64
      and arm64 are present.
- [ ] The production rollout records an exact version or digest and retains an
      executable rollback target.
