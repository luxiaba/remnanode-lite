<!-- translation: locale=zh-CN; source=docs/release.md; source-sha256=93c964fdc96c132a3d026be0dce21f67ed5b46756a5b77ee79a9798860b4bb59 -->

# 发布 Remnanode Lite

> 本页是中文译文；发布规则以[英文原文](../../release.md)为准。

[文档首页](README.md) | [版本与镜像标签](versioning.md)

Remnanode Lite 使用“一次构建、后续晋升”的发布方式。合并到 `main` 后，CI 会先产出容器镜像和 Native Linux 资产；发布 workflow 只校验并发布这些既有候选，不会重新构建。

GitHub Release 采用先 draft、后公开的方式。创建 draft 本身不会创建 Git tag；公开已验证的 draft 时，GitHub 才会在已接受的 `main` 提交上创建 `v<version>` tag，并通过 Release immutability 锁定 tag 和资产、生成 Release attestation。

```text
dev -> pull request -> main
                         |
                  CI + candidate workflow
                         |
          +--------------+----------------+
          |                               |
  sha-<commit> image              Native release assets
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

## 发布类别

版本号本身决定发布类别，不使用额外的稳定性开关。

| 版本 | GitHub Release | 精确 GHCR 标签 | 移动通道 | GitHub Latest |
| --- | --- | --- | --- | --- |
| `X.Y.Z` | 稳定版 | `X.Y.Z` | `latest` | 是 |
| `X.Y.Z-rnl.N` | 预发布版 | `X.Y.Z-rnl.N` | `preview` | 否 |

`rnl.N` 是 Remnanode Lite 自己的修订号，与官方 Node 版本没有直接对应关系。它既可以用于提前开发下一条版本线，也可以用于完善已实现的官方契约。项目版本与 `ContractVersion` 的关系见[版本策略](versioning.md)。

## 仓库设置

发布保证同时依赖仓库代码和 GitHub 设置。请保持以下配置：

- `main` 是受保护的发布分支，日常改动经由 `dev` 的 pull request 合并。
- 默认 `GITHUB_TOKEN` 保持只读，每个 job 只申请所需的最小权限。
- Actions 使用完整 commit SHA 固定版本，Dependabot 在 `dev` 上维护这些更新。
- `release` environment 只允许从 `main` 部署。
- 在 **Settings -> General -> Releases** 启用 **Release immutability**。
- tag ruleset 必须允许 Releases API 在公开 draft 时创建 `v*` tag；公开后由 Release immutability 保护 tag。

不要在工作站上手工创建或推送发布 tag。唯一受支持的发布入口是 release workflow。对于尚未公开的版本，如果已经存在 `v*` tag，workflow 会直接失败，不会接管它。

## 1. 准备版本

在 `dev` 完成代码、测试、部署文件、文档和变更日志。完成运行验收后不应再补一个“只改文档”的提交，因为每个新提交都会生成不同的候选。

至少检查以下项目：

- `internal/version/version.go` 是准备发布的项目版本。
- `internal/version/contract.version` 反映代码真正实现的契约。
- 稳定版 `X.Y.Z` 必须与契约版本相同；预发布版可保留较早但已经验证的契约。
- `CHANGELOG.md` 包含 `## [VERSION] - YYYY-MM-DD` 格式的日期标题。
- Compose 默认值和 Native 文档使用同一个版本。
- 运行时资产变更固定在 `release/runtime-assets.lock.json`。

当本地有已固定的官方源码时，可执行完整本地门禁：

```bash
export REMNANODE_OFFICIAL_SOURCE=/path/to/pinned/remnawave-node
export REQUIRE_GOVULNCHECK=1
bash scripts/check.sh
```

本地检查用于缩短反馈周期；GitHub CI 才是发布的正式记录。

## 2. 合并并等待候选

合并经过审查的 `dev -> main` pull request。该 `main` 提交必须完成两个 workflow：

- `ci` 运行 Go、仓库、Native bootstrap 和 Linux 网络测试。
- `candidate` 构建并证明多架构容器镜像，构建和校验两个 Native bundle，并将完整发布资产作为 workflow artifact 保存。

候选镜像格式如下：

```text
ghcr.io/luxiaba/remnanode-lite:sha-<完整 40 位 main commit>
```

`edge` 可能暂时指向同一镜像，但它是移动的观察通道，不能作为发布证据。Native 候选 artifact 保留 30 天；如已过期，可在 `main` 重新运行 candidate workflow。重跑会验证并复用已有的 `sha-<commit>` 镜像，不会重新构建它，然后用相同源码和锁定输入重新生成 Native bundle。

## 3. 维护者验收

部署精确的 `sha-<commit>` 镜像或其 manifest digest，并确认和本次发布相关的行为：

- 在维护的资源限制下正常启动并变为 healthy；
- 能连接目标 Panel，且项目与契约版本正确；
- rw-core 真实承载代理流量；
- 用户、插件、统计和生命周期操作符合本次变更；
- 没有非预期重启、OOM 或关闭异常。

若 Native 交付有改动，还应在受影响的 systemd 或 OpenRC 平台测试候选 bundle。请明确实际验证过的架构和发行版；一台机器不能代表全部 Linux 目标。

验收记录属于运维数据。不要把主机清单、地址、Panel 信息、Secret、日志、容器标识或 smoke 输出提交到仓库。

