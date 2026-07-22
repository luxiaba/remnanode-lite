<!-- translation: locale=zh-CN; source=docs/versioning.md; source-sha256=537ceb6d5ee75178d7d68eb114849d7a656d0efc51c5dcac090f9b15b064ae28 -->

# 版本与镜像标签

[英文原文](../../versioning.md) · [文档索引](README.md) · [发布流程](release.md)

Remnanode Lite 将项目身份与兼容性声明分开。代码可以独立演进，同时继续实现
已经验证的旧版官方 Node 契约。发布名称和移动镜像通道也彼此独立：精确版本只
标识一个发布，`preview` 与 `latest` 选择的是发布类别。

本文定义这些身份与通道。Release workflow 是发布行为的可执行真相源，
`scripts/release-metadata.sh` 负责 stable/preview 分类。

| 维度 | 真相源 | 含义 |
| --- | --- | --- |
| 项目 `Version` | `internal/version/version.go` | 本项目源码、二进制、Release 资产和精确容器标签的身份 |
| 官方 `ContractVersion` | `internal/version/contract.version`、`internal/version/version.go` 与固定契约证据 | 实际实现并向 Panel 报告的官方 Node 行为 |
| Panel 集成目标 | 维护者发布验证 | 实际集成测试使用的 Panel 版本；不会编译进发布身份 |
| rw-core 与运行时资产 | `release/runtime-assets.lock.json` | Docker 和 Native Linux 共用的 core、GeoIP、GeoSite、ASN、源码、许可证与校验和输入 |

例如：

```text
Version:         2.8.0-rnl.1
ContractVersion: 2.8.0
```

这表示第一个包含新 Native Linux 发行方案的项目预览版，仍实现官方 Node `2.8.0` 契约。`rnl.1` 是本项目自己的版本，不是官方项目发布的修订号。仅修改 `Version` 不会扩大契约声明。

修改 `ContractVersion` 必须固定官方源码、审查契约差异、同步实现和测试，并完成兼容性验证。只修改 `Version` 永远不会扩大契约声明。

## 两类正式发布

每个项目版本只能采用以下一种格式。

### 稳定版：`X.Y.Z`

纯版本表示已经对齐同号官方契约的稳定版。仓库门禁要求此时 `Version == ContractVersion`。

稳定版发布为：

- annotated Git tag `vX.Y.Z`；
- 普通 GitHub Release，并标记为 Latest；
- 精确 GHCR 标签 `X.Y.Z`；
- 移动 GHCR 稳定通道 `latest`。

GitHub Release 也会标记为 Latest。因此，纯版本既是不可变的同版本对齐点，也是成功
发布后由稳定通道选中的版本。

### 预览版：`X.Y.Z-rnl.N`

`rnl.N` 是 Remnanode Lite 的项目预览版，可用于提前开发下一版本，也可在不改变官方契约的情况下改进架构、发行和资源行为。

预览版发布为：

- annotated Git tag `vX.Y.Z-rnl.N`；
- GitHub Prerelease；
- 精确 GHCR 标签 `X.Y.Z-rnl.N`；
- 移动 GHCR 预览通道 `preview`。

预览版绝不会更新 `latest`，也不会成为 GitHub Latest Release。即使完整自动化门禁通过，
它仍然是预览版，只有发布纯稳定版本后才会形成稳定里程碑。

同一 `X.Y.Z` 线中，`N` 从 1 开始单调递增；已经发布的编号和精确标签不得复用。数字前缀表示项目开发线，不等于已经实现同号官方契约。

## 当前版本线

| 版本 | 契约 | 类型 | 状态 |
| --- | --- | --- | --- |
| `2.8.0` | `2.8.0` | 稳定版 | 已发布；现有 tag 与历史保持不变 |
| `2.8.0-rnl.1` | `2.8.0` | 预览版 | 计划中的首个自包含 Native Linux bundle 发行 |

SemVer 会把 `2.8.0-rnl.1` 排在 `2.8.0` 之前，即使预览版日历发布时间更晚。不要依靠 SemVer 排序判断最新通道；workflow 根据标签语法明确选择 `preview` 或 `latest`。

发布预检还会把稳定版本与已有稳定 Git tag 比较，拒绝更低的版本，避免合法的 tag
语法或契约检查通过后仍意外让 `latest` 回退。

## Git 与容器标签

```text
Git tag:       v2.8.0-rnl.1
Container tag: ghcr.io/luxiaba/remnanode-lite:2.8.0-rnl.1
```

Git tag 带 `v`，容器标签不带。两者都是不可移动的发布身份，但标识的是不同对象：

- annotated Git tag 标识从 `main` 接受的源码提交；
- 精确容器标签标识该提交已经构建并完成证明的多架构 manifest。

Release workflow 不重新构建镜像，而是验证当前 `main` 的 `sha-<commit>` 候选，
再把同一 digest 赋予精确版本标签。仓库规则应禁止更新和删除 `v*`；workflow 还会
在建立 draft、发布 Release 和推进通道前重新解析远端 annotated tag，任何 tag 漂移
都会失败关闭。

registry tag 本质上仍是名称。要求最强固定时使用：

```text
ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>
```

## 容器引用

