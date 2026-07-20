<!-- translation: locale=zh-CN; source=docs/README.md; source-sha256=2aa2c1a8af7dd4a53a1ff38c98f79befbd165fad6ccc797999fee870b400c988 -->

# Remnanode Lite 文档中心

> [!IMPORTANT]
> 英文是唯一权威来源；本页是便于阅读的简体中文翻译。请以[英文文档索引](../../README.md)为准。

[根目录中文 README](../../../README.zh-CN.md) · [本地化说明](../README.md)

这里是 Remnanode Lite 的文档入口。README 负责让使用者快速认识和启动项目，本目录保存完整的项目定位、架构、部署、开发、兼容与发布说明。

如果文档描述与代码、发布资产或实际运行结果不一致，请按本文末尾的“真相源”判断，并在同一个变更中修正文档。

## 按角色开始

### 我要部署一个节点

1. 先阅读[项目定位与目标](project.md)，确认支持范围和非目标。
2. Docker 用户使用 [Docker Compose 部署](deployment-docker.md)；systemd/OpenRC 用户使用[原生 Linux 部署](deployment-native.md)。
3. 按[配置参考](configuration.md)填写运行参数、Secret 和可选能力。
4. 在选择镜像前阅读[版本与镜像标签策略](versioning.md)，区分 `latest`、精确版本、`edge` 和 `sha-*`。
5. 启动后按[运维手册](operations.md)检查服务健康、Panel 连接和 rw-core 日志。

目标机器是整机 `512 MiB RAM / 1 vCPU / 2 GB disk` 时，应保留仓库提供的内存、CPU、PID、tmpfs 和日志限制，不要在生产节点上进行源码构建。

### 我要维护线上节点

1. 从[运维手册](operations.md)的健康检查、日志、更新、回滚和故障定位开始。
2. 对照[配置参考](configuration.md)确认当前部署方式的配置来源与覆盖关系。
3. 对照[资源预算](development/resource-budget.md)理解内存、磁盘、日志和关闭预算。
4. 出现协议或生命周期问题时，查看[架构与运行时设计](architecture.md)和[契约基线](development/contract-2.8.0.md)。
5. 回滚必须使用之前记录的精确版本或 manifest digest，不使用 `edge`，也不依赖 `latest` 的历史指向。

### 我要阅读或修改 Go 代码

最短上手路径不要求先读完全部设计文档：

1. 用 5 分钟阅读[项目定位与目标](project.md)，确认兼容边界和非目标。
2. 按[开发上手与代码导航](development/README.md)准备工具链、跑通普通测试并定位目标包。
3. 只阅读[架构与运行时设计](architecture.md)中与目标组件相关的所有权、数据流和锁序章节。
4. 按[测试策略](development/testing.md)选择与改动风险匹配的验证；提交前遵循[贡献指南](contributing.md)。
5. 只有修改 `/node` 行为、DTO 或错误语义时，才必须先阅读版本化的[官方 2.8.0 契约基线](development/contract-2.8.0.md)。

### 我要同步官方版本或发布

1. 先阅读[版本与镜像标签策略](versioning.md)，不要把 `Version` 与 `ContractVersion` 绑定。
2. 查看[改造路线](development/roadmap.md)与当前兼容契约。
3. 按[发布流程](release.md)冻结候选、准备记录并执行门禁。
4. 验收数据必须遵循[发布验收证据协议](development/release-acceptance.md)。
5. 发布结果写入[英文变更日志](../../../CHANGELOG.md)和对应 Release note；不存在 Git tag 与 Release 资产时，不得把开发版本描述成已发布版本。

## 完整导航

### 项目与治理

| 文档 | 内容 |
| --- | --- |
| [项目定位与目标](project.md) | 出发点、与官方的关系、目标、非目标、受众和当前状态 |
| [版本与镜像标签策略](versioning.md) | 项目版本、契约版本、正式对齐版本及 GHCR 标签语义 |
| [改造路线](development/roadmap.md) | 已完成里程碑、当前发布验收和后续事项 |
| [贡献指南](contributing.md) | 分支、提交、测试、审查和文档同步要求 |
| [安全策略](security.md) | 漏洞报告方式、受支持版本和敏感信息边界 |
| [许可证（英文唯一权威来源）](../../../LICENSE) | AGPL-3.0-only 许可说明；本地化树不复制许可证 |

### 部署与运维

| 文档 | 内容 |
| --- | --- |
| [Docker Compose 部署](deployment-docker.md) | 单文件部署、资源限制、镜像选择、日志、更新和回滚 |
| [原生 Linux 部署](deployment-native.md) | Debian/Ubuntu systemd 与 Alpine OpenRC 安装、升级和卸载 |
| [配置参考](configuration.md) | runtime、容器、安装器和构建变量的作用域、默认值与安全要求 |
| [运维手册](operations.md) | 健康检查、日志、更新、回滚、磁盘维护和故障排查 |
| [根目录 Compose](../../../compose.yaml) | 生产容器约束的可执行配置 |
| [单文件 Compose 示例](../../../deploy/compose.single-file.yaml) | 直接内联运行变量、适合大量独立小节点的完整模板 |
| [容器环境模板](../../../.env.example) | 选择 `.env` 部署方式时使用的变量模板 |
| [原生环境模板](../../../deploy/node.env.example) | systemd/OpenRC 安装的 Node 配置模板 |
| [资源预算](development/resource-budget.md) | 512 MiB 目标、实测基线、保护策略和关闭预算 |

