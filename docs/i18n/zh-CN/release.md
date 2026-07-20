<!-- translation: locale=zh-CN; source=docs/release.md; source-sha256=5e345e8952ce822f786de4992b90b44660fa9747521a856dde3f20d80eb791ce -->

# 发布与版本维护手册

> [!IMPORTANT]
> 英文是唯一权威来源；本页是便于阅读的简体中文翻译。请以[英文原文](../../release.md)为准。

[返回文档首页](README.md) · [版本模型](versioning.md)

本文面向项目维护者，定义从日常开发、候选冻结、真实验收，到 GitHub Release 与 GHCR 镜像发布的长期流程。这里描述的是仓库的正式发布规则，不是某个版本的一次性清单。

版本名称和镜像渠道的规范定义见 [`versioning.md`](versioning.md)；本文重点说明如何把这些规则落实为一次可验证的发布。

发布流程有三个核心目标：

1. 每个正式版本都能追溯到受保护 `main` 上的确定提交。
2. 验收记录同时绑定候选提交、二进制摘要和实际测试的容器 manifest digest。
3. `latest` 始终指向最近一次完整发布且已验证 attestation 的候选 digest，历史版本重跑不能让它回退。

## 1. 版本模型

项目版本与官方 Node 契约版本是两个独立概念。

| 名称 | 示例 | 含义 |
| --- | --- | --- |
| 项目版本 | `2.8.1-rnl.9` | 示例 remnanode-lite 构建和 Release 的身份 |
| 契约版本 | `2.8.0` | 当前代码级可证明并默认向 Panel 上报的官方 Node 契约基线 |
| Git tag | `v2.8.1-rnl.9` | 触发正式发布 workflow，发布后按政策不可移动 |
| 镜像 tag | `2.8.1-rnl.9` | 与项目版本完全一致，不带 Git tag 的 `v` 前缀 |

正式版本只允许以下两种格式：

- `X.Y.Z-rnl.N`：本项目的独立迭代版本。`N` 与官方发布次序没有直接关系；项目可以提前开展下一版本的工作，也可以在已经对齐的官方版本上继续改进。
- `X.Y.Z`：官方对齐版本。只有当前契约、固定的官方源码、实现、测试和真实验收都完成 `X.Y.Z` 对齐时才能使用纯版本号。

纯 `X.Y.Z` 发布时，项目版本必须等于契约版本。`X.Y.Z-rnl.N` 的前三段不构成官方兼容声明，实际兼容范围始终以契约版本和 Release note 中的兼容矩阵为准。

例如，以下演进都是合法的：

```text
项目版本          契约版本          说明
2.8.1-rnl.1      2.8.0            提前开展 2.8.1 项目线，仍按 2.8.0 契约报告
2.8.1            2.8.1            完成官方 2.8.1 对齐后的纯版本
2.8.1-rnl.9      2.8.1            在已对齐版本上继续进行项目改进
2.8.2-rnl.1      2.8.1            提前进入下一条项目开发线
```

`-rnl.N` 在 SemVer 语法中属于预发布后缀，因此 SemVer 会认为 `2.8.1-rnl.9` 小于 `2.8.1`。本项目不使用 SemVer 排序决定发布时间或 `latest` 指向；稳定性由完整发布门禁决定，先后顺序由实际发布记录决定。

### 1.1 标签不变性

- 已推送的正式 Git tag `v${VERSION}` 不得移动、覆盖或复用。
- 已发布的 GHCR 精确版本 tag 和 `sha-*` tag 不得主动覆盖。
- `edge` 与 `latest` 是明确的可变别名，不能作为回滚依据。
- 未完成正式验收的构建只能使用 `edge`、`sha-*` 或 `candidate-sha-*`，不能创建 `v*` 正式 tag。
- `latest` 属于本项目自己的稳定镜像通道，不代表 `remnawave/node` 官方镜像的 latest。

## 2. 分支与自动化边界

仓库长期维护两个分支：

