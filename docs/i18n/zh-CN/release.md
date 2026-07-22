<!-- translation: locale=zh-CN; source=docs/release.md; source-sha256=1b293b98db8706c3a64fa7f8c036e02a00162ce6d35074dcc44284133ca002c2 -->

# 发布 Remnanode Lite

> 这是中文译文；发布规则以[英文原文](../../release.md)为准。

[文档首页](README.md) | [版本模型](versioning.md)

本文介绍维护者如何发布 Remnanode Lite。发布版本对应 publish workflow 创建
annotated tag 时选定的、已经审查过的 `main` 提交。容器则使用此前为同一提交构建并
完成证明的多架构候选镜像；Release workflow 只验证并重新标记这个 digest，不会重新
构建容器。

发布资产采用先 draft、后公开的流程。workflow 会在 GitHub Release 仍为 draft 时
完成构建、校验、attestation、上传和逐项比对；只有精确的容器版本标签准备好后才会
发布该 draft，随后把同一个 digest 晋升到 `preview` 或 `latest`。

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
手动运行 publish workflow
        |
        v
workflow 创建带注释的 v<version> tag
        |
        v
验证过的 draft Release 与资产
        |
        v
精确 GHCR 版本标签
        |
        v
发布 GitHub Release
        |
   +----------------------+----------------------+
   |                                             |
X.Y.Z-rnl.N 预览版                         X.Y.Z 稳定版
GitHub Prerelease                         完整 Release
GHCR preview                              GHCR latest
```

发布前不再额外创建“最终化”提交。运行时观察结果不存入仓库，GitHub Release notes
由 GitHub 自动生成。

## 1. 版本身份

项目版本与官方 Node 契约版本有关联，但二者不能混为一谈。

| 身份 | 示例 | 含义 |
| --- | --- | --- |
| 项目版本 | `2.8.1-rnl.3` | 本项目源码、二进制、GitHub Release 和精确镜像标签的版本 |
| 契约版本 | `2.8.0` | 当前实现并向 Panel 报告的官方 Node 行为版本 |
| Git tag | `v2.8.1-rnl.3` | 由 Release workflow 创建的不可变发布标签 |
| 镜像标签 | `2.8.1-rnl.3` | 精确容器版本，不带 Git tag 的 `v` 前缀 |

项目版本采用以下两种格式之一：

- `X.Y.Z-rnl.N` 表示 Remnanode Lite 的独立迭代。它既可以继续完善已有契约，
  也可以提前开始下一条项目版本线。仅凭前三段数字，不能声称与对应官方版本兼容。
- `X.Y.Z` 专门用于完成同版本对齐的里程碑。使用这种格式之前，项目版本、契约版本、
  固定的官方源码、代码实现、测试和真实环境表现必须全部一致。

`rnl.N` 在 SemVer 中属于预发布后缀，但本项目不会根据 SemVer 排序推断发布时间。
由 Release workflow 根据 tag 类别决定版本晋升为 `preview` 或 `latest`。

发布预检还会把 stable 版本与已有 stable Git tag 比较，并拒绝更低的版本，避免合法的
版本格式意外让 `latest` 回退。

已经发布的 Git tag 和精确版本镜像标签不可变。`sha-*` 候选标签也按不可变策略管理。
`edge`、`preview` 和 `latest` 是移动通道；其中 `preview` 与 `latest` 严格分离：

| 镜像引用 | 用途 |
| --- | --- |
| `sha-<40 位 main commit>` | 从某个 `main` 提交构建、可复现的候选镜像 |
| `edge` | 最近一个符合条件的 `main` 镜像，适合观察，不适合作为回滚依据 |
| `X.Y.Z` 或 `X.Y.Z-rnl.N` | 精确的已发布版本 |
| `preview` | 最近晋升的 `X.Y.Z-rnl.N` 预览版 |
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
| [`ci`](../../../.github/workflows/ci.yml) | Go、仓库、Native bootstrap、asset lock 和 Linux 网络管理检查 |
| [`container`](../../../.github/workflows/container.yml) | 为 Pull Request 构建镜像，并为每个 `main` 提交构建带证明的 `linux/amd64` 与 `linux/arm64` 候选 |
| [`security`](../../../.github/workflows/security.yml) | 漏洞检查 |
| [`contract-sync`](../../../.github/workflows/contract-sync.yml) | 检测官方 Node 新版本并创建 Issue，不自动修改契约 |
| [`release`](../../../.github/workflows/release.yml) | 校验 tag 和候选镜像、创建 Release 资产、发布 GitHub Release，并晋升正确通道 |

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

使用固定的官方源码运行完整仓库检查：

```bash
export REMNANODE_OFFICIAL_SOURCE=/path/to/pinned/remnawave-node
export REQUIRE_GOVULNCHECK=1
bash scripts/check.sh
```

Release workflow 会重复完整门禁。本地通过可以缩短反馈时间，但不能替代 CI。

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
CANDIDATE_DIGEST="$(docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' "$IMAGE")"
printf '%s\n' "$CANDIDATE_DIGEST" \
  | grep -Eq '^sha256:[0-9a-f]{64}$'
```