### 架构、开发与测试

| 文档 | 内容 |
| --- | --- |
| [架构与运行时设计](architecture.md) | 组件边界、请求链、Xray 生命周期、插件、网络和资源所有权 |
| [开发上手与代码导航](development/README.md) | Go 工具链、目录职责、常用命令和开发工作流 |
| [测试策略](development/testing.md) | 单元、race、契约、Linux namespace、容器和发布测试 |
| [官方 2.8.0 契约基线](development/contract-2.8.0.md) | 固定官方证据、26 条路由、请求响应和已知差异 |
| [历史审计整改记录](archive/2026-07-audit-remediation.md) | 首轮静态审计的历史范围；不作为当前状态真相源 |

### 发布与验收

| 文档 | 内容 |
| --- | --- |
| [发布流程](release.md) | 候选冻结、验收、tag、Release、GHCR 和回滚 |
| [发布验收证据协议](development/release-acceptance.md) | 验收文件、环境、数据边界和机器校验规则 |
| [变更日志（英文唯一权威来源）](../../../CHANGELOG.md) | 已发布和待发布的用户可见变化；本地化树不复制该文件 |

## 关键概念

### 项目版本不等于兼容版本

`Version` 标识 Remnanode Lite 自身构建，`ContractVersion` 表示当前实际实现并向 Panel 报告的官方 Node 契约。项目可以提前开发新的 `rnl.N` 版本，但不能提前上报未完成的官方契约。详见[版本策略](versioning.md)。

### 候选镜像不等于正式发布

`edge`、`sha-*` 和手动触发的 `candidate-sha-*` 来自 `main` 构建，用于主线观察与服务器验收。只有实际存在且按发布政策不移动的 Git tag、GitHub Release 和精确 GHCR 标签才构成正式发布；需要技术上的内容寻址时使用 manifest digest。代码或文档中出现一个版本字符串，不代表对应资产已经发布。

### 兼容结论必须有边界

静态契约测试、真实 Panel 连接、长期资源测试和发行环境验收证明的是不同层面。文档应分别写明“代码已实现”“测试已通过”“环境已验收”和“版本已发布”，不能用其中一项替代全部结论。

## 术语速查

| 术语 | 在本项目中的含义 |
| --- | --- |
| Node | 长期运行的 `remnanode-lite` 控制进程；接收 Panel 请求并拥有 rw-core 生命周期 |
| rw-core | 实际承载代理数据面的 Xray Core 二进制，由 Node 启停和管理 |
| `Version` | 本项目构建、GitHub Release 和精确镜像的版本身份 |
| `ContractVersion` | 当前已实现并默认向 Panel 上报的官方 Node 行为基线 |
| operation epoch | 识别一次 Xray start/stop 操作所有权的递增值，不是 rw-core 进程身份 |
| process lease | 绑定具体 rw-core process epoch 与 abstract socket 的短期许可，防止一次 mutation 跨进程执行 |
| lifecycle lease | HTTP 层协调 start、stop、Plugin/用户 mutation 和 reset-capable stats 的共享/独占许可，不是持久锁文件 |
| 候选 `C` | 代码进入 `main` 后被冻结并接受真实环境验收的 commit |
| 最终提交 `F` | `C` 之后只增加发布资料的最终 `main` commit；正式 Git tag 指向它 |
| manifest digest | GHCR 多架构镜像索引的 `sha256:...` 内容地址，比可移动 tag 更适合严格固定 |

## 文档真相源

| 事实 | 首要真相源 | 文档职责 |
| --- | --- | --- |
| 项目构建版本 | `internal/version/version.go` | 解释含义，不单独证明已发布 |
| 官方契约版本 | `internal/version/contract.version`、`internal/contract` | 记录固定证据与已知差异 |
| 公开路由 | `internal/httpserver/node_routes.go` | 解释行为与入口，不复制另一份注册表 |
| 请求/响应约束 | `internal/contract`、`internal/nodeapi` | 提供可读摘要与验证方法 |
| runtime 配置 | `internal/config/config.go` | 说明默认值、优先级与安全边界 |
| 容器运行约束 | `compose.yaml`、`Dockerfile` | 解释为何设置能力、资源与 tmpfs |
| CI 与发布行为 | `.github/workflows`、`scripts/*check*.sh` | 给出维护流程并与自动化保持一致 |
| 正式发布状态 | Git tag、GitHub Releases、GHCR 精确标签 | 不预告为既成事实，不使用不存在的下载地址 |
| 资源上限 | 代码常量、集成测试和候选验收记录 | 区分设计上限、工程基线与正式验收数据 |

## 文档维护约定

- 行为、配置、版本或 workflow 变化必须在同一 PR 更新对应文档。
- 版本化契约和基准报告记录特定时间点，不使用含义不明的“当前”替代具体版本或日期。
- README 保持简洁，详细原理和操作放入本目录，并从本文导航。
- 命令示例不得包含真实 Secret、JWT、证书、主机地址或用户数据。
- 示例版本只用于解释时必须明确标注；面向部署的命令只能引用实际存在的 tag、digest 或用户主动选择的 `sha-*`。
- 历史审计与路线记录不得覆盖当前代码和自动化所定义的事实。
- 新增文档后应同时加入本文导航，并检查所有相对链接。
- 提交前运行 `go run ./cmd/docs-check`；仓库门禁会校验 H1、代码围栏、本地文件、锚点和从根 README 可达性。
