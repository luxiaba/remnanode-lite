<!-- translation: locale=zh-CN; source=docs/versioning.md; source-sha256=d1ea36548d542eeab32fb79026fc98456cf3352d14e53699754a4418a5d6724c -->

# 版本与镜像标签策略

> [!IMPORTANT]
> 英文是唯一权威来源；本页是便于阅读的简体中文翻译。请以[英文原文](../../versioning.md)为准。

[返回文档索引](README.md)

Remnanode Lite 同时面对项目自身迭代、官方 Node 契约、Panel 集成和 rw-core 运行时。它们变化的节奏不同，不能压缩成一个含义模糊的“当前版本”。

本文件是版本命名与镜像标签的规范。具体发布操作见[发布流程](release.md)。

## 四个独立维度

| 维度 | 真相源 | 含义 |
| --- | --- | --- |
| 项目版本 `Version` | `internal/version/version.go` | 本项目代码、二进制、Release 和精确镜像的身份 |
| 官方契约 `ContractVersion` | `internal/version/contract.version` 及固定源码证据 | 当前真正实现并向 Panel 报告的官方 Node 行为基线 |
| Panel 验证目标 | 对应版本的验收记录 | 完成集成验证时使用的 Panel 版本，不是编译版本 |
| rw-core 版本 | Dockerfile、安装脚本及发布记录 | 镜像或原生安装实际携带并验证的 core 版本 |

`Version` 与 `ContractVersion` 必须显式解耦。项目可以提前进入下一条开发线，也可以在已对齐的官方版本上继续改进，而不应因此伪报新的官方契约。

例如，以下组合是合理的：

```text
Version:         2.8.1-rnl.1
ContractVersion: 2.8.0
```

它表示项目已经开始 `2.8.1` 开发线的自主迭代，但当前可证明、可上报的官方 Node 契约仍是 `2.8.0`。只有完成官方 `2.8.1` 的源码固定、契约差分、实现调整和验收后，`ContractVersion` 才能改为 `2.8.1`。

## 项目版本格式

项目接受两类正式版本标识。

### 自主迭代版本：`X.Y.Z-rnl.N`

`rnl.N` 是 Remnanode Lite 自身的迭代编号，不表示“官方版本的第 N 次修订”，也不直接证明与官方 `X.Y.Z` 兼容。

它可以用于：

- 在官方对应版本发布前，提前开发下一条项目版本线；
- 在某个官方契约基线上继续修复 bug、改善架构或降低资源占用；
- 发布已验证但包含本项目独立演进内容的稳定构建；
- 清楚地区分本项目构建与官方同名版本。

同一个 `X.Y.Z` 命名空间内，`N` 从 1 开始单调递增。已经发布的编号不得复用，已有 tag 和精确镜像标签不得移动。

项目版本的前三段表示项目所处的开发或发行线，不自动等于 `ContractVersion`。发布说明必须单独记录实际契约基线。

### 官方对齐版本：`X.Y.Z`

不带 `rnl.N` 的纯版本只用于正式完成官方同版本行为对齐的发布。创建该版本前必须满足：

- `ContractVersion` 已更新为同一个 `X.Y.Z`；
- 官方源码版本与提交已经固定；
- 契约差分和代码实现已经完成；
- 要求的自动化门禁与真实环境验收已经通过；
- 发布说明明确记录 Panel、rw-core、架构和已知限制。

纯 `X.Y.Z` 是一个不可变的对齐里程碑，而不是可被后续修复反复覆盖的浮动标签。之后仍可继续发布 `X.Y.Z-rnl.N` 作为本项目的独立完善版本。

## 时间顺序示例

下面只说明命名关系，不表示这些版本已经发布：

```text
2.8.1-rnl.1  提前进入项目 2.8.1 开发线，契约仍可能是 2.8.0
2.8.1-rnl.2  继续实现或修复
2.8.1        已完成官方 Node 2.8.1 的正式行为对齐
2.8.1-rnl.3  在该契约基础上继续本项目的独立完善
2.8.1-rnl.9  后续验证稳定构建，latest 可以指向它
2.8.2-rnl.1  开始下一条项目开发线，契约按实际完成情况上报
```

`rnl.N` 在语义化版本中属于预发布标识，因此 `2.8.1-rnl.9` 的 SemVer 优先级低于纯 `2.8.1`，即使它在时间上发布得更晚。本项目不会依赖 SemVer 自动排序决定“最新稳定版”；GitHub Release 展示和 GHCR `latest` 必须由发布流程显式选择。

## Git tag 与容器标签

Git 的正式发布 tag 带 `v` 前缀，容器版本标签不带：

```text
Git tag:       v2.8.1-rnl.9
Container tag: ghcr.io/luxiaba/remnanode-lite:2.8.1-rnl.9
```

两者都按项目政策不可重写，但指向的对象不同：正式 Git tag 指向发布资料最终提交 `F`，而精确容器 tag 指向已从候选提交 `C` 构建并完成验收的 manifest digest。`F` 是发布时最新的 `main` HEAD，验证器要求它只比 `C` 多一个白名单内的发布资料提交；release workflow 会拒绝从 `dev`、临时分支或旧的 `main` 提交发布，也不会从 `F` 重新构建另一份容器。

## 镜像渠道

