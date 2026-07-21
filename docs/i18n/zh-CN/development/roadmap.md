<!-- translation: locale=zh-CN; source=docs/development/roadmap.md; source-sha256=f57ae26bc5f10062f52e13341cc8d320832e1f05cd1b3f55d7d598d89ecf9514 -->
# Remnanode Lite 路线图

> 这是中文译文；路线和状态以[英文原文](../../../development/roadmap.md)为准。

[返回开发文档](README.md) · [项目说明](../project.md) · [版本模型](../versioning.md)

## 项目目标

本仓库维护一套独立的 Go 实现和自己的发布历史。官方 `remnawave/node` 只作为行为与契约参考，不是 Git 上游。[项目说明](../project.md)定义长期目标、受众和非目标；本页记录里程碑和后续工作。

首个版本线从 `2.8.0` 开始，目标如下：

- 对官方 Node `2.8.0@596f015` 达到行为级兼容。
- 与 Panel `2.8.1` 完成真实集成验证。
- 修复已知生命周期、插件、防火墙、契约和安装供应链问题。
- 以在 `512 MiB RAM / 1 vCPU / 2 GB disk` 的 Linux 节点稳定运行为工程目标；
  与候选绑定的整机运行验证安排在首个版本之后。
- 提供 Linux `amd64` 与 `arm64` 产物，`2.8.0` 的运行时验收范围限定为生产 `amd64` Docker 方案。
- 保留 Debian/systemd 与 Alpine/OpenRC 安装路径，原生运行期验收延后到首个版本之后。

项目版本与官方契约版本彼此独立。`X.Y.Z-rnl.N` 是项目自己的迭代标识，既可以用于提前开发下一条版本线，也可以继续完善已有的官方基线。纯 `X.Y.Z` 只有在对应官方契约完成对齐后才能发布。官方发布监测只会创建同步 Issue，不会自动修改契约或发布任何内容。完整规则见[版本模型](../versioning.md)。

## 设计原则

1. 兼容性以官方契约和外部可观测行为为准，不以复刻官方 TypeScript 架构为目标。
2. 所有请求必须在产生副作用前完成完整校验。
3. 外部副作用必须通过可替换接口执行，并返回可传播的错误。
4. 状态只在外部操作成功后提交；失败必须允许同一请求安全重试。
5. 所有并发、队列、请求体和缓存都必须有明确上限。
6. Node 只管理自己启动的 rw-core 进程、内部 socket 和 nftables 私有表，不接管整机防火墙。按 IP 执行 socket destroy 可能影响宿主 network namespace，属于必须明确记录的副作用。
7. `dev` 是稳定开发与集成分支，主题分支通过 PR 和 CI 进入；`main` 是发布分支，只从 `dev` 接收已通过代码门禁的候选。
8. 候选合入 `main` 后才冻结为 M8 发布候选 `C`；此后不再混入功能修改，真实验收结果必须绑定该 commit。

## 兼容边界

- `/node` 路由严格遵循官方 Node 2.8.0 的 HTTP 方法、请求、响应和错误语义。
- 自有诊断或运维能力只放在 CLI 或独立内部接口，不扩展官方 `/node` 契约。
- Node 重启后等待 Panel 重新下发配置，不从磁盘恢复可能已失效的完整代理配置。
- 请求体上限和资源保护允许形成有文档的安全偏差，但必须返回明确错误，不能静默降级。
- nftables 插件使用独立表，可与 firewalld 共存；端口开放由系统管理员负责。

## 当前进度

| 里程碑 | 状态 |
| --- | --- |
| M0 自有项目基线 | 已完成 |
| M1 契约证据 | 已完成 |
| M2 API 边界 | 已完成 |
| M3 Xray 生命周期 | 已完成 |
| M4 插件与 nftables | 已完成 |
| M5 用户、连接与统计 | 已完成 |
| M6 512 MiB 资源优化 | 已完成 |
| M7 系统与供应链 | 已完成 |
| M8 发布验收 | 推进中 |