| 引用 | 可变性 | 含义 | 用途 |
| --- | --- | --- | --- |
| `sha-<40-character-commit>` | 按政策不可变 | 一个 `main` 提交构建并完成 attestation 的候选 | 发布验证、复现和诊断 |
| `edge` | 移动 | 最新合格 `main` 候选 | 仅用于主线观察 |
| `X.Y.Z-rnl.N` | 按政策不可变 | 一个已发布预览版 | 受控预览部署与回滚 |
| `preview` | 移动 | 最近晋升的预览版 | 主动跟随预览通道 |
| `X.Y.Z` | 按政策不可变 | 一个已发布稳定版 | 推荐生产部署与回滚 |
| `latest` | 移动 | 最近晋升的稳定版 | 主动跟随稳定通道 |
| `name@sha256:...` | 内容寻址 | 一个 registry manifest digest | 最严格的部署与验证固定 |

普通 `main` push 只能更新候选和 `edge`，不能更新 `preview` 或 `latest`。只有对应
Release 发布后，Release workflow 才能晋升这些通道。

### 稳定与预览通道永不重叠

两条移动通道有意保持分离：

- `latest` 只解析到纯 `X.Y.Z` 稳定版；
- `preview` 只解析到 `X.Y.Z-rnl.N` 预发布版；
- 预览版不能推进、替换或修复 `latest`；
- 稳定版不能推进 `preview`。

移动通道不会自动更新运行容器。只有显式 `docker compose pull` 并 recreate 才会运行新镜像。

## 如何选择 Docker 引用

生产通常使用精确稳定版：

```text
ghcr.io/luxiaba/remnanode-lite:2.8.0
```

需要最强固定时，记录并部署其 manifest digest：

```text
ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>
```

接受预览状态时才使用精确预览版：

```text
ghcr.io/luxiaba/remnanode-lite:2.8.0-rnl.1
```

`preview` 适合短期评估；fleet 测试仍优先精确 tag 或 digest，因为它不会在节点之间移动。
保留上一个精确引用用于回滚。

`latest` 是主动选择的稳定更新通道，不是回滚身份。即使跟随它，也要先阅读 Release、
记录解析后的 digest，再显式更新：

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

回滚永远使用记录的精确版本或 digest，而不是移动通道的历史含义。

使用 `sha-<commit>` 验证可能成为 Release 的候选，不要用 `edge` 做发布验收，因为另一个
`main` 构建可能在测试期间移动它。

## Native 只接受精确版本

Native bundle 把 archive 名称、`SHA256SUMS`、manifest、嵌入版本和源码 revision 作为同一个发行身份校验，因此不能跟随移动通道：

```bash
sudo sh install.sh --version 2.8.0-rnl.1
sudo rnlctl upgrade --to 2.8.0-rnl.2
```

`latest`、`preview`、`edge` 和 `sha-*` 都不是合法 Native 版本参数。

## 什么才算已发布

源码版本字符串、`main` 候选或单独的 Git tag 都不能单独构成完整发布。

预览版发布必须同时具备：

1. 指向接受的 `main` 提交的 annotated `vX.Y.Z-rnl.N` tag；
2. 已发布且资产验证完成的 GitHub Prerelease；
3. 与候选 digest 相同的精确 GHCR 标签；
4. 同一 digest 成功晋升到 `preview`。

稳定版对应普通 `vX.Y.Z` tag、正式 GitHub Release、精确 `X.Y.Z` 镜像、GitHub Latest 和 GHCR `latest`。

两类发布使用相同的代码、兼容性、资产、来源证明和 attestation 门禁；区别只是发布状态
和通道，不是预览版的构建路径更弱。

在把计划中的版本或资产 URL 告知使用者前，应检查 Git tag、GitHub Release 和精确 GHCR 标签
确实已经存在。

## 跟进官方版本

定时 workflow 只会在发现官方新版本时创建 Issue，不会自动修改契约、源码、项目版本或镜像标签。
同步新契约需要：固定官方版本和不可变提交；审计 route、schema、error、side effect 和插件
依赖；更新契约证据与测试；对齐 Go 实现；用目标 Panel、rw-core 和 Linux 环境验证；最后才
修改 `ContractVersion`。

项目版本应独立选择。对齐完成前可以先发布新的 `X.Y.Z-rnl.N`，但二进制必须继续报告实际
实现的旧契约；只有项目版本和契约版本相同，才能使用纯稳定版本。

## 版本输出与发布元数据

Node 和 `rnlctl` 都会报告项目版本与契约版本：

```text
remnanode-lite <Version> (contract <ContractVersion>)
rnlctl <Version> (contract <ContractVersion>)
```

发布记录还应标明：

- 发布类别和晋升的通道；
- 项目版本、Git tag 和源码提交；
- 官方契约版本与固定源码提交；
- 已接受的容器 manifest digest 及其 attestation；
- 维护者验收使用的 Panel 和运行环境范围；
- 固定的 rw-core 与运行时资产版本；
- `amd64` 与 `arm64` 的发布状态；
- 已知差异、风险和回滚引用；
- Native bundle 校验和与资产 attestation。

GitHub 会根据合并变更自动生成 Release notes。宿主清单、Panel 详情、日志、secret 和其他
运行时观察结果不是 Release 资产，也不得提交到仓库。

`NODE_CONTRACT_VERSION` 仅用于受控诊断和紧急兼容性测试，不会改变实现行为、固定证据、
二进制身份或发布声明。