记录该 digest，供维护者验收时使用。若打 tag 前 `main` 前进，新的 HEAD 就是新的
候选；不要继续给旧提交打 tag，应重新审查并验证新的候选。

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

如果本次发布改变 Native 交付或生命周期，还要从同一干净提交构建 Native bundle，
并在目标发行版上测试受影响的 systemd 或 OpenRC 路径：

```bash
bash scripts/build-native-bundle.sh dist/native amd64 arm64
```

自动化 workflow 会交叉构建并进行两个架构的结构校验；本地或托管环境的运行测试
必须明确写出实际测试的架构、发行版、service manager 和生命周期操作，不能把单个平台
的结果扩展成所有 Linux 环境的结论。

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

## 6. 运行发布预检并发布

只能发布当前远端 `main` HEAD。手动触发 Release 前，先确认本地工作区与已经验收的
候选一致：

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

需要最后复核时，可以使用已固定的官方源码 checkout 在本地运行发布预检：

```bash
export REMNANODE_OFFICIAL_SOURCE=/path/to/remnawave-node
export REQUIRE_GOVULNCHECK=1

RELEASE_TAG="$TAG" bash scripts/release-check.sh
```

不要在工作站上创建或推送 release tag。应从 GitHub Actions 启动 release workflow，
由 workflow 创建 annotated tag：

```bash
gh workflow run release.yml \
  --repo luxiaba/remnanode-lite \
  --ref main \
  -f operation=publish \
  -f version="$VERSION"
```

当前稳定发布使用 `VERSION=2.8.0` 与 `TAG=v2.8.0`。

手动 workflow 会确认自己运行在 `refs/heads/main`，确认 `GITHUB_SHA` 仍是远端
`main` HEAD，通过 GitHub API 创建 annotated `v<version>` tag，然后在同一轮 release
run 中继续后续发布。Release workflow 不再由 `v*` tag push 触发；从工作站 push tag
不是受支持的发布入口。

仓库设置应优先保护已经发布的结果，而不是阻断 release workflow 自己创建 tag：

- 启用 immutable releases，避免已发布的 GitHub Release 及其关联 tag 被静默修改。
- 为 `v*` 添加 tag ruleset，限制 tag 更新和删除，并阻止 force push。
- 不要启用 tag 创建限制，除非仓库已经有明确的自动化 bypass，可以让 release workflow
  创建 `v<version>` tag。

在 workflow 初始源码身份检查通过前保持 `main` 不变。Workflow 也会在建立 draft、
发布 Release 和推进通道前重新解析远端 annotated tag。如果之后发现源码缺陷，应在
`main` 修复并使用新版本，不要移动已发布 tag。

