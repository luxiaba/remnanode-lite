<!-- translation: locale=zh-CN; source=docs/versioning.md; source-sha256=d4b26b248b395c36314c449fd2e4757dcfe4549af2a31c96fb8994e387b1eb88 -->

# 版本与镜像标签

> 本页是中文译文；版本和发布规则以[英文原文](../../versioning.md)为准。

[英文原文](../../versioning.md) · [文档首页](README.md) · [发布流程](release.md)

Remnanode Lite 把项目身份和兼容性声明分开管理。项目可以独立演进，同时继续实现一个已经验证的官方 Node 契约。精确版本和移动镜像通道也承担不同职责：精确版本标识一个发布，`preview` 与 `latest` 选择的是发布类别。

release workflow 是发布行为的可执行真相源。它调用 `release-tool metadata`，从源码版本推导稳定版或预发布版分类。

## 两个版本维度

| 维度 | 真相源 | 含义 |
| --- | --- | --- |
| 项目 `Version` | `internal/version/version.go` | 本项目源码、二进制、Release 资产和精确容器标签的身份 |
| 官方 `ContractVersion` | `internal/version/contract.version`、`internal/version/version.go` 与固定契约证据 | 实际实现并向 Panel 报告的官方 Node 行为 |
| Panel 集成目标 | 维护者发布验收 | 实际集成测试使用的 Panel 版本，不编译进发布身份 |
| rw-core 与运行时资产 | `release/runtime-assets.lock.json` | Docker 与 Native Linux 共用的 core、GeoIP、GeoSite、ASN、源码、许可证和校验和输入 |

例如：

```text
Version:         2.8.0
ContractVersion: 2.8.0
```

这表示与官方 Node `2.8.0` 契约对齐的稳定发布线。对应 GitHub Release 公开后，才会附带 Native Linux 发行包。未来的 `rnl.N` 后缀属于本项目，不是官方发布的修订号。仅改变 `Version` 不会扩大项目声明的兼容范围。

改变 `ContractVersion` 前，必须固定官方源码、审查契约差异、更新实现和测试，并完成兼容性验证。只改项目版本从不等于新增兼容性。

## 发布类别

### 稳定版：`X.Y.Z`

纯数字版本是与同版本官方 Node 契约对齐的稳定发布。仓库检查要求此形式的 `Version` 与 `ContractVersion` 相同。

稳定版发布后包含：

- 公开 draft GitHub Release 时在已接受 `main` 提交上创建的 `X.Y.Z` tag；
- 普通 GitHub Release；
- 精确的 `X.Y.Z` GHCR 标签；
- 移动的 GHCR `latest` 通道。

GitHub 也会将该 Release 标记为 Latest。它既是不可变的对齐点，也是稳定通道完成发布后选中的版本。

### 预发布版：`X.Y.Z-rnl.N`

`rnl.N` 是 Remnanode Lite 的预发布修订。它可以用于提前开发下一条官方版本线，也可以在保持旧契约的前提下改进架构、交付或资源行为。

预发布版发布后包含：

- 公开 draft GitHub Release 时在已接受 `main` 提交上创建的 `X.Y.Z-rnl.N` tag；
- GitHub Prerelease；
- 精确的 `X.Y.Z-rnl.N` GHCR 标签；
- 移动的 GHCR `preview` 通道。

预发布版绝不更新 GHCR `latest`，也不会成为 GitHub Latest Release。即使它通过完整自动化流程，仍然是预发布版，直到正式稳定版发布。

同一 `X.Y.Z` 版本线内，`N` 从 1 开始递增。已发布版本和精确镜像标签绝不复用。数字前缀描述项目开发线，并不自动表示对应官方契约已经完成。

### 当前版本线

| 发布线 | 契约 | 类别 | 状态 |
| --- | --- | --- | --- |
| `2.8.0` | `2.8.0` | 稳定版 | 当前契约对齐的发布线；只有已公开 Release 才有 Native bundle |

SemVer 会将 `X.Y.Z-rnl.N` 排在对应的 `X.Y.Z` 稳定版之前。不要据此推断发布顺序或通道选择；workflow 会根据版本格式明确选择 `preview` 或 `latest`。

## Git Tag 与精确镜像标签

正式 Git tag 与精确容器标签一样，直接使用项目版本号：

```text
Git tag:       X.Y.Z
Container tag: ghcr.io/luxiaba/remnanode-lite:X.Y.Z
```

两者都是不可变发布身份，但对应不同对象：

- GitHub Release 创建的 Git tag 标识从 `main` 接受的源码提交；
- 精确容器标签标识该提交已构建、已证明的多架构 manifest。

release workflow 不会重建容器。它先验证 `main` 生成的 `sha-<commit>` 候选，再把同一个 manifest digest 赋予精确版本标签。

不要在工作站上创建或推送发布 tag。创建 draft 本身不会创建 tag；公开 draft 时，GitHub 才会在已接受的 `main` 提交上创建精确版本 tag，并通过 Release immutability 锁定 tag 和资产。tag ruleset 可以禁止更新和删除，但必须允许 `GITHUB_TOKEN` 创建新 tag；标准 GitHub Actions token 不是 ruleset bypass 列表中的 actor。对于尚未公开的版本，workflow 发现已有 tag 时会拒绝继续，而不会接管该 tag。

Registry tag 是名称，不是内容地址。需要最强固定时，请部署：

```text
ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>
```

## 容器引用