| 分支 | 职责 | 常规进入方式 |
| --- | --- | --- |
| `dev` | 日常开发、集成和回归 | 从短期主题分支发起 PR，经 CI 后合入 |
| `main` | 发布候选与正式发布来源 | 从 `dev` 发起 PR，经 CI 后合入 |

GitHub Actions 的职责彼此独立：

| Workflow | 触发条件 | 产物或作用 |
| --- | --- | --- |
| `ci` | `dev`/`main` push 与相关 PR | Go、仓库、installer 和 Linux 网络管理门禁，由 `ci / gate` 汇总 |
| `container` | 容器输入发生变化的 `dev`/`main` push 或 PR | `dev`/PR 只构建；`main` 先按 digest 构建和证明，再发布 `sha-<commit>` 与 `edge` |
| `security` | 定时或手动 | 扫描可达 Go 漏洞 |
| `contract-sync` | 定时或手动 | 检查固定官方契约，发现官方新版本时创建 Issue，不自动改代码 |
| `release` | push `v*` tag | 最终门禁、候选 attestation 验证、GitHub Release、精确 GHCR 标签和 `latest` 晋升 |

`ci` 与 `container` 不是重复流程：前者验证代码和仓库，后者验证或发布容器。分支保护应始终要求不会因路径过滤而缺失的 `ci / gate`；按路径触发的 `container` 不适合作为唯一 required check。

## 3. 日常开发

所有功能、修复、依赖、workflow、部署文件和长期文档变更先通过主题分支进入 `dev`：

```bash
git switch dev
git pull --ff-only origin dev
git switch -c chore/prepare-next-release

# 修改并运行与风险匹配的检查
bash scripts/check-go.sh

git status --short
git diff --check
git add <明确的文件列表>
git diff --cached --check
git commit -m "type(scope): describe the change"
git push -u origin chore/prepare-next-release

# 在 GitHub 发起 chore/prepare-next-release -> dev 的 PR；CI 成功并评审后合入
```

即使只有一位维护者，也使用同一条 PR 路径，让 `dev` 的变更、CI 结论和评审上下文可追溯；不要把“允许 push”理解为常规开发入口。

发布前必须在 `dev` 完成版本元数据更新。设本次项目版本为：

```bash
VERSION=X.Y.Z-rnl.N   # 或已经完成官方对齐的 X.Y.Z
TAG="v${VERSION}"
```

版本更新必须同步应用版本、安装/升级脚本默认 tag、容器默认镜像、contract probe 身份、CHANGELOG 和相关测试。若只是进入新的 `rnl` 项目线，不得顺手修改契约版本或官方源码 pin。

官方契约升级是单独的兼容性任务，至少包括：

- 固定新的官方 Node tag 与完整 commit。
- 更新契约版本和 Panel 上报版本。
- 重新审计路由、schema、错误和副作用。
- 调整实现与自动化测试。
- 更新兼容文档、真实 Panel 目标和验收范围。

## 4. 将代码候选合入 main

完成 `dev` 回归后，发起 `dev -> main` PR。代码 PR 可以使用仓库允许的正常合并方式；候选提交 `C` 必须定义为 PR 已经合入后 `main` 上的最终提交，而不是合入前的 `dev` 提交。

```bash
git fetch origin dev main
git switch main
git pull --ff-only origin main

C="$(git rev-parse HEAD)"
git rev-parse "${C}^{commit}"
git rev-parse "${C}^{tree}"
```

从此刻开始冻结 `main`。冻结范围包括 Go 代码、测试、脚本、workflow、Dockerfile、Compose、部署服务文件和非发布专用文档。验收期间如果 `main` 出现任何超出最终化白名单的变化，原候选证据失效，必须以新的 `main` 提交作为 `C` 并重新执行相关验收。

不要提前创建正式 `v${VERSION}` tag。候选身份使用完整的 40 位 commit 即可；如确实需要本地标记，可以创建包含 commit 短 SHA 的本地候选 tag，但不得将它当作 Release tag。

