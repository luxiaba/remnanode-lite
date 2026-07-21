<!-- translation: locale=zh-CN; source=docs/release.md; source-sha256=7a775820e98f2a52c97a42e69b15b729a991faf261665ee0266f376c68fb54d7 -->

# 发布 Remnanode Lite

> 这是中文译文；发布规则以[英文原文](../../release.md)为准。

[文档首页](README.md) | [版本模型](versioning.md)

本文介绍维护者如何发布源码版本、二进制归档和容器镜像。核心原则很简单：
发布版本就是当前的 `main` 提交，而该版本使用的容器镜像，必须是此前为同一提交
构建并完成证明的镜像。

正常流程如下：

```text
在 dev 上开发
        |
        v
向 main 发起 Pull Request
        |
        v
CI + 不可变的 sha-<commit> 候选镜像 + edge
        |
        v
维护者使用真实 Panel 和真实流量验证候选镜像
        |
        v
在当前 main HEAD 上创建带注释的 v<version> tag
        |
        v
GitHub Release + 精确版本镜像标签 + latest
```

发布前不再额外创建“最终化”提交。运行时观察结果不存入仓库，GitHub Release notes
由 GitHub 自动生成。

## 1. 版本身份

项目版本与官方 Node 契约版本有关联，但二者不能混为一谈。

| 身份 | 示例 | 含义 |
| --- | --- | --- |
| 项目版本 | `2.8.1-rnl.3` | 本项目源码、二进制、GitHub Release 和精确镜像标签的版本 |
| 契约版本 | `2.8.0` | 当前实现并向 Panel 报告的官方 Node 行为版本 |
| Git tag | `v2.8.1-rnl.3` | 触发 Release workflow 的不可变标签 |
| 镜像标签 | `2.8.1-rnl.3` | 精确容器版本，不带 Git tag 的 `v` 前缀 |

项目版本采用以下两种格式之一：

- `X.Y.Z-rnl.N` 表示 Remnanode Lite 的独立迭代。它既可以继续完善已有契约，
  也可以提前开始下一条项目版本线。仅凭前三段数字，不能声称与对应官方版本兼容。
- `X.Y.Z` 专门用于完成同版本对齐的里程碑。使用这种格式之前，项目版本、契约版本、
  固定的官方源码、代码实现、测试和真实环境表现必须全部一致。

`rnl.N` 在 SemVer 中属于预发布后缀，但本项目不会根据 SemVer 排序推断发布时间。
由 Release workflow 决定哪个已完成的版本晋升为 `latest`。

已经发布的 Git tag 和精确版本镜像标签不可变。`sha-*` 候选标签也按不可变策略管理。
只有 `edge` 和 `latest` 是移动通道：

| 镜像引用 | 用途 |
| --- | --- |
| `sha-<40 位 main commit>` | 从某个 `main` 提交构建、可复现的候选镜像 |
| `edge` | 最近一个符合条件的 `main` 镜像，适合观察，不适合作为回滚依据 |
| `X.Y.Z` 或 `X.Y.Z-rnl.N` | 精确的已发布版本 |
| `latest` | 本项目当前推荐的发布版本 |
| `name@sha256:<digest>` | 按内容寻址的生产环境固定引用 |

## 2. 分支与自动化

仓库长期维护两个分支：

| 分支 | 职责 |
| --- | --- |
| `dev` | 日常开发、集成和回归测试 |
| `main` | 受保护的发布分支，也是容器候选镜像的来源 |

一般先通过主题分支把改动合入 `dev`，再通过 `dev -> main` Pull Request 进入
`main`。直接修改 `main` 不属于正常发布流程。

各 workflow 的职责如下：

