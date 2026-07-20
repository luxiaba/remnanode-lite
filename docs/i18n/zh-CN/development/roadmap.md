<!-- translation: locale=zh-CN; source=docs/development/roadmap.md; source-sha256=aa527bac062e638dbb17cd0cfb74d1b0ba3d82eb14bff72b54fad6e01a73e866 -->
# Remnanode Lite 路线图

> **翻译说明：** [英文原文](../../../development/roadmap.md)是唯一权威来源；本页用于中文阅读，并应随英文源同步。

[返回开发文档](README.md) · [项目说明](../project.md) · [版本模型](../versioning.md)

## 项目目标

本仓库维护独立 Go 实现、代码基线和发布历史。官方 `remnawave/node` 只作为行为与契约兼容参考，不是 Git 上游。长期目标、受众和非目标以[项目说明](../project.md)为准；本页只记录阶段状态和后续方向。

首个版本线从 `2.8.0-rnl.1` 开始，目标如下：

- 对官方 Node `2.8.0@596f015` 达到行为级兼容。
- 与 Panel `2.8.1` 完成真实集成验证。
- 修复已知生命周期、插件、防火墙、契约和安装供应链问题。
- 在 `512 MiB RAM / 1 vCPU / 2 GB disk` 的 Linux 节点稳定运行。
- 支持 Linux `amd64` 与 `arm64`。
- Debian/systemd 为主验收环境，Alpine/OpenRC 为第二验收环境。

项目版本与官方契约版本彼此独立。`X.Y.Z-rnl.N` 是本项目自己的迭代标识，可以提前开发下一条版本线，也可以继续完善某个官方版本；纯 `X.Y.Z` 只有完成对应官方契约对齐后才能发布。官方发布监测只创建同步 Issue，不会自动修改契约或发布。完整规则见[版本模型](../versioning.md)。

## 设计原则

1. 官方 Contract 和可观测行为是兼容依据，官方 TypeScript 架构不是照搬对象。
2. 所有请求必须在产生副作用前完成完整校验。
3. 外部副作用必须通过可替换接口执行，并返回可传播的错误。
4. 状态只在外部操作成功后提交；失败必须允许同一请求安全重试。
5. 所有并发、队列、请求体和缓存都必须有明确上限。
6. Node 只拥有自己的 rw-core 进程、内部 socket 和 nftables 私有表，不接管宿主机整体防火墙策略；按 IP 执行的 socket destroy 是明确记录的宿主 network namespace 副作用。
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

M6/M7 的资源与发行环境数据是工程基线，不是当前 M8 候选的发布证据；冻结候选 `C` 后必须按验收协议重新执行。

当前静态实现、CI、候选镜像和代码级 512 MiB 约束已经落地。正式版本 tag、冻结候选上的完整 Panel/systemd/OpenRC、双架构、资源故障和长期 soak 证据仍在 M8 完成。`main` 的 `sha-*` 镜像可用于验收，但不等于正式 Release。

## 当前重点

- **Now**：完成首个正式候选的 M8 真实验收，补齐经过脱敏和摘要绑定的证据。
- **Next**：根据官方 Release 监测结果评估下一份契约，先固定源码和差分，再决定项目版本线。
- **Later**：在不牺牲 512 MiB 目标的前提下改进可观测性、自动化升级和更多发行环境验证。

以下事项作为已接受限制或后续增强，不阻塞 `2.8.0-rnl.1`：

- installer 不实现持久 phase journal；被 `SIGKILL` 或掉电中断后重新运行 installer，容器部署则重新创建容器；
- OpenRC 正常 `stop_post` 继续清理专用 cgroup；`supervise-daemon` 自身异常退出后通过重启主机或重新部署恢复；
- active config 常驻副本与运行期 `dump-config` 的内存取舍；
- P3 测试补强：`runNode` 顶层失败收敛，以及 Unix server 活动 handler 取消。
- 首轮真实生产 soak 后，再按实际变更压力从 `xray.Manager` 内部渐进提取 process supervisor、runtime state 和 version tracker，外部保留 Manager facade 与当前并发不变式；
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
- Ubuntu 24.04/systemd 与 Alpine 3.22/OpenRC 已真实通过安装、重复安装、成功升级、坏 service 回滚、启停与卸载隔离；两边的非 root 服务进程均只保留 `NET_ADMIN`/`NET_BIND_SERVICE` effective 与 ambient capability。
- 固定 rw-core、ASN 与 Release 归档均在写盘前校验 SHA-256；rw-core 资产组和 Node 升级事务均通过写入后故障注入与逐文件摘要恢复测试。

### M8 - 发布验收

- 完成真实 rw-core、Panel、nftables、systemd/OpenRC 集成测试。
- 在冻结候选上复核 `xray start/stop` 与 `plugin sync/recreate` 的 shared-start/exclusive-mutation coordinator、固定锁序和取消传播。
- 在 systemd/OpenRC 中验证 rw-core 独立进程组、正常停止、超时升级和 leader 自然退出后的后代清理；不要求 Node 或 supervisor 自身被强杀后的自动恢复。
- 通过 `go test`、race、vet、静态检查、脚本检查和多架构构建。
- 在目标资源限制下完成持续运行与故障恢复测试。
- 更新兼容矩阵、风险清单、运维文档和 `2.8.0-rnl.1` Release 资料。
- 先冻结代码候选 commit；全部验收记录绑定该 commit，之后只允许发布文档白名单变化。
- 使用严格 JSON、文件摘要和 Git ancestry 校验 M8 证据；协议见 [`release-acceptance.md`](release-acceptance.md)。

## 开发与发布规则

- `main` 是受保护的发布分支，`dev` 是稳定开发与集成分支。
- 日常变更先进入 `dev`；发布候选通过 PR 从 `dev` 提升到 `main`。
- commit 只包含一个可说明、可验证的变化，不混入无关格式化。
- 提交前必须运行与改动风险匹配的测试；失败不得合入 `dev` 或 `main`。
- 正式 tag 使用 `vX.Y.Z` 或 `vX.Y.Z-rnl.N` 并与项目 `Version` 完全一致；已发布精确 tag 不得覆盖。
- 仓库不配置代码上游 remote；外部实现只作为协议或行为验证材料。
