<!-- translation: locale=zh-CN; source=docs/archive/2026-07-audit-remediation.md; source-sha256=01399e82b44f39aac83f79f2ced0aa0bc7700dbafabeb880add3165eb218f97c -->
# 2026-07 静态审计整改记录（归档）

> 这是中文译文；涉及历史记录和规则时，请以[英文原文](../../../archive/2026-07-audit-remediation.md)为准。

[返回开发文档](../development/README.md) · [当前路线图](../development/roadmap.md) · [架构说明](../architecture.md)

这里记录的是 2026 年 7 月为首个独立版本线完成的静态审计和整改工作，仅供回顾，不是当前待办或新维护者指南。审计早于仓库历史重置，原基线 commit 已不存在于当前 Git 历史。仍需追踪的事实可以在代码和测试、[2.8.0 契约基线](../development/contract-2.8.0.md)、[资源预算](../development/resource-budget.md)和当前[发布流程](../release.md)中找到。

下文的 M8 计划原本要求原生和双运行时测试、50,000 用户测试及 24 小时 soak。仓库后来不再把这些运行观测作为版本化源码资料。当前流程为每个 `main` 提交构建一个不可变的 `sha-<40位提交>` 候选；维护者用真实 Panel 和流量确认候选后，当前 release workflow 会校验 draft Release，在 `main` 公开其 tag、校验已接受资产、把同一 digest 晋升为精确版本与 `latest`，并公开 GitHub 自动生成的 Release notes。当前发布规则以发布手册为准；本文只记录历史计划。

本记录形成时，M0-M7 整改已经完成，M8 规划了候选的真实 Panel/Linux、资源、恢复和
长期运行测试。掉电或 installer/supervisor 被强制终止后的自动恢复不在范围内；操作人员
应根据情况重新运行 installer、重启主机或重建容器。

## 工程原则

- 官方实现决定兼容目标，不决定本项目的内部架构。不要照搬 fail-open、吞掉错误、资源
  无上限或无法重试的行为。
- 每个进程、生命周期、规则集和状态域都只有一个所有者。Manager 与 Plugin 分别管理
  内部状态，HTTP lifecycle coordinator 负责跨边界操作的顺序。
- 进程成功启动后，必须先登记所有权再返回。只有确认 leader 和后代都已退出，才能报告
  `stopped` 或移除防火墙规则。
- 每次 mutation 都绑定一个 rw-core 进程身份，其 RPC 操作和本地 hash/state commit 在
  同一个 process lease 内完成。
- 防火墙更新采用 fail-closed 的 plan/apply/reconcile/commit 流程。失败必须可见，在承诺
  回滚的地方恢复旧状态，并且可以安全重试。
- 所有来自 HTTP、Panel、插件配置、rw-core webhook、文件和外部命令的数据都必须有字节、
  条目、深度、并发、时间和输出边界。错误与日志本身也属于资源预算。
- `512 MiB RAM / 1 vCPU / 2 GiB disk` 是整机目标。Go runtime 的内存软限制只是预算的
  一部分，还要计算 rw-core、cgroup、nftables 状态、页缓存、安装文件和日志。
- 兼容性 oracle 必须独立于 Go 实现生成。拿实现常量与同一份 schema 的手写副本比较，
  不能证明兼容性。

## 已执行的整改阶段

1. **输入与部署安全**：移除 OpenRC 的 root shell source，校验 Secret，并限制请求数组、
   validation issue、JSON 深度、插件展开和日志字段。
2. **Xray 生命周期**：原子登记进程所有权，要求完整清理后代，并加入 operation/process
   lease。HTTP coordinator 允许 start 共享访问，而 stop 与 plugin mutation 保持独占和
   writer-preferred。
3. **Plugin 与连接正确性**：让过载可见、nft 删除可幂等重试、动态 block 可以保留，并让
   cleanup 先处理 core、socket drop 可以重试。torrent rule 和 `recreate-tables` 只更新
   真正需要变化的部分。
4. **官方行为对齐**：修复 stats reset、protobuf encoding、HTTP parsing、响应模型、系统
   信息、JWT 和 Unix config target 等已确认差异。
5. **资源边界**：补齐 OpenRC cgroup、低内存升级迁移、磁盘和归档预检、可靠停止与回滚，
   以及进程身份健康检查。
6. **可信测试链**：从固定的官方源码和 SDK 证据生成静态契约，并通过可执行检查验证代码和产物。
7. **发布准备**：打 tag 前运行测试、race detection、vet、static analysis 和双架构构建。原计划随后执行 systemd、OpenRC、Panel 2.8.1、真实 rw-core、50,000 用户和 24 小时 soak。

## 当时采用的提交策略

- 以可回归的阶段批次提交相关实现、测试和文档，避免把同一问题拆成大量微小 commit。
- 架构迁移先提交所有者与接口，再提交调用方，禁止长期保留双重真相源。
- 当前批次全部修改完成后再集中运行完整门禁；只有真实失败并修复后才重复一次。
- 官方契约或明确代码 bug 的 P1/P2 立即加入当前阶段；发布和运维后置项单独记录。