| 引用 | 可变性 | 含义 | 使用场景 |
| --- | --- | --- | --- |
| `sha-<40位commit>` | 策略上不可变 | 一个 `main` 提交构建并证明的候选 | 发布验收、复现和诊断 |
| `edge` | 可移动 | 最近一个合格的 `main` 候选 | 仅主线观察 |
| `X.Y.Z-rnl.N` | 策略上不可变 | 一个已发布预发布版 | 受控预发布部署和精确回滚 |
| `preview` | 可移动 | release workflow 最近推进的预发布版 | 主动选择的预发布跟踪 |
| `X.Y.Z` | 策略上不可变 | 一个已发布稳定版 | 推荐的生产部署和精确回滚 |
| `latest` | 可移动 | release workflow 最近推进的稳定版 | 主动选择的稳定更新通道 |
| `name@sha256:...` | 内容寻址 | 一个 registry manifest digest | 最强部署与验证固定 |

普通 `main` push 可以更新 `edge`，但不能更新 `preview` 或 `latest`。只有发布 workflow 在对应 Release 公开后才能推进它们。

`latest` 只指向纯 `X.Y.Z` 稳定版，`preview` 只指向 `X.Y.Z-rnl.N` 预发布版。两个通道永不交叉。移动通道不会自动更新正在运行的容器：Docker 只在显式 `pull` 时检查标签，Compose 也只在显式操作时重建容器。

## 如何选择 Docker 引用

常规生产部署使用精确稳定版本：

```text
ghcr.io/luxiaba/remnanode-lite:X.Y.Z
```

需要最强固定时，记录并使用 manifest digest：

```text
ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>
```

只有接受预发布状态和变更内容时才使用精确预发布标签：

```text
ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N
```

`preview` 适合短期评估；批量测试更适合用精确预发布标签或 digest，以免不同节点在更新间隔中拉到不同镜像。始终保留上一个精确引用，用于回滚。

`latest` 是主动选择的稳定更新通道，不是回滚身份。即使跟踪它，也应阅读 Release、记录已解析 digest，并显式更新：

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

`sha-<commit>` 用于验收可能成为 Release 的候选。不要用 `edge` 进行发布验收，因为新的 `main` 构建可能在测试期间移动它。

candidate workflow 还会把已接受的内容地址记录在已 attestation 的 `release-index.json` 资产中。发布前必须确认该记录与已验证的 `sha-<commit>` 候选一致；Release 变为 immutable 后，恢复流程直接使用其中记录的 digest，不会把 registry tag 当作持久身份。

## Native Linux 只接受精确版本

Native 安装和升级解析的是完整、带版本号的发布 bundle，因此不会跟随移动通道：

```bash
sudo sh install.sh --version "<published-version>"
sudo rnlctl upgrade --to <精确版本>
```

`latest`、`preview`、`edge` 和 `sha-*` 不是有效的 Native 版本输入。精确版本让归档名、`SHA256SUMS`、Release manifest、嵌入版本和源码提交可以作为同一个发布身份校验。

## 何时才算已发布

源码中的版本、一个 `main` 候选或仅有 Git tag，都不是完整发布。

已发布的预发布版同时具备：

1. 公开 draft Release 时在已接受 `main` 提交上创建的 `X.Y.Z-rnl.N` tag；
2. 带已验证资产（包括已 attestation 的 `release-index.json`）的已公开 GitHub Prerelease；
3. 与该 index 中 digest 一致的精确 `X.Y.Z-rnl.N` GHCR 标签；
4. 成功推进到 `preview` 的同一个 digest。

稳定版则具备普通 `X.Y.Z` tag、完整 GitHub Release、精确 `X.Y.Z` 镜像、GitHub Latest 标识以及 `latest` 晋升。

两类发布通过同样的代码、兼容性、资产、provenance 和 attestation 门禁。区别在发布状态和通道，不在于预发布版走了一条更弱的构建路径。

在对外宣布版本或资产 URL 可用前，请确认 Git tag、GitHub Release 和精确 GHCR 标签都已经存在。

## 跟进官方 Node 发布

定时契约 workflow 发现新的官方版本后只创建 Issue；它不会自动修改 `ContractVersion`、源码、项目版本或容器标签。

同步新契约需要固定官方版本与不可变源码提交，审计路由、schema、错误、side effect 和插件依赖，更新契约证据与测试，对齐 Go 实现，再用目标 Panel、rw-core 和 Linux 环境验证。完成后才能修改 `ContractVersion`。

项目版本单独选择。对齐完成前可以先发布新的 `X.Y.Z-rnl.N`，但二进制必须继续报告真正实现的契约；只有项目版本和契约版本相同时才允许纯稳定版本。

## 版本输出与发布元数据

Node 和 `rnlctl` 都报告项目版本与契约版本：

```text
remnanode-lite <Version> (contract <ContractVersion>)
rnlctl <Version> (contract <ContractVersion>)
```

发布记录还应清楚标明发布类别与通道、项目版本和源码提交、官方契约与固定源码、已接受的镜像 digest 与 attestation、将其绑定到源码提交的 `release-index.json`、锁定的运行时资产、两种架构的发布状态、已知风险与回滚引用，以及 Native bundle 校验和和资产 attestation。

GitHub 根据已合并变更生成 Release notes。主机清单、Panel 详情、日志、Secret 和其他运行观察不属于 Release 资产，也不能提交到仓库。

`NODE_CONTRACT_VERSION` 仅用于受控诊断和紧急兼容性测试；它不改变实现行为、固定证据、二进制身份或发布声明。