| Workflow | 职责 |
| --- | --- |
| [`ci`](../../../.github/workflows/ci.yml) | 运行仓库、Go、安装器和集成检查 |
| [`container`](../../../.github/workflows/container.yml) | 为 Pull Request 构建容器，并为每个 `main` 提交发布多架构候选镜像 |
| [`security`](../../../.github/workflows/security.yml) | 定时或按需运行漏洞检查 |
| [`contract-sync`](../../../.github/workflows/contract-sync.yml) | 报告官方 Node 的新版本，但不会自动修改本仓库 |
| [`release`](../../../.github/workflows/release.yml) | 校验 `v*` tag、发布资产，并晋升已有的候选镜像 |

每次 push 到 `main` 后，只要 container workflow 成功，就会产生以下引用：

```text
ghcr.io/luxiaba/remnanode-lite:sha-<完整 main commit>
ghcr.io/luxiaba/remnanode-lite:edge
```

候选镜像使用完整的 40 位 commit 标识。对当前 `main` 提交手动重跑 workflow 时，
仍使用相同的 `sha-*` 身份，不会产生第二套候选命名空间。

## 3. 在 `dev` 上准备发布

在合入 `main` 之前，应完成所有源码、测试、workflow、部署文件和文档变更。
项目版本与带日期的 `CHANGELOG.md` 条目也应作为同一批开发工作完成。

至少确认以下内容彼此一致：

- `internal/version/version.go`，以及所有包含项目版本的用户可见默认值；
- 契约版本和固定的官方源码；如果契约没有变化，则不要改动它们；
- Compose、安装器和升级工具中的默认值；
- 测试和兼容性文档；
- 当前 `CHANGELOG.md` 标题，格式为 `## [VERSION] - YYYY-MM-DD`。

发起 Pull Request 前，根据改动范围运行相应检查：

```bash
bash scripts/check-go.sh
git diff --check
git status --short
```

升级官方契约是一项完整的兼容性工作，不是简单修改版本字符串。必须固定官方 tag
和 commit，审查路由、schema、错误、行为副作用和插件行为，更新相应实现与测试，
并使用目标 Panel 完成真实集成验证。

## 4. 合并并确定候选镜像

发起 `dev -> main` Pull Request，并在 required checks 全部通过后合并。候选提交是
合并完成后的 `main` HEAD，而不是合并前 `dev` 上的提交：

```bash
git fetch origin main
C="$(git rev-parse origin/main)"
printf '%s\n' "$C"
```

等待 CI 和 `main` 的 container workflow 全部完成。此时，不可变的候选镜像为：

```bash
IMAGE="ghcr.io/luxiaba/remnanode-lite:sha-${C}"
docker buildx imagetools inspect "$IMAGE"
```

发布验证不要使用 `edge`。只要有新的提交进入 `main`，它就可能移动。

评估候选镜像期间，应保持 `main` 不变。如果 `main` 前进了，新的 HEAD 就是新的
候选提交；创建 tag 前需要审查新增改动，并重新执行相关检查。

## 5. 在真实环境中验证候选镜像

使用不可变的 `sha-${C}` 镜像进行验证，也可以先解析 manifest digest，再在测试部署中
固定该 digest：

```bash
CANDIDATE_DIGEST="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "$IMAGE")"
printf '%s\n' "$CANDIDATE_DIGEST" \
  | grep -Eq '^sha256:[0-9a-f]{64}$'

PINNED_IMAGE="ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}"
```

发布之前，维护者应确认这个确定的候选镜像：

- 能正常启动，并在预期的 Compose 资源限制下保持健康；
- 能连接目标 Panel，并报告预期的项目版本和契约版本；
- 能正确承载真实代理流量；
- 测试期间没有异常重启、OOM 或生命周期问题。

这是一次人工发布判断，不是仓库中的发布资料。不要提交宿主机清单、容器标识、时间戳、
IP 地址、Panel 信息、日志、smoke JSON 或其他运行时观察结果。Secret 绝不能进入 Git
或 GitHub Release。

Release workflow 会独立验证供应链身份。维护者也可以在创建 tag 前运行同样的
attestation 检查：