## 4. 运行 Release Workflow

打开 **Actions -> release -> Run workflow**，选择 `main`，填写精确的源码版本。例如：

```text
version: 2.8.0
```

等价的 GitHub CLI 命令为：

```bash
gh workflow run release.yml \
  --repo luxiaba/remnanode-lite \
  --ref main \
  -f version=2.8.0
```

workflow 会依次完成：

1. 确认输入版本与源码一致，且发起时的提交仍是远端 `main` HEAD。
2. 找到该提交成功完成的 `ci` 和 `candidate` workflow。
3. 下载既有 Native 资产，并校验文件集合、校验和、bundle manifest、SBOM、源码提交和 attestation。
4. 解析 `sha-<commit>`，校验两个可运行镜像 manifest、各自 attestation manifest 与 GitHub provenance。
5. 创建或更新 draft GitHub Release，此时不创建 Git tag。
6. 将每个 draft 资产的名称、digest 和大小与本地资产比对，并要求尚未公开的 `v<version>` tag 仍不存在。
7. 将已接受镜像 digest 晋升为不可变的精确版本标签，不重新构建镜像。
8. 再次确认已接受提交仍是远端 `main` HEAD，且 draft 校验期间没有出现 `v<version>` tag。
9. 以正确的稳定版或预发布状态公开 draft；`v<version>` tag 在这一步创建。
10. 要求 GitHub Release 为 immutable，并验证 tag 指向、Release attestation 和每个资产。
11. 稳定版把同一 digest 推进到 `latest`，预发布版则推进到 `preview`。

只有发布 Release 或 registry 标签的 job 拥有写权限；候选验证保持只读。

## 5. 核验发布结果

Release 页面应显示 **Immutable**。也可以在新版 GitHub CLI 中验证：

```bash
VERSION="<published-version>"
gh release verify "v${VERSION}" --repo luxiaba/remnanode-lite
```

稳定版应得到以下引用：

```text
Git tag:       vX.Y.Z
GitHub Release vX.Y.Z
GHCR exact:    ghcr.io/luxiaba/remnanode-lite:X.Y.Z
GHCR channel:  ghcr.io/luxiaba/remnanode-lite:latest
```

预发布版将版本替换为 `X.Y.Z-rnl.N`，移动通道应是 `preview`，GitHub 不应将它标记为 Latest。

可比较精确标签和移动标签是否指向同一个 manifest：

```bash
docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' \
  ghcr.io/luxiaba/remnanode-lite:X.Y.Z

docker buildx imagetools inspect \
  --format '{{.Manifest.Digest}}' \
  ghcr.io/luxiaba/remnanode-lite:latest
```

## 失败与重试

流程刻意把不可逆的发布动作放在最后。

| 失败位置 | 外部状态 | 正确处理 |
| --- | --- | --- |
| 源码、CI、候选、包或 provenance 校验 | 尚未创建 Release | 修复原因或等待必要 workflow 成功，再运行 release |
| 创建 draft 或上传资产 | 可能已有 draft；release tag 尚不存在 | 仅当 `main` 仍指向已接受提交时才用同一版本重试；workflow 会更新并重新校验 draft |
| 精确镜像标签晋升 | 已有已验证 draft；精确标签可能已经正确 | 重试；不可变晋升只接受 digest 一致的已有标签 |
| 公开或公开后校验 | Release 和 tag 可能已经公开 | 用同一版本重跑一次；workflow 会识别已公开的 immutable Release，跳过修改步骤并重新验证结果。不要删除或重写它 |
| `latest` 或 `preview` 晋升 | immutable Release 和精确镜像均有效 | 对该发布 tag 运行 `reconcile-channel` |

`reconcile-channel` 只接受已经公开的 immutable Release。它不会把 `latest` 移离 GitHub 当前的稳定 Latest Release，也不会把 `preview` 移到较旧的预发布版。

draft 与已接受提交绑定。如果 draft 尚未公开而 `main` 已前进，workflow 会有意拒绝继续执行。只删除该未公开 draft，验收新的 `main` 候选后再重新发起发布。已经公开的 Release 和 tag 绝不能删除、改指向或重新创建。

若公开后的版本有缺陷，修复源码并发布新版本；不要复用 tag 或替换资产。

## 回滚

保留部署记录中的上一个精确镜像标签或 digest。Docker 回滚是显式的 Compose 更新：

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

Native 安装保留一个已验证的上一代，可使用 `rnlctl rollback`。不要把 `latest` 或 `preview` 倒退当作回滚手段。

## 发布检查清单

- [ ] 版本、契约、变更日志、部署示例和文档一致。
- [ ] `dev -> main` pull request 已通过必需检查并完成审查。
- [ ] 当前 `main` 提交的 `ci` 和 `candidate` 均成功。
- [ ] 精确 `sha-<commit>` 镜像已通过真实 Panel 与流量验收。
- [ ] Native 交付有改动时已完成相应测试。
- [ ] 从 `main` 以精确源码版本发起了 release workflow。
- [ ] 已发布 Release 显示 **Immutable**，且 `gh release verify` 成功。
- [ ] 精确镜像标签指向已接受的候选 digest。
- [ ] 稳定版只推进 `latest`，预发布版只推进 `preview`。
- [ ] 仍保留此前的精确部署引用，便于回滚。
