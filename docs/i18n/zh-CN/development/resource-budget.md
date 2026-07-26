<!-- translation: locale=zh-CN; source=docs/development/resource-budget.md; source-sha256=058752507be2a502430a68c9eae4f66cc47892a3274a98ceec94a765c6d70ac5 -->
# 512 MiB 资源预算与工程基准

> 这是中文译文；资源数字和边界以[英文原文](../../../development/resource-budget.md)为准。

[返回开发文档](README.md) · [运维与排障](../operations.md)

本文汇总带日期的工程测量和当前资源策略。每项结果只适用于所列提交、测试日期、工具链、架构和测试资产；它们用于后续对比，不代表其它构建或环境。

## 生产边界

生产目标是整机 `512 MiB RAM / 1 vCPU / 2 GB disk`。Docker Compose 和 Native 服务都会为宿主机预留资源，把 Node 与 rw-core 合计限制为 `448 MiB` 内存，不允许服务或容器使用额外 swap，并限制为 `1 CPU` 和 `256` 个 PID/任务。容器的内存上限与内存加 swap 上限相同，因此即使宿主机有 swap，容器也不会获得额外 swap 配额。

发起 release 前，维护者会在这些限制下使用真实 Panel 和真实代理流量验证不可变的 `sha-<40-character-main-commit>` 镜像。该运维验证不存入仓库。带日期的 M6 工程门禁使用相同的核心 cgroup 限制完成了可复现的资源测试：

- `448 MiB` hard memory limit，为宿主机内核与基础服务保留至少 `64 MiB`。
- `1 CPU`、`256` 个 PID、禁用 swap 与外部网络。
- 只读 rootfs，并提供单个 `/tmp:size=64m` 测试 tmpfs。
- `LOW_MEMORY=1`，Go 运行时软内存上限为 `180 MiB`。
- 大配置包含 `50,000` 个 VLESS 用户。

历史门禁脚本为 [`scripts/test-low-memory.sh`](../../../../scripts/test-low-memory.sh)，Linux 集成测试为 [`internal/xray/resource_linux_integration_test.go`](../../../../internal/xray/resource_linux_integration_test.go)。M6 执行还通过最小 protobuf wire client 验证了系统统计、inbound 用户数、VLESS 热增删和用户 IP 统计 RPC。

生产 Compose 使用另一套 tmpfs 布局：`/run`、`/tmp` 和 rw-core 日志合计 `48 MiB`，日志不写入持久卷。历史门禁中的单个 64 MiB `/tmp` 只是测试夹具，并未逐项复现生产 Compose。历史负载或较大宿主机上的一次运行都不能单独证明精确整机目标。

2026-07-15 的 M6 数据和 2026-07-19 的 M7 init 快照仍是有价值的工程基线。改动可能显著影响资源预算时，应重新执行相关测量并对比结果。

## M6 固定测试资产（2026-07-15 工程基线）

- 日期：2026-07-15
- 容器架构：Linux arm64
- Go：`go1.26.5`
- Docker Engine：`29.5.2`
- rw-core：`v26.6.27`
- 官方资产：`Xray-linux-arm64-v8a.zip`
- 资产 SHA-256：`13a251379bea366c2cf10363ad71e75734193d401f26f518bf0c25e5c8f8c931`

执行命令：

```bash
scripts/test-low-memory.sh \
  --rw-core /path/to/rw-core-v26.6.27 \
  --users 50000 \
  --memory 448
```

## M6 实测结果

`cgroup_current` 和 `cgroup_peak` 包含 Node 测试进程、rw-core、文件页和容器开销；`node_test_rss` 只表示 Node 测试进程 RSS。因此 `cgroup_peak` 是本门禁的判定指标。

| 阶段 | cgroup current | cgroup peak | Node test RSS |
| --- | ---: | ---: | ---: |
| 空闲，core 未启动 | 40.3 MiB | 44.3 MiB | 11.1 MiB |
| 启动 1k 用户 | 50.2 MiB | 51.1 MiB | 13.2 MiB |
| 1k 配置无变化同步 | 50.2 MiB | 51.1 MiB | 13.4 MiB |
| 强制重启为 50k 用户 | 102.2 MiB | 143.9 MiB | 22.6 MiB |
| 50k 用户热增删与统计 | 102.3 MiB | 143.9 MiB | 22.6 MiB |

50k 用户场景峰值为预算的 `32.1%`，距离 `448 MiB` 门禁还有约 `304 MiB`。无变化同步没有抬高峰值，说明活动配置已按设计释放，运行时只保留哈希的状态模型确实生效。

## M6 二进制与磁盘

使用同一 Go 工具链和 `CGO_ENABLED=0 go build -trimpath -ldflags='-s -w'` 对比优化前工程基线：