## 5. 候选镜像验收

若候选提交包含容器输入变化，`main` 的 `container` workflow 会先构建没有业务 tag 的多架构 manifest，生成 BuildKit SBOM/provenance 和 GitHub build attestation，最后才发布：

```text
ghcr.io/luxiaba/remnanode-lite:edge
ghcr.io/luxiaba/remnanode-lite:sha-${C}
```

`sha-${C}` 第一次写入后由 workflow 拒绝移动；`edge` 只有在 `C` 仍是当前 `main` HEAD 时才会更新。先用 `sha-${C}` 定位自动候选，再把 registry 返回的 manifest digest 作为验收的规范镜像身份；从同一个 `C` 下载 Compose 或部署文件，避免镜像和配置来自不同提交。

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
  --signer-workflow luxiaba/remnanode-lite/.github/workflows/container.yml \
  --source-digest "$C" \
  --source-ref refs/heads/main \
  --deny-self-hosted-runners
```

手动从 `main` 运行候选 workflow 时只发布按策略不移动的 `candidate-sha-${C}`，不会覆盖自动 push 已生成的 `sha-${C}`。它是手动候选专用的发现别名，digest 不保证与自动候选相同。两种候选都可以进入验收，但从开始验收到正式发布必须固定同一个完整 commit 和 manifest digest；正式 Release 直接验证该 digest 的存在性及其 attestation，不依赖候选使用了哪一种 tag 别名。

M8 的容器验收使用 `C` 中的 `deploy/compose.single-file.yaml`，将其中镜像引用替换为 `ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}` 后运行。证据必须同时保存候选 Git object 中该模板的文件 SHA-256 和实际运行的 digest；仅运行 `docker compose config` 或测试相同 tag 的另一次构建不能替代最终候选验收。完整字段和采集口径见 [`development/release-acceptance.md`](development/release-acceptance.md#docker-compose-证据)。

## 6. 冻结候选与真实验收

正式验收必须针对同一个 `C`，并使用 `scripts/build-release-binaries.sh` 从干净工作树构建二进制。官方 Git repository 必须包含当前契约基线固定的 commit；source oracle 只读取该 commit object，不信任其 checkout、index 或 HEAD：

```bash
export REMNANODE_OFFICIAL_SOURCE=/path/to/pinned-remnawave-node
export REQUIRE_GOVULNCHECK=1

bash scripts/check.sh
git status --short
```

真实验收范围以 [`development/release-acceptance.md`](development/release-acceptance.md) 为准，至少覆盖：

- 固定官方 Node 与候选 Node 的路由、响应、错误和副作用差分。
- 目标 Panel 的节点注册、Xray 生命周期、统计、用户和插件流程。
- Ubuntu/systemd 与 Alpine/OpenRC，架构并集覆盖 amd64 和 arm64。
- 候选 manifest 同时包含 linux/amd64、linux/arm64，并分别在原生 amd64 与 arm64 验收机上用批量部署 Compose 模板启动同一个验收 digest。
- 容器实际 cgroup、read-only rootfs、init/reaper、tmpfs、capability、health、优雅停止、PIDs/zombie 和日志轮转约束。
- rw-core、nftables、socket destroy、进程组清理、安装、升级、回滚和卸载隔离。
- 整机 `512 MiB RAM / 1 vCPU / 2 GiB disk / no swap` 的资源预算；容器磁盘峰值必须保留一个真实回滚镜像。
- 50k 用户、规定的持续运行时间和故障恢复场景。

验收材料写入当前项目版本对应的目录：

```text
docs/development/acceptance/v${VERSION}/
  manifest.json
  systemd.json
  openrc.json
  panel.json
  compose.json
  resource-fault.json