## 7. Release workflow 校验什么

Publish workflow 首先确认源码与候选身份：

1. 手动发布必须从 `refs/heads/main` 运行，且 workflow 执行初始身份检查时，
   `GITHUB_SHA` 必须仍是经过审查的 `origin/main` HEAD。接受该身份后，后续 `main`
   前进不会使正在进行的发布失效。
2. Workflow 会创建或验证与 `Version` 完全一致的 annotated tag，并在每个外部发布
   边界再次复核这个 tag。
3. 固定的官方源码和完整发布门禁必须通过。
4. 根据 tag 语法把版本明确分类为 stable 或 preview。
5. `sha-<commit>` 候选必须存在，并解析为有效的 manifest digest。
6. 候选 OCI index 必须恰好包含一个可运行的 `linux/amd64` 镜像、一个可运行的
   `linux/arm64` 镜像，以及相应的 attestation。
7. OCI attestation 必须指向 `main` 上的 container workflow 和该 tag 对应的源码提交。
8. Linux 网络管理集成测试必须在隔离 namespace 中通过。
9. 每个外部发布边界都会确认远端 tag 仍是指向同一 commit 的 annotated tag。

Release workflow 不接受 `edge` 作为替代，也不会重新构建容器。精确版本和移动通道
只是给已经接受的候选 digest 增加新名称。

## 8. Release 资产

稳定版和预览版都会构建并上传以下资产：

| 资产 | 用途 |
| --- | --- |
| `install.sh` | 安装精确 Native Release 或本地 bundle 的 POSIX bootstrap |
| `remnanode-lite_<version>_linux_amd64.tar.gz` | amd64 自包含 Native Linux bundle |
| `remnanode-lite_<version>_linux_arm64.tar.gz` | arm64 自包含 Native Linux bundle |
| `compose.yaml` | 仓库中的 Compose 部署文件 |
| `docker-compose.single-file.yaml` | 固定到精确 Release 镜像的单文件 Compose 模板 |
| `remnanode-lite.env.example` | 固定到精确 Release 镜像的环境变量模板 |
| `SHA256SUMS` | 其余所有 Release 资产的校验和 |

每个 Native archive 都是一套完整 generation，包含：

- `remnanode-lite` 和 `rnlctl` 二进制；
- 固定的 rw-core、GeoIP、GeoSite 和生成后的 ASN 数据库；
- systemd 与 OpenRC service 文件；
- `release-manifest.json` 和 `runtime-assets.lock.json`；
- SPDX SBOM、第三方说明、许可证与源码提供说明；
- bundle 内置安装器。

运行时资产已经包含在 bundle 中。Release 不再单独提供 ASN 数据库，也不会在发布时
从未固定的移动来源下载文件。Docker 与 Native bundle 使用同一个
`release/runtime-assets.lock.json`。

`cmd/release-tool` 会根据目标架构、项目版本、契约版本、源码 revision、文件
manifest、asset lock 和内置校验和验证每个 archive。workflow 在上传前还会校验
`SHA256SUMS`。

## 9. Draft、发布与通道晋升

发布顺序是固定的。

### 9.1 为每项资产生成证明

Release staging 目录里的每个文件，包括 `SHA256SUMS`，都会获得与 release workflow
run 和源码提交绑定的 GitHub artifact attestation。workflow 在创建 draft 前立即验证
这些 attestation。

### 9.2 创建并验证 draft

workflow 创建带自动生成 notes 和正确 prerelease 标志的 draft GitHub Release。
如果同一 tag、同一源码提交已有 draft，重跑时可以替换其中的资产；已经公开发布的
Release 绝不能用这种方式替换。

继续之前，workflow 会把 draft 的完整资产列表与本地构建逐项比较。每个文件的名称、
SHA-256 和字节数都必须一致。检查期间，draft 对普通用户不可见。