| 标签 | 可变性 | 来源 | 适用场景 |
| --- | --- | --- | --- |
| `sha-<40位提交>` | 首次发布后拒绝移动 | `main` 的具体 commit 和已证明 manifest digest | 候选验收、精确复现和问题定位 |
| `candidate-sha-<40位提交>` | 首次发布后拒绝移动 | 从 `main` 手动触发的独立候选构建 | 自动候选缺失或需要重新构建时的验收入口 |
| `edge` | 可变 | 最新的 `main` 容器构建 | 观察主线，不作为稳定性承诺 |
| `X.Y.Z-rnl.N` | 按政策不移动 | 对应自主迭代 Release | 固定部署已验证的项目版本 |
| `X.Y.Z` | 按政策不移动 | 对应官方对齐 Release | 固定部署已验证的官方对齐里程碑 |
| `latest` | 可变 | 最近一次完整成功的正式 Release | 希望主动跟随稳定版本的节点 |
| `name@sha256:...` | 内容寻址 | Registry manifest digest | 最严格的生产固定与供应链核验 |

手动从 `main` 触发候选发布时，workflow 只生成 `candidate-sha-<commit>`，不会覆盖自动 push 已发布的 `sha-<commit>`。两者都表示同一源码提交的候选构建，但手工重建的 manifest digest 不保证与此前构建相同。tag 只负责定位候选；验收记录中的 commit 与实际 manifest digest 才是正式发布验证和晋升的规范身份。

### `latest` 的准确含义

`latest` 表示“本项目当前推荐的、已经完成相应验证的稳定构建”。它可以指向纯 `X.Y.Z`，也可以指向后续的 `X.Y.Z-rnl.N`。

因此：

- `latest` 不等于“与官方最新版本完全一致”；
- `latest` 不指向 `edge`，也不应由普通 `main` push 更新；
- 较旧版本的补发或重跑不得让 `latest` 倒退；
- 只有正式发布流程可以移动 `latest`，每个成功完成全部门禁的正式 tag 都会自动成为新的 `latest`；
- 移动 `latest` 不会自动替换正在运行的容器。

创建正式 tag 就表示维护者选择该版本进入稳定通道。尚不准备成为 `latest` 的构建不应伪装成正式 Release，应继续使用 `sha-*` 或 `candidate-sha-*` 完成验收。

使用 `latest` 的服务器仍需主动执行：

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

## 部署时如何选择

### 生产固定版本

推荐使用精确项目版本或 manifest digest：

```text
ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N
ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>
```

它们便于审核变更、批量灰度和快速回滚。精确版本适合大多数节点，digest 适合需要防止 Registry tag 被意外移动的环境。

### 自动跟随稳定版

可以使用：

```text
ghcr.io/luxiaba/remnanode-lite:latest
```

这适合愿意在每次更新前阅读 Release note、主动拉取并验证的节点。`latest` 是更新渠道，不是回滚依据；回滚时必须使用之前记录的精确版本或 digest。

### 候选验收

服务器验收通常从完整的 `sha-<40位提交>` 定位自动候选；需要手动重建时可以从 `candidate-sha-<40位提交>` 定位。开始验收前必须把所选 tag 解析为 manifest digest，后续运行、证据和正式晋升全部固定该 digest，同时从同一 commit 获取部署文件。不要使用 `edge` 记录验收证据，因为它会随下一次 `main` 构建移动。

## 发布与稳定性的关系

修改 `Version`、合入 `main` 或成功构建候选镜像，都不等于已经发布正式版本。一次正式发布至少包含：

1. 版本、契约和依赖元数据一致；
2. 候选 commit、候选 manifest digest 和二进制通过规定的代码与环境验收；
3. 创建发布后按政策不可移动的 Git tag；
4. GitHub Release 和二进制资产成功生成，已验收的候选 digest 被晋升为精确 GHCR 版本；
5. 候选镜像摘要和 attestation 可以按源码提交验证；
6. 发布说明记录真实兼容范围与已知风险；
7. 发布 workflow 自动把同一 digest 晋升为 `latest`，并把对应 GitHub Release 标记为 Latest。

仓库中出现的版本字符串可能代表开发目标。判断一个版本是否已经发布，应以 Git tag、GitHub Releases 和 GHCR 中实际存在的精确标签为准。当前源码版本是 `2.8.0`；源码字符串本身永远不能作为已经发布的证据。

## 官方版本同步

官方 Node 发布新版本后，自动化只负责发现变化并创建同步 Issue，不直接修改 `ContractVersion`、代码或镜像标签。同步流程为：

1. 固定官方版本和不可变 commit。
2. 审计路由、schema、错误、副作用和插件依赖变化。
3. 更新版本化契约证据与测试。
4. 调整 Go 实现并完成代码回归。
5. 使用目标 Panel、rw-core 和 Linux 环境执行验收。
6. 根据实际结果更新 `ContractVersion`。
7. 选择纯官方对齐版本或合适的 `rnl.N` 项目版本发布。

任何“提前开发”的项目版本都不能跳过第 2 至第 6 步后直接上报尚未实现的契约版本。

## 版本输出与发布记录

二进制版本输出同时展示项目版本和契约版本：

```text
remnanode-lite <Version> (contract <ContractVersion>)
```

每份正式 Release note 至少应列出：

- 项目版本与 Git tag；
- 候选提交 `C` 与验收 manifest digest；最终 Release 提交 `F` 由正式 Git tag 唯一解析，不能写入其自身 commit 中；
- ContractVersion 及官方源码 commit；
- 已验证的 Panel 版本；
- 打包的 rw-core 版本与资产摘要；
- `amd64`、`arm64` 支持状态；
- 资源验收范围；
- 已知差异和回滚方式；
- 镜像 manifest digest 与验证命令。

`NODE_CONTRACT_VERSION` 运行时覆盖只用于受控调试或紧急兼容验证。它不会改变二进制真实实现、契约证据或发布身份，不得用于制造虚假的兼容声明。