```

当前 `cmd/release-evidence-check` 是首个版本线的严格验收 profile，会固定版本、官方提交、Panel、rw-core、系统、路由数量和资源策略。准备新的项目版本或契约时，必须先在普通代码 PR 中更新并测试这个 profile；不能等到 tag workflow 中临时放宽校验，也不能把旧版本 evidence 改名复用。

`manifest.json` 必须记录 `C`、candidate tree、`CANDIDATE_DIGEST`、项目版本、Git tag、官方契约 pin、Panel、rw-core、资源策略、风险和其余 evidence 的 SHA-256。它不记录尚未产生的最终提交 `F`；严格 schema 会拒绝这个未知字段。不得提交 Secret Key、JWT、CA、证书、私钥、IP、hostname、Panel URL、原始响应 body 或可还原用户的数据。

## 7. 受保护 main 下提交发布资料

候选验收通过后，只允许提交发布最终化资料。当前白名单为：

```text
README.md
README.zh-CN.md
README.ru.md
CHANGELOG.md
docs/development/roadmap.md
docs/i18n/zh-CN/development/roadmap.md
docs/development/acceptance/v${VERSION}/manifest.json
docs/development/acceptance/v${VERSION}/systemd.json
docs/development/acceptance/v${VERSION}/openrc.json
docs/development/acceptance/v${VERSION}/panel.json
docs/development/acceptance/v${VERSION}/compose.json
docs/development/acceptance/v${VERSION}/resource-fault.json
docs/releases/v${VERSION}.md
```

验收前必须完成其他长期文档；`C` 之后修改架构、配置、部署或本发布手册都会使候选失效。

从 `C` 创建专用文档分支：

```bash
git switch --detach "$C"
git switch -c "release/v${VERSION}-docs"

# 填写 evidence、Release note、CHANGELOG，并同步 canonical README/roadmap
# 及其持续维护的译文
git add README.md README.zh-CN.md README.ru.md CHANGELOG.md \
  docs/development/roadmap.md docs/i18n/zh-CN/development/roadmap.md \
  "docs/development/acceptance/v${VERSION}" \
  "docs/releases/v${VERSION}.md"
git diff --cached --check
git commit -m "docs(release): record v${VERSION} acceptance"
git push -u origin "release/v${VERSION}-docs"
```

然后发起该分支到 `main` 的 PR。这里有一个不可省略的约束：**发布资料 PR 必须使用 squash merge**。

验收验证器会逐个检查 `C..HEAD` 的提交，并拒绝：

- 任意双 parent 或多 parent 的 merge commit。
- 白名单以外的路径变化。
- 修改后再 revert 的代码漂移。
- evidence 与 HEAD、Git index 或工作树不一致。

因此不能使用普通 merge commit 完成发布资料 PR。合并时 `main` 仍必须停在 `C`；若其他提交已经进入 `main`，不要 rebase 后继续沿用旧 evidence，应重新评估变更并冻结新的候选。

Squash merge 后，最终发布提交记为 `F`：

```bash
git fetch origin main
git switch main
git pull --ff-only origin main

F="$(git rev-parse HEAD)"
git merge-base --is-ancestor "$C" "$F"
git diff --name-only "$C..$F"
```

正式 tag 指向 `F`，不是候选代码提交 `C`。验证器会证明 `F` 只比 `C` 多出允许的发布资料。

## 8. Release note 要求

每个正式版本必须提供：

```text
docs/releases/v${VERSION}.md
```

首行必须是：

```markdown
# v${VERSION}
```

Release note 至少包含以下章节：

```markdown
## 兼容范围
## 验收结果
## 已知风险
## 安装与升级
```

兼容范围必须分别写明项目版本、契约版本、固定官方 commit、目标 Panel、rw-core 和支持架构；验收结果还必须列出候选提交 `C` 与 `candidateImageDigest`，并包含门禁要求的精确相对链接：

```markdown
[验收 manifest](../development/acceptance/v${VERSION}/manifest.json)
```

Release note 不能写入自身所在提交 `F`，否则会形成不可实现的 Git SHA 自引用。发布完成后，正式 tag 指向的 commit 就是 `F`，可用 `git rev-list -n 1 v${VERSION}` 或 GitHub Release 的 target commit 解析。已知风险不能用空泛的“无”替代审计结论；不存在开放风险时也应说明验收边界和未覆盖范围。文件不得包含 `TODO`、`TBD`、`待补`、`Unreleased` 或“开发中”等占位内容。

## 9. 最终门禁与 tag

在最新 `main` 的干净工作树上执行最终检查：

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

确认版本、证据和最终提交无误后创建 annotated tag：

```bash
git tag -a "$TAG" -m "release ${TAG}"