### 9.3 发布精确容器标签

Draft 完整后，workflow 才把已经接受的候选 digest 晋升为精确版本标签。不可变晋升
工具只会在既有标签解析为同一 digest 时接受它；若指向不同镜像则拒绝覆盖。

### 9.4 发布 GitHub Release

经过验证的 draft 按版本类型公开：

- `X.Y.Z-rnl.N` 成为 GitHub Prerelease，并使用 `make_latest=false`；
- `X.Y.Z` 成为完整 GitHub Release，并使用 `make_latest=true`。

workflow 会复核公开后的状态，尤其确保预览版不能通过 GitHub Latest Release endpoint
解析出来。在尚未有完整稳定版时，GitHub 对该 endpoint 返回 `404`；workflow 只对预览版
接受这一结果，稳定版必须成为 GitHub Latest。

### 9.5 晋升 GHCR 通道

只有 GitHub Release 公开后，workflow 才移动对应通道：

- 预览版：接受的 digest -> `preview`；
- 稳定版：接受的 digest -> `latest`。

通道晋升不会重建或复制平台镜像，只会为完全相同的已接受 digest 发布新的 manifest
引用。

## 10. 验证已发布版本

先确定本次发布的完整身份：

```bash
VERSION=2.8.0
TAG="v${VERSION}"
C="$(git rev-list -n 1 "$TAG")"
CHANNEL=latest
IMAGE=ghcr.io/luxiaba/remnanode-lite
```

预览版使用精确的 `X.Y.Z-rnl.N` 版本号，并将 `CHANNEL` 设置为 `preview`。

检查 GitHub 的公开状态：

```bash
gh api "repos/luxiaba/remnanode-lite/releases/tags/${TAG}" \
  --jq '{tag_name, draft, prerelease, target_commitish}'
gh api repos/luxiaba/remnanode-lite/releases/latest --jq .tag_name
```

Release 不能是 draft；预览版 `prerelease` 必须为 `true`，纯版本必须为 `false`。
GitHub Latest 必须等于新的稳定 tag，不能等于预览 tag。

下载并验证每项 Release 资产：

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

确认候选、精确版本标签和正确通道解析为同一个 manifest digest：

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

最后，根据发布提交验证容器 attestation：

```bash
gh attestation verify "oci://${IMAGE}@${EXACT_DIGEST}" \
  --repo luxiaba/remnanode-lite \
  --cert-identity \
    https://github.com/luxiaba/remnanode-lite/.github/workflows/container.yml@refs/heads/main \
  --source-digest "$C" \
  --deny-self-hosted-runners
```

## 11. 失败与恢复

恢复发布时不要删除或移动精确 Git tag，也不要覆盖精确镜像标签。处理方式取决于
workflow 停在哪一步：

| 失败位置 | 预期外部状态 | 恢复方式 |
| --- | --- | --- |
| Tag 创建、候选查找或预检 | 尚未创建 draft 或 Release；如果 tag 创建已经成功，annotated tag 可能存在 | 确认 CI 与 container workflow 成功后，对同一版本重跑 `operation=publish` |
| 资产构建、attestation 或上传 | 尚无 draft，或存在绑定同一 commit 的 draft | 重跑 workflow；同一 draft 的资产可以替换，随后会再次校验 |
| 精确镜像晋升 | 可能已有已验证 draft；精确镜像可能存在，也可能不存在 | 重跑；只有既有标签 digest 正确时，不可变晋升才会接受它 |
| GitHub 公开发布 | 精确镜像和 draft 可能已经存在 | Release 仍为 draft 时重跑失败 job |
| GHCR 通道晋升 | GitHub Release 与精确镜像已经公开 | 对该已发布 tag 使用 release workflow 的手动通道校正 |
| 公开后发现缺陷 | 精确 Release 保持不变 | 修复 `main`、选择新版本并完成一次新的发布 |