2026-07-15 的 M6 50,000 用户测量和 2026-07-19 的 M7 init/发行环境快照仍是有价值的工程基线。它们早于候选 `C`，因此不是当前 M8 候选的运行时证据，也不需要为 `2.8.0` 重新执行。M6 完成的是有界资源实现，并未完成延期的 `whole-host-512mib-runtime` profile。

`2.8.0` 仍未发布，M8 正在推进。实现、CI、候选镜像流水线和代码级 512 MiB 约束已经落地。现在只有一个验收方案会阻塞发布：冻结候选上的 `docker-production-smoke-v2`。运行时先把候选 tag 解析为不可变的 manifest 摘要，再用生产 Compose 模板在实际记录的原生 `amd64`/`x86_64` 宿主机上运行该摘要；期间必须观察到真实 Panel 连接和代理流量，并确认容器资源限制精确、持续健康运行，未发生 OOM kill 或重启。宿主机资源会记录，但不要求符合整机 512 MiB 目标。`main` 上的 `sha-*` tag 只用于定位候选，既不是验收身份，也不是正式 Release。

## 当前重点

- **当前**：完成冻结候选的 `amd64` Docker 生产 smoke，记录完成 M8 验收所需的宿主容量、严格容器限制、Panel、流量、进程状态、内存、OOM 和重启观测。
- **下一步**：根据官方 Release 监测结果评估下一份契约，先固定源码和差分，再决定项目版本线。
- **后续**：在不牺牲 512 MiB 目标的前提下改进可观测性、自动化升级和更多发行环境验证。

以下事项作为已接受限制或后续增强，不阻塞 `2.8.0`：

- `whole-host-512mib-runtime`、`arm64-production-runtime`、`native-systemd-install` 与 `native-openrc-install` 延后执行。
- 候选级 50,000 用户负载、24 小时 soak 以及故障/回滚注入作为扩展验证延后执行。
- 安装器不维护持久化阶段日志。被 `SIGKILL` 或掉电中断后重新运行安装器；容器部署则重新创建容器。
- OpenRC 正常执行 `stop_post` 时会清理专用 cgroup。`supervise-daemon` 异常退出后，通过重启主机或重新部署恢复。
- 只有出现实测需求时，才重新评估活动配置常驻副本与运行时 `dump-config` 的内存取舍。
- P3 测试补强：`runNode` 顶层失败收敛，以及 Unix server 活动 handler 取消。
- 首轮真实生产 soak 完成后，再根据实际变更压力，逐步从 `xray.Manager` 中拆分进程监管、运行时状态和版本跟踪职责；对外仍保留 Manager facade 和当前并发不变式。
- 已将实际职责是 rw-core gRPC wire adapter 的包规范为 `internal/xrayrpc`；只有在有真实解耦收益时才引入中性应用类型。

## 里程碑

### M0 - 自有项目基线

- 修正 module、仓库地址、版本和发布归属。
- 固定官方 Node 与 Panel 兼容版本。
- 建立路线、验收门槛和分支发布规则。

### M1 - 契约证据

- 固化 26 条路由及其 HTTP 方法。
- 将官方 Zod 请求和响应约束转为可执行测试数据。
- 覆盖合法、缺字段、错类型、未知类型、额外 JSON 和错误响应。
- 建立官方 Node 与 Go Node 的黑盒差分测试入口。
- 契约细节与已知偏差见 [`contract-2.8.0.md`](contract-2.8.0.md)。

### M2 - API 边界

- 引入统一严格 JSON 解码、DTO 校验和错误编码。
- 将 HTTP transport 与业务服务分离。
- 保证畸形请求不会调用 Xray、nftables、`ss` 或修改内存状态。

### M3 - Xray 生命周期

- 将启动、停止、健康检查和进程退出整理为显式状态机。
- 移除 `last-start.json` 和离线旧配置恢复。
- 修复并发启动、超时、取消、子进程回收和优雅退出。
- 保证 Panel 停用和 Node 重启语义与官方一致。

### M4 - 插件与 nftables