RELEASE_TAG="$TAG" \
REMNANODE_OFFICIAL_SOURCE="$REMNANODE_OFFICIAL_SOURCE" \
REQUIRE_GOVULNCHECK=1 \
REQUIRE_TAG_AT_HEAD=1 \
  bash scripts/release-check.sh

git push origin "$TAG"
```

如果 tag push 后因非瞬时问题失败，不得 force-move 原 tag。修复需要源码变化时使用新的项目版本并重新走候选流程。

从冻结 `C` 开始直到 `latest` 晋升和发布后验证完成，`main` 应保持冻结。若正式 workflow 运行期间 `main` 已经前进，精确版本仍可保留为可审计 Release，但 promotion 会拒绝把它设为 `latest`；需要推荐新的主线状态时，应从新的 `main` HEAD 准备后续版本，不能绕过 HEAD guard。

## 10. Tag Release 自动化

`.github/workflows/release.yml` 收到 `v${VERSION}` 后按以下顺序执行：

1. 验证 tag commit 正是当前 `origin/main` HEAD，并重新运行版本、证据、代码、供应链和 Linux namespace 门禁。
2. 从 acceptance manifest 读取 `C` 和已验收 digest，直接确认该 digest 仍存在，并严格校验 attestation 的仓库、签名 workflow、源码提交与 `refs/heads/main` 来源；不要求它必须来自某一种候选 tag 别名。
3. 构建 linux/amd64 与 linux/arm64 二进制归档、compact ASN 数据库、标准 Compose、单文件 Compose、环境模板和 `SHA256SUMS`；证据验证器会比较两种架构的 Node 二进制摘要。
4. 使用 `docs/releases/v${VERSION}.md` 创建 GitHub Release，但暂不把它标为 GitHub 的 Latest Release；已有同名资产不会被覆盖。
5. 不重新构建容器，而是把已经验收并证明的 `CANDIDATE_DIGEST` 发布为按政策不移动的精确版本：

   ```text
   ghcr.io/luxiaba/remnanode-lite:${VERSION}
   ```

6. 只有精确版本发布成功，且 tag commit 仍是当前 `origin/main` HEAD 时，才把同一个已证明 digest 晋升为 GHCR `latest`，并将对应 GitHub Release 标记为 Latest Release：

   ```text
   ghcr.io/luxiaba/remnanode-lite:latest
   ```

正式镜像的构建 provenance 和 OCI revision 指向候选代码提交 `C`；Git tag 与 GitHub Release 指向只比 `C` 多发布资料的最终提交 `F`。acceptance manifest 和 Release note 记录 `C` 与 digest，Git tag 本身唯一解析出 `F`；commit-bound 文件不能自引用尚未产生的 `F`。不能把精确版本描述为重新从 `F` 构建的另一份容器。

精确版本写入采用“不存在则创建、存在则必须是同一 digest”的规则，完整 workflow 重跑不能把它改向另一份内容。`latest` 晋升只移动浮动 tag，不重新构建镜像。promotion job 必须独立重新读取 `origin/main` 并检查 HEAD，不能只依赖更早 job 曾经完成的检查。任何历史 tag 都不得更新 GHCR `latest` 或 GitHub Latest Release。release workflow 使用仓库级串行组，避免两个正式发布同时竞争 registry 写入。

纯 `X.Y.Z` 和 `X.Y.Z-rnl.N` 在通过同一套正式门禁后都属于稳定 Release，也都会被自动晋升为 `latest`。实验性构建不要通过降低 GitHub Release 的 prerelease 标记来发布，应继续使用候选镜像通道。

## 11. 发布后验证

设候选提交为 `C`、最终资料提交为 `F`：

```bash
VERSION=X.Y.Z-rnl.N   # 或 X.Y.Z
C=REPLACE_WITH_40_CHAR_CANDIDATE_COMMIT
F="$(git rev-list -n 1 "v${VERSION}")"
CANDIDATE_DIGEST=sha256:REPLACE_WITH_64_HEX_DIGEST
IMAGE="ghcr.io/luxiaba/remnanode-lite:${VERSION}"
CANDIDATE_IMAGE="ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}"
LATEST_IMAGE="ghcr.io/luxiaba/remnanode-lite:latest"
```

确认 multi-arch manifest：

```bash
docker buildx imagetools inspect "$IMAGE"
docker buildx imagetools inspect "$CANDIDATE_IMAGE"
```

输出必须包含 `linux/amd64` 和 `linux/arm64`，且精确版本与 acceptance manifest 中的候选引用解析为同一 manifest digest。标准自动候选的 `sha-${C}` 或实际使用过的手动候选 tag 如仍保留，也应解析为该 digest。

验证 GitHub attestation：

```bash
gh attestation verify \
  "oci://ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}" \
  --repo luxiaba/remnanode-lite \
  --signer-workflow luxiaba/remnanode-lite/.github/workflows/container.yml \
  --source-digest "$C" \
  --source-ref refs/heads/main \
  --deny-self-hosted-runners