手动 dispatch 也提供已发布 Release 的移动通道校正路径。只有需要修复 GHCR 通道时，
才使用 `operation=reconcile`，不要使用 `operation=publish`：

```bash
gh workflow run release.yml \
  --repo luxiaba/remnanode-lite \
  --ref main \
  -f operation=reconcile \
  -f release_tag=v2.8.0
```

校正流程会确认：

- tag 是 annotated tag，并标识已经公开的 Release；
- GitHub 的 prerelease 与 Latest 状态符合 tag 类型；
- 精确镜像与原始 `sha-<commit>` 候选 digest 相同；
- 候选 attestation 与源码提交一致；
- `rnl.N` 的目标通道是 `preview`，纯版本的目标通道是 `latest`。

随后只修复对应的 GHCR 通道引用。它不会重建资产、替换已经公开的 Release、修改精确
镜像，也不会修复错误的 GitHub Release 元数据。元数据错误应先在 GitHub 中纠正，再
运行校正。

如果失败来自发布源码本身，不要反复重试。应在 `dev` 修复缺陷，合入新的 `main`
候选，提升项目版本，再发布新的身份。

## 12. 发布后的部署回滚

发布身份不可变与部署回滚是两件事。生产部署应记录精确版本或 manifest digest，并保留
上一个版本。需要回滚容器节点时，恢复之前的镜像引用及其配套部署配置，再重新创建服务：

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
```

不要把 `latest` 当作回滚身份。它只是会移动的推荐版本，也不会自动更新正在运行的
容器。大规模部署应分批进行，并始终保留可用的已知良好版本或 digest。日常容器运维
说明见 [`deployment-docker.md`](deployment-docker.md)。

Native 部署使用 `rnlctl rollback` 切回保留的 previous generation：

```bash
sudo rnlctl rollback
```

如果要降级到 previous 之外的版本，应使用经过验证的精确 Release bundle。项目范围的
修正版必须使用新版本并走完整发布流程；不要手动把 `latest` 移回一个未经审查的镜像。

## 13. 跟进官方 Node 版本

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

## 14. 维护者检查清单

推送最终 tag 之前以及发布完成后，确认：

- [ ] 项目版本采用允许的 `X.Y.Z` 或 `X.Y.Z-rnl.N` 格式；
- [ ] 纯版本号与已经实现并固定的契约版本一致；
- [ ] 版本元数据、测试、部署默认值和带日期的 CHANGELOG 一致；
- [ ] 英文规范文档和维护中的翻译均已更新；
- [ ] `dev -> main` Pull Request 已合并，required CI 已通过；
- [ ] `sha-<当前 main commit>` 存在，并包含 `linux/amd64` 和 `linux/arm64` 镜像；
- [ ] 确定的候选镜像已连接目标 Panel、承载真实流量，且没有异常生命周期或资源问题；
- [ ] Native 生命周期变更已在声明支持的发行版和架构上验证；
- [ ] 仓库中没有加入运行测试数据、基础设施标识或 secret；
- [ ] 在干净且指向当前 `main` 的工作区中，`scripts/release-check.sh` 检查通过；
- [ ] `v${VERSION}` 是带注释的 tag，指向当前 `main` HEAD，且从未发布过；
- [ ] Draft 资产的名称、digest 和字节数与构建结果一致；
- [ ] 每项 Release 资产与容器 manifest 都有有效 attestation；
- [ ] 精确镜像标签与接受的候选 digest 相同；
- [ ] 预览版是 GitHub Prerelease，且不改变 GitHub Latest 或 GHCR `latest`；
- [ ] 稳定版是完整 GitHub Release，并推进 GHCR `latest`；
- [ ] 发布后，候选标签、精确版本标签和正确通道（`preview` 或 `latest`）指向同一个
      已证明的 digest，Release 资产也通过 `SHA256SUMS` 校验。
- [ ] 保留上一个精确部署引用，供回滚使用。