```bash
gh attestation verify \
  "oci://ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}" \
  --repo luxiaba/remnanode-lite \
  --cert-identity \
    https://github.com/luxiaba/remnanode-lite/.github/workflows/container.yml@refs/heads/main \
  --source-digest "$C" \
  --deny-self-hosted-runners
```

## 6. 创建发布 tag

只能给当前远端 `main` HEAD 创建 tag。先准备一个干净且与远端同步的本地工作区：

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

使用已固定的官方源码 checkout 运行发布预检：

```bash
export REMNANODE_OFFICIAL_SOURCE=/path/to/remnawave-node
export REQUIRE_GOVULNCHECK=1

RELEASE_TAG="$TAG" bash scripts/release-check.sh
```

然后创建并验证带注释的 tag：

```bash
git tag -a "$TAG" -m "release ${TAG}"

RELEASE_TAG="$TAG" \
REQUIRE_TAG_AT_HEAD=1 \
  bash scripts/release-check.sh

git push origin "$TAG"
```

候选测试与创建 tag 之间，不需要额外的纯文档最终化分支或提交。如果预检发现版本、
CHANGELOG、代码或文档问题，应通过 `dev` 修复，合入新的 `main` 提交，并验证新的
候选镜像后再创建 tag。

## 7. Release workflow 会发布什么

由 tag 触发的 workflow 会依次执行以下操作：

1. 确认 tag 指向当前 `main` HEAD，并确认 tag、项目版本、契约元数据和带日期的
   CHANGELOG 一致。
2. 使用 tag 对应的源码重新运行发布检查。
3. 解析 `sha-${GITHUB_SHA}`，确认候选镜像是 OCI index，其中恰好包含一个可运行的
   `linux/amd64` 镜像和一个可运行的 `linux/arm64` 镜像，并且每个平台都有预期的
   BuildKit attestation manifest。
4. 验证 GitHub build attestation，确保它来自本仓库、`refs/heads/main` 上准确的
   `container.yml` workflow 身份，以及当前 tag 对应的源码提交。
5. 构建可下载的发布资产和对应的 `SHA256SUMS` 文件。
6. 创建 GitHub Release，并自动生成 Release notes。
7. 将已经验证的候选 digest 先晋升为精确版本标签，再晋升为 `latest`。发布过程中
   不会重新构建容器。

发布资产包括：

```text
remnanode-lite_linux_amd64.tar.gz
remnanode-lite_linux_arm64.tar.gz
asn-prefixes.bin
compose.yaml
docker-compose.single-file.yaml
remnanode.env.example
SHA256SUMS
```

如果精确版本标签已经指向另一个 digest，发布会拒绝覆盖。在移动 `latest` 之前，
workflow 会重新获取 `main`；过期 tag 无法让稳定通道回退。共享的 registry concurrency
group 还会避免候选镜像和正式版本的晋升过程相互竞争。

完成上述 workflow 后，`X.Y.Z` 与 `X.Y.Z-rnl.N` 都属于本项目的稳定发布版本。
实验性工作只能留在 `dev` 或 `sha-*`/`edge` 通道，不应创建正式 tag。

## 8. 验证已发布版本

workflow 成功后，确认候选标签、精确版本标签和 `latest` 都解析为同一个 manifest
digest：

```bash
VERSION=X.Y.Z                    # 也可以是 X.Y.Z-rnl.N
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

检查精确版本镜像，确认两个受支持的平台都存在：

```bash
docker buildx imagetools inspect "${IMAGE}:${VERSION}"
```

根据发布提交验证 attestation：

```bash
gh attestation verify "oci://${IMAGE}@${VERSION_DIGEST}" \
  --repo luxiaba/remnanode-lite \
  --cert-identity \
    https://github.com/luxiaba/remnanode-lite/.github/workflows/container.yml@refs/heads/main \
  --source-digest "$C" \
  --deny-self-hosted-runners