- 将同步改为 `plan -> apply -> commit`。
- 统一 nftables 初始化、可用性、错误传播、清理和幂等重试。
- 修复 ingress unblock、退出残留、ASN 缺失和 torrent 状态偏离。
- 对 nftables 使用 Linux network namespace 集成测试。

### M5 - 用户、连接与统计

- 修复用户热更新的校验与部分失败语义。
- 让连接踢除报告真实结果并保护特殊地址。
- 用固定 worker 或批量 RPC 替代无界 goroutine 与 N+1 放大。
- 为所有 gRPC 调用增加有界超时和取消传播。

### M6 - 512 MiB 资源优化

- 将 Xray 配置收敛为单份规范 JSON，避免 map、clone、JSON 和持久化多副本。
- 限制 zstd 解码内存、报告队列、临时切片和请求峰值。
- 评估使用最小 protobuf 客户端替代完整 Xray Go 实现依赖。
- 在 cgroup 限制下记录 idle、启动、同步和大用户集峰值。
- 50k 用户真实 rw-core 峰值为 `143.9 MiB`；完整预算和复现方式见 [`resource-budget.md`](resource-budget.md)。

### M7 - 系统与供应链

- 使用专用用户、最小 capability 和 systemd sandbox。
- 对齐 Debian/systemd 与 Alpine/OpenRC 的目录权限和生命周期。
- 所有 Release、rw-core、ASN 与辅助脚本都必须固定版本并校验摘要。
- 安装、升级、失败回滚和卸载不得影响不属于本项目的进程或 nftables 表。
- Ubuntu 24.04/systemd 与 Alpine 3.22/OpenRC 已通过真实的全新安装、重复安装、升级、错误 service 回滚、启停和隔离卸载测试。
- 两边的非 root 服务进程都只保留 `NET_ADMIN` 和 `NET_BIND_SERVICE` 的 effective 与 ambient capability。
- 固定的 rw-core、ASN 与 Release 归档都会在安装前校验。
- 故障注入测试覆盖写入后失败，以及 rw-core 资产和 Node 升级事务的逐文件摘要恢复。

### M8 - 发布验收

- 通过 `go test`、race、vet、静态检查、脚本检查和多架构构建。
- 先冻结代码候选，并将阻塞验收记录绑定到候选 commit 与候选镜像。
- 完成唯一阻塞的运行期 profile `docker-production-smoke-v2`：在实际记录的原生宿主机上，把 `amd64` 生产 Compose 部署固定到候选 manifest digest，严格使用 `448 MiB / 1 CPU / no container swap / 256 PIDs` 限制，接入真实 Panel 并通过真实代理流量，记录内存与 PID 使用量，确认容器持续运行且健康、OOM kill 与 restart 都为零。
- 将现有生命周期协调器、进程组清理、init、50,000 用户与回滚测试保留为代码级或带日期的工程证据，不把它们表述为当前候选运行期观测。
- 将 `whole-host-512mib-runtime`、`arm64-production-runtime`、`native-systemd-install`、`native-openrc-install`、`50000-user-load`、`24h-soak` 与 `fault-and-rollback-injection` 列为不阻塞发布的后续验证。
- 更新兼容矩阵、风险清单、运维文档和 `2.8.0` Release 资料。
- 按 [`release-acceptance.md`](release-acceptance.md) 校验发布记录与候选身份，随后只允许最终发布收尾变化。

## 开发与发布规则

- `main` 是受保护的发布分支，`dev` 是稳定开发与集成分支。
- 日常变更先进入 `dev`；发布候选通过 PR 从 `dev` 提升到 `main`。
- commit 只包含一个可说明、可验证的变化，不混入无关格式化。
- 提交前必须运行与改动风险匹配的测试；失败不得合入 `dev` 或 `main`。
- 正式 tag 使用 `vX.Y.Z` 或 `vX.Y.Z-rnl.N` 并与项目 `Version` 完全一致；已发布精确 tag 不得覆盖。
- 仓库不配置代码上游 remote；外部实现只作为协议或行为验证材料。