```

确认本次发布被晋升为稳定版本时，再比较 `latest`：

```bash
docker buildx imagetools inspect "$LATEST_IMAGE"
```

`latest`、精确版本和 acceptance 中的候选 digest 应指向同一 manifest digest。若 tag commit `F` 在发布期间已不再是 `main` HEAD，`latest` 保持上一稳定版本是预期行为。

验证 GitHub Release 资产：

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

`SHA256SUMS` 验证下载内容与 workflow 生成内容一致；容器的 GitHub attestation 则提供独立的构建来源证明。除非 Release workflow 另行增加文件级 artifact attestation，不应把容器 attestation 描述为二进制归档的证明。

## 12. 部分失败与恢复

发布 workflow 是分阶段产生外部状态的。失败后先确认已经创建了哪些对象，再决定重试范围。

| 失败位置 | 已有状态 | 恢复方式 | `latest` 状态 |
| --- | --- | --- | --- |
| 门禁、候选 digest 或 attestation 校验失败 | 没有新 Release 和正式镜像 | 修正 evidence 或候选状态；源码变化则创建新候选和版本 | 不变 |
| GitHub Release 成功、精确版本发布失败 | Release 资产可能已存在 | 只重跑失败 job；已有 Release 资产和目标 tag 均不得覆盖 | 不变 |
| 精确版本成功、GHCR `latest` 晋升失败 | 精确版本、候选证明和 Release 资产完整 | 只重跑 promotion job，按同一已验收 digest 晋升 | 不变，直到晋升成功 |
| GHCR `latest` 已晋升、GitHub Latest 标记失败 | GHCR 稳定通道已更新，GitHub Release 尚未标记 | 重跑 promotion job；同 digest 的 GHCR 操作是幂等的，随后补写 GitHub 标记 | GHCR 已更新，GitHub UI 暂时落后 |
| 历史 run 重试未完成 job | 历史 Release/镜像可能已经部分存在 | 只修复该精确版本；promotion 必须再次检查当前 main HEAD | 不得回移 |

优先使用 GitHub Actions 的“Re-run failed jobs”。完整重跑会保留已有 Release 资产，并只允许精确版本继续指向相同 digest；若已有状态与 acceptance manifest 不一致，workflow 会失败，不能通过删除或覆盖证据掩盖冲突。先记录 GitHub Release、GHCR manifest、attestation 和 workflow run 的对应关系，再决定是否需要新的项目版本。

如果 `latest` 因 workflow 缺陷或人工操作指向错误 digest，应按发布事故处理：记录错误和上一稳定 digest，用受控 promotion 恢复 `latest`，然后修复 workflow。不要修改任何已发布精确版本 tag 来掩盖问题。

## 13. 回滚

服务端回滚使用上一稳定版本的精确 tag 或 manifest digest：

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
```