```

可以使用随 Release 发布的 checksum 文件校验下载内容：

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

## 9. 失败与重试

GitHub、registry 或网络出现暂时性故障时，优先使用 **Re-run failed jobs**。发布流程按
幂等方式设计：已有的精确版本标签只有在指向预期 digest 时才会被接受，已有 Release
资产也不会被覆盖。

| 失败位置 | 预期状态 | 恢复方式 |
| --- | --- | --- |
| 预检、候选、OCI 或 attestation 检查 | 没有新的正式镜像 | 通过 `dev` 修复源码或候选镜像，并使用新的 `main` 提交 |
| 发布 GitHub Release 资产 | Release 可能不完整；精确版本镜像尚未晋升 | 如果属于暂时性故障，重新运行失败的 jobs |
| 晋升精确版本标签 | Release 资产可能已经存在；`latest` 不变 | 重新运行，并晋升同一个已验证 digest |
| 晋升 `latest` 或设置 GitHub Latest | 精确版本仍可正常使用 | 确认当前 `main` HEAD 后重新运行晋升 job |

tag 一旦推送，就不要移动、覆盖、删除后复用或强制推送。需要修改仓库才能解决的
非暂时性问题，必须使用新的项目版本和新的候选镜像。绝不能通过改写精确版本镜像标签
来掩盖失败或错误的发布。

## 10. 回滚

生产部署应记录精确版本或 manifest digest，并保留上一个版本。需要回滚容器节点时，
恢复之前的镜像引用及其配套部署配置，再重新创建服务：

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
```

不要把 `latest` 当作回滚身份。它只是会移动的推荐版本，也不会自动更新正在运行的
容器。大规模部署应分批进行，并始终保留可用的已知良好版本或 digest。日常容器运维
说明见 [`deployment-docker.md`](deployment-docker.md)。

使用原生 systemd 或 OpenRC 安装时，可以明确指定已发布 tag：

```bash
sudo RNL_TAG=vX.Y.Z bash upgrade.sh --yes
```

## 11. 跟进官方 Node 版本

`contract-sync` workflow 会检查官方 `remnawave/node` 是否发布新版本，发现后创建
Issue。它绝不会自动修改代码、契约元数据、项目版本、Git tag 或镜像。

跟进新的官方版本时：

1. 记录官方 tag，并固定其完整 commit。
2. 审计路由、schema、错误、行为副作用、插件和运行时差异。
3. 更新官方源码契约依据、实现和自动化测试。
4. 使用目标 Panel 和真实代理流量验证结果。
5. 只有实现了新基线后，才能修改契约版本。
6. 只有完成同版本对齐后，才能发布纯 `X.Y.Z`；否则应如实使用
   `X.Y.Z-rnl.N` 项目版本。

官方发布新版本只意味着兼容性工作可以开始。它不会自动决定本项目的下一个版本，
也不会代表本项目发布任何内容。

## 12. 维护者检查清单

推送最终 tag 之前，确认：

- [ ] 项目版本采用允许的 `X.Y.Z` 或 `X.Y.Z-rnl.N` 格式；
- [ ] 纯版本号与已经实现并固定的契约版本一致；
- [ ] 版本元数据、测试、部署默认值和带日期的 CHANGELOG 一致；
- [ ] `dev -> main` Pull Request 已合并，required CI 已通过；
- [ ] `sha-<当前 main commit>` 存在，并包含 `linux/amd64` 和 `linux/arm64` 镜像；
- [ ] 确定的候选镜像已连接目标 Panel、承载真实流量，且没有异常生命周期或资源问题；
- [ ] 仓库中没有加入运行测试数据、基础设施标识或 secret；
- [ ] 在干净且指向当前 `main` 的工作区中，`scripts/release-check.sh` 检查通过；
- [ ] `v${VERSION}` 是带注释的 tag，指向当前 `main` HEAD，且从未发布过；
- [ ] 发布后，候选标签、精确版本标签和 `latest` 指向同一个已证明的 digest，
      Release 资产也通过 `SHA256SUMS` 校验。