| 架构 | 基线 | M6 | 减少 |
| --- | ---: | ---: | ---: |
| linux/arm64 | 17,563,810 B | 12,320,930 B | 29.9% |
| linux/amd64 | 18,874,530 B | 13,176,994 B | 30.2% |

## M7 Init 快照（2026-07-19 工程基线）

M7 增加了两份来自真实发行版布局的快照：

| 环境 | 运行内存 | 项目/整机磁盘 | 说明 |
| --- | ---: | ---: | --- |
| Ubuntu 24.04 arm64 / systemd | Node RSS `11.9 MiB` | 项目文件约 `74 MiB` | 全新安装，真实 rw-core/geo/ASN，core 尚未由 Panel 拉起 |
| Alpine 3.22 arm64 / OpenRC 容器（历史数据） | 整容器 `44.1 MiB` | 整个 rootfs `150.2 MiB` | 容器限制 `512 MiB / 1 CPU / 256 PIDs`，包含真实安装依赖与服务；不能作为受支持宿主机的资格证明 |

这份 Alpine 容器历史测量仍可用于比较资源形态，但不能证明发行版受到支持。当前 Alpine 支持要求持久化的 3.22.x `sys` 安装、作为 PID 1 运行的发行版 OpenRC，以及[测试指南](testing.md#native-发行版资格验证)规定的完整宿主机资格检查。

项目文件约有 `12 MiB` 属于 Node，`34 MiB` 属于 rw-core 和支持文件，另有 `28 MiB` 的 geo/ASN 资产。

两条 rw-core 日志流都使用有上限的 writer。每个当前文件及其 `.1` 文件的轮转阈值都是 `4 MiB`，因此两条日志流的稳定阈值预算合计 `16 MiB`。崩溃后，两个固定的 `.1.tmp` 文件还可能增加约 `8 MiB`。Docker 的 `28 MiB` 日志 tmpfs 正是按这个边界预留。

OpenRC 还会通过 supervisor 写入 `openrc.log` 和 `openrc.err.log`，每 10 秒检查并 copy-truncate。成功检查后，每个 `.1` 文件的阈值为 `4 MiB`；但当前文件可能在下一次轮询前继续增长，因此这不是严格的字节上限。四组当前文件加 `.1` 文件的阈值预算为 `32 MiB`。如果四个固定临时文件全部残留，总量约为 `48 MiB`，还要加上两个当前文件在一次轮询间隔内的额外增长。

在 systemd 247 或更新版本上，项目管理的加固 drop-in 每 30 秒最多接收 200 条服务日志；更早的 systemd 使用不含该指令的基础 unit。两种情况下，字节用量和长期增长都仍由宿主机的 journald 配额决定；速率限制并不是磁盘配额。

Native 生命周期状态和缓存归档存放在仅 root 可访问的 `/var/lib/remnanode-lite-installer`。生命周期和配置变更都会持有 `/run/remnanode-lite-installer/operation.lock`。generation 与服务生命周期事务还会由持久化的 `journal.json` 记录边界，因此这类切换在崩溃后会进入明确的 `recovery-required` 状态，而不是留下无法判断的半安装状态。`rnlctl repair` 会使用经过校验的缓存材料，恢复已提交的 generation 和预期的服务状态。配置和 Secret 变更采用原子文件操作及进程内恢复，不属于带 journal 的崩溃安全事务。

当前版本和上一版本都是位于 `/usr/local/lib/remnanode-lite/generations` 下的完整 generation，并保留各自对应的修复归档。新的 generation 提交后，比这两者更旧的 generation 及其缓存会被清理。这项设计以有界的磁盘开销换取一次本地回滚能力。磁盘特别小的主机应在升级前检查可用空间，并以 2 GB 整机目标为参考。

root 生命周期操作使用权限为 `0700` 的私有工作目录，并在操作结束后删除。运维人员提供的安全绝对路径 `TMPDIR` 优先使用，不安全的值会被忽略。否则，bootstrap 和生命周期控制器优先使用 `/var/lib/remnanode-lite-installer/tmp`，再回退到 `/var/tmp`。只有这两个常规位置都不可用时，Go 控制器才会为兼容性最后尝试平台临时目录。正常的 root 操作不会在小型主机可能位于内存中的 `/tmp` 解压临时内容。

bootstrap、生命周期引擎和 release 校验器都将 Native 归档限制为 `512 MiB`。校验最多允许 512 个归档条目、总计 `512 MiB` 的解压后载荷，以及单个不超过 `256 MiB` 的载荷。manifest 和生命周期状态另有各自的小型上限。归档会先复制到仅 root 可访问的私有工作目录再解析，调用方无法在安装期间替换已经检查过的路径。

Node、`rnlctl`、rw-core、GeoIP、GeoSite、ASN 数据、第三方声明、SBOM 和服务文件共同组成一个 bundle。Native 不存在独立的 core/data 更新器，也不提供自定义运行时资产 URL；磁盘峰值与回滚身份因此始终绑定到同一个 release generation。

生产 `node.env` 必须是普通的非符号链接文件。Go 在设置内存软上限前最多读取 `1 MiB`，并接受最多 `4096` 行和 `256` 个赋值。单行上限也是 `1 MiB`，因此可以迁移旧版最多 `256 KiB` 的内联 Secret。

`node.env` 与 `SECRET_KEY_FILE` 都只打开一次，并使用 `O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`。同一个文件描述符依次经过 `fstat -> 有界读取 -> fstat`，避免检查后打开的竞态和 FIFO 阻塞。systemd 与 OpenRC 都使用固定的 `REMNANODE_ENV=/etc/remnanode-lite/node.env` 和 `/usr/bin/env -i` 启动，只保留 `PATH/HOME/USER/LOGNAME`。`GOMEMLIMIT` 和 contract/core 版本覆盖值由同一个 Go 配置解析器校验并应用；Secret 和未知配置值不会进入 Node 或 rw-core 环境。

## 保护策略

- low-memory 默认请求体上限为 `16 MiB`，显式 `BODY_LIMIT_MB` 只能是 `1..1024`，`0/空` 表示自动默认。
- decoder 的压缩输入硬上限为 `64 MiB`，window 硬上限为 `32 MiB`；公开路由还会先取更小的逐路由上限，因此当前有效输入和 window 都不超过 `16 MiB`。最多两个单线程 decoder 并发。
- 单次 gRPC 响应最多 `16 MiB`，内部 RPC 具有 deadline。
- Unix 内部服务请求体最多 `8 KiB`，最多 `8` 个连接和 `4` 个活动 handler。
- 解码后的 webhook 使用 `64` 条有界队列和单 worker；队列满时最多等待内部请求的 `30s` deadline，容量未恢复、请求取消或服务关闭时明确返回 `503 + Retry-After`，不会把未接纳事件伪报为成功。
- torrent report 环形队列最多保留最新 `1024` 条。
- Xray ready 后释放解码配置树和规范 JSON，仅保留 hash 与运行状态。
- 受维护的 Docker 和 Native 模板默认设置 `LOW_MEMORY=1`。
- Alpine/OpenRC 会校验 cgroup v2 的 `448 MiB` memory、零 swap、1 CPU、256 PID 以及启动 shell 的实际 cgroup 成员关系；同时要求可用的 `cpu`、`memory`、`pids` controller、`memory.swap.max`、可写的父级 `cgroup.procs` 与服务 `cgroup.kill`。控制项缺失或写入未生效时拒绝启动。停止后不依赖 OpenRC 0.62.6 的路径清理，而是将 `stop_post` 自身迁出、通过 `cgroup.kill` 清理精确 service cgroup、最多等待 5 秒确认 `populated=0` 后删除该目录。

上述 Alpine/OpenRC 清理适用于 init 正常执行 `stop_post` 的停止路径。生命周期 journal 会记录宿主机文件和服务意图，但无法让异常退出的 `supervise-daemon` 自动移除残留的内核 cgroup。此时应先停止服务的残留进程或重启主机；如果生命周期状态报告操作曾被中断，再运行 `rnlctl repair`。

任何修改请求解码、Xray 配置生命周期、RPC 消息、报告队列或依赖图的提交，都应重新执行该工程门禁并比较阶段峰值。该比较是维护约束，不是发布资料。

## 关闭预算

| 层级 | 上限 | 语义 |
| --- | ---: | --- |
| Node 整体 | `25s` | 所有应用清理共享同一个 deadline，不是每项各 25 秒 |
| rw-core | `5s + 5s` | 对独立进程组先发 SIGINT，未退出再发 SIGKILL；整组清理成功后才删除插件 nft 表 |
| Plugin Close | `min(剩余预算, 15s)` | gate、nft 子命令和 worker join 共用剩余时间 |
| Unix server | `5s` | 收到根 context 取消后关闭，失败则 force close |
| HTTPS server | 整体剩余预算 | deadline 后 force close |
| systemd | `30s` | `TimeoutStopSec`，为 25 秒应用预算保留约 5 秒外层余量 |
| Alpine/OpenRC | `TERM/30/KILL/5` | supervise-daemon 的外层兜底 |

整体 deadline 到期会返回聚合错误；外层 service manager 随后可以强杀，不能据此声称所有故障路径都在 25 秒内优雅完成。

core 或插件清理若快速返回瞬时错误，会等待 `100ms` 后在同一 deadline 内重试一次，重试不会创建新的 25 秒预算。公开 `xray/stop` 同样先确认 core 停止，再删除插件规则，避免运行中的 core 出现无过滤窗口。

`plugin sync/recreate` 与 `xray start/stop` 共用应用层 lifecycle gate。锁顺序固定为 `lifecycle gate -> plugin operation gate -> Manager`，不会在 core 配置启动期间提交不一致的插件快照。