同时恢复与该版本配套的 Compose 和配置。不要通过移动旧版本 tag 或把 `latest` 强行改成任意历史构建来代替节点级回滚。

`latest` 不会自动替换运行中的容器。选择自动跟随稳定通道的节点仍需主动执行 `docker compose pull` 与 recreate；大规模部署应分批更新，并保留上一个精确版本或 digest 作为回退点。完整容器操作见 [`deployment-docker.md`](deployment-docker.md)。

原生 systemd/OpenRC 部署回退只能使用本项目确实发布过的 tag：

```bash
sudo RNL_TAG=vX.Y.Z-rnl.N bash upgrade.sh --yes
```

升级器会验证 Release 摘要和二进制版本，并按自身事务规则恢复 binary、service、support、配置和运行状态。

## 14. 官方版本同步

`contract-sync` 定时检查官方 `remnawave/node` 最新 Release。检测到变化时只创建同步 Issue，不自动修改契约、项目版本、代码、tag 或镜像。

处理官方新版本时：

1. 记录官方 tag 和完整 commit。
2. 审计路由、schema、错误、插件和运行行为差异。
3. 更新契约证据、实现和测试。
4. 选择合适的项目版本；它可以是新的 `rnl` 版本，不要求立即发布纯官方版本号。
5. 完成真实验收后，才将契约版本更新为已验证基线。
6. 只有完成同版本官方对齐时，才允许发布纯 `X.Y.Z`。

项目版本的演进由本项目维护计划决定，官方 Release 只改变兼容工作输入，不能自动决定下一个 `rnl.N`。

## 15. 最终检查清单

推送正式 tag 前逐项确认：

- [ ] `VERSION` 使用允许的纯版本或 `rnl` 格式。
- [ ] 纯版本与契约版本相同；`rnl` 版本的真实契约已在文档中明确。
- [ ] 版本元数据、脚本、Compose、probe、CHANGELOG 和测试一致。
- [ ] 代码 PR 已进入受保护 `main`，候选 `C` 是合入后的 main commit。
- [ ] `C` 的 `ci` 与候选容器 workflow 成功。
- [ ] 所有 evidence 绑定同一个 `C`、candidate tree 和实际验收的多架构 manifest digest。
- [ ] 目标 Panel、两种 init、两种架构、资源与故障验收通过。
- [ ] `compose.json` 绑定 `C` 中的单文件 Compose 和同一 digest；amd64/arm64 两条实测 run 都满足整机资源、容器限制、隔离、init/reaper、健康、停止、PIDs/zombie、日志、磁盘余量及实际回滚启动门禁。
- [ ] 发布资料 PR 只修改最终化白名单，并 squash 为恰好一个单 parent 提交。
- [ ] README 已移除首发前提示，roadmap 已把本版本 M8 状态更新为完成。
- [ ] 最终提交 `F` 是当前 `origin/main` HEAD。
- [ ] `scripts/release-check.sh` 在干净工作树通过。
- [ ] `v${VERSION}` 是指向 `F` 的 annotated tag，且从未发布过。
- [ ] tag push 后 GitHub Release、精确镜像、候选 attestation 验证和 `latest` promotion 全部成功。
- [ ] 精确版本与 `latest` 都等于 acceptance digest，实际使用的候选别名仍解析为该 digest，amd64/arm64 均存在。
- [ ] 生产更新记录了精确版本或 digest，并保留可执行的回滚目标。
