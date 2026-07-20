<!-- translation: locale=zh-CN; source=docs/development/resource-budget.md; source-sha256=1f17183c5e55ab79b185a9a9e82f0e2d7acd66101271492190df766d4cef9a33 -->
# 512 MiB 资源预算与 M6-M8 基准

> 这是中文译文；资源数字和边界以[英文原文](../../../development/resource-budget.md)为准。

[返回开发文档](README.md) · [运维与排障](../operations.md)

本文汇总带日期的工程测量和当前资源策略。每项结果只适用于所列提交、测试日期、工具链、架构和测试资产；它们用于提供工程背景，不会自动成为其他发布候选的运行时证据。

## 验收边界

`v2.8.0` 唯一会阻塞发布的资源与运行时验收方案，是冻结候选上的 `docker-production-smoke-v1`。它要求在 `amd64` 上以镜像摘要固定生产 Compose 部署，接入真实 Panel、通过真实代理流量，并记录实际内存和 PID 用量；容器必须持续健康运行，且 OOM kill 和重启次数均为零。[验收协议](release-acceptance.md#docker-生产-smoke)定义完整条件，其中包括在指定宿主、容器、健康和就绪限制下至少运行 600 秒。

`arm64-production-runtime`、`native-systemd-install`、`native-openrc-install`、`50000-user-load`、`24h-soak` 和 `fault-and-rollback-injection` 均作为不阻塞发布的后续 profile 延后执行。未在当前候选上运行这些 profile 时，不得暗示已经取得相应结果。

生产目标仍是整机 `512 MiB RAM / 1 vCPU / 2 GB disk`。带日期的 M6 工程门禁曾将 Node 测试进程与真实 rw-core 放在同一个 cgroup 内，并使用以下限制：

- `448 MiB` hard memory limit，为宿主机内核与基础服务保留至少 `64 MiB`。
- `1 CPU`、`256` 个 PID、禁用 swap 与外部网络。
- 只读 rootfs，并提供单个 `/tmp:size=64m` 测试 tmpfs。
- `LOW_MEMORY=1`，Go 运行时软内存上限为 `180 MiB`。
- 大配置包含 `50,000` 个 VLESS 用户。

历史门禁脚本为 [`scripts/test-low-memory.sh`](../../../../scripts/test-low-memory.sh)，Linux 集成测试为 [`internal/xray/resource_linux_integration_test.go`](../../../../internal/xray/resource_linux_integration_test.go)。M6 执行还通过最小 protobuf wire client 验证了系统统计、inbound 用户数、VLESS 热增删和用户 IP 统计 RPC。

生产 Compose 使用另一套 tmpfs 布局：`/run`、`/tmp` 和 rw-core 日志合计 `48 MiB`，日志不写入持久卷。历史门禁中的单个 64 MiB `/tmp` 只是测试夹具，并未逐项复现生产 Compose。阻塞发布的 `amd64` smoke 使用生产布局，但不重复历史 50,000 用户负载。

2026-07-15 的 M6 数据和 2026-07-19 的 M7 init 快照都早于当前 M8 候选。它们仍是有价值的工程基线，但不是当前候选的运行时证据，也不需要为发布 `2.8.0` 重新执行。

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

M7 使用最终安装布局补充了两类真实发行环境快照：

| 环境 | 运行内存 | 项目/整机磁盘 | 说明 |
| --- | ---: | ---: | --- |
| Ubuntu 24.04 arm64 / systemd | Node RSS `11.9 MiB` | 项目文件约 `74 MiB` | 全新安装，真实 rw-core/geo/ASN，core 尚未由 Panel 拉起 |
| Alpine 3.22 arm64 / OpenRC 容器 | 整容器 `44.1 MiB` | 整个 rootfs `150.2 MiB` | 容器限制 `512 MiB / 1 CPU / 256 PIDs`，真实安装依赖与服务 |

项目文件约有 `12 MiB` 属于 Node，`34 MiB` 属于 rw-core 和支持文件，另有 `28 MiB` 的 geo/ASN 资产。

两条 rw-core 日志流都使用有上限的 writer。每个当前文件及其 `.1` 文件的轮转阈值都是 `4 MiB`，因此两条日志流的稳定阈值预算合计 `16 MiB`。崩溃后，两个固定的 `.1.tmp` 文件还可能增加约 `8 MiB`。Docker 的 `28 MiB` 日志 tmpfs 正是按这个边界预留。

OpenRC 还会通过 supervisor 写入 `openrc.log` 和 `openrc.err.log`，每 10 秒检查并 copy-truncate。成功检查后，每个 `.1` 文件的阈值为 `4 MiB`；但当前文件可能在下一次轮询前继续增长，因此这不是严格的字节上限。四组当前文件加 `.1` 文件的阈值预算为 `32 MiB`。如果四个固定临时文件全部残留，总量约为 `48 MiB`，还要加上两个当前文件在一次轮询间隔内的额外增长。

systemd journal 每 30 秒最多接收 200 条服务日志，但字节用量和长期增长仍由宿主机 journald 配额决定。后续扩展验证应在 `2 GB` 整机磁盘上测量日志故障风暴和长期增长。这项工作已在 `2.8.0` 延期，上述阈值不能代替它的结果。

安装和升级把大资产放在仅 root 可访问的 `/var/lib/remnanode-installer`，不使用可能映射到内存的 `/tmp`。五个变更入口都持有 `/run/lock/remnanode-installer.lock`。嵌套安装器会复用并验证同一个打开的文件描述，`RNL_TMP_ROOT` 不影响锁路径，也没有退出路径会删除锁 inode。

修改包、文件或服务的同步子进程会继承这把锁。即使父安装器意外退出，其他变更也会等当前操作结束。下载、归档检查、Node/rw-core 自检、状态查询和 OpenRC 启动链则会先关闭自己的锁描述符，避免短命工具或常驻 supervisor 在安装器完成后继续持锁。

Release 归档的上限是 `64 MiB` 压缩体积、`128 MiB` 解压体积和 `64` 个条目。rw-core zip、自定义 core、geo 与 ASN 都有各自的下载和流式解压硬上限。`GEO_ZAPRET_FILE` 与 `IP_ZAPRET_FILE` 本地输入各限制为 `64 MiB`，并在目标目录中原子暂存。单次下载最长 `300s`，另有连接和低速超时；tar 与 unzip 操作最长 `120s`。

升级首先为“现有备份 + `512 MiB`”预留空间。rw-core 下载通过 zip 结构检查后，再分别计算 installer、core、geo 与 ASN 所在文件系统的需求，包括真实归档条目、可选 custom core/ASN、备份、目标暂存和每个文件系统 `64 MiB` 的安全余量。

upgrade 调用 rw-core 安装器时，外层事务是唯一的备份所有者，不会再复制一份相同资产。独立安装器无法完整回滚时，会保留仅 root 可访问的事务目录并返回失败，而不会删除唯一备份。

生产 `node.env` 必须是普通的非符号链接文件。Go 在设置内存软上限前最多读取 `1 MiB`，并接受最多 `4096` 行和 `256` 个赋值。单行上限也是 `1 MiB`，因此可以迁移旧版最多 `256 KiB` 的内联 Secret。

`node.env` 与 `SECRET_KEY_FILE` 都只打开一次，并使用 `O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`。同一个文件描述符依次经过 `fstat -> 有界读取 -> fstat`，避免检查后打开的竞态和 FIFO 阻塞。systemd 与 OpenRC 都使用固定的 `REMNANODE_ENV=/etc/remnanode/node.env` 和 `/usr/bin/env -i` 启动，只保留 `PATH/HOME/USER/LOGNAME`。`GOMEMLIMIT` 和 contract/core 版本覆盖值由同一个 Go 配置解析器校验并应用；Secret 和未知配置值不会进入 Node 或 rw-core 环境。

## 保护策略

- low-memory 默认请求体上限为 `16 MiB`，显式 `BODY_LIMIT_MB` 只能是 `1..1024`，`0/空` 表示自动默认。
- decoder 的压缩输入硬上限为 `64 MiB`，window 硬上限为 `32 MiB`；公开路由还会先取更小的逐路由上限，因此当前有效输入和 window 都不超过 `16 MiB`。最多两个单线程 decoder 并发。
- 单次 gRPC 响应最多 `16 MiB`，内部 RPC 具有 deadline。
- Unix 内部服务请求体最多 `8 KiB`，最多 `8` 个连接和 `4` 个活动 handler。
- 解码后的 webhook 使用 `64` 条有界队列和单 worker；队列满时最多等待内部请求的 `30s` deadline，容量未恢复、请求取消或服务关闭时明确返回 `503 + Retry-After`，不会把未接纳事件伪报为成功。
- torrent report 环形队列最多保留最新 `1024` 条。
- Xray ready 后释放解码配置树和规范 JSON，仅保留 hash 与运行状态。
- Debian 与 Alpine 安装器在 `MemTotal <= 512 MiB` 时自动写入 `LOW_MEMORY=1`。
- OpenRC 校验 cgroup v2 的 `448 MiB` memory、零 swap、1 CPU、256 PID 以及启动 shell 的实际 cgroup 成员关系；controller 缺失或写入未生效时拒绝启动。停止后不依赖 OpenRC 0.62.6 的路径清理，而是将 `stop_post` 自身迁出、通过 `cgroup.kill` 清理精确 service cgroup、最多等待 5 秒确认 `populated=0` 后删除该目录。

上述 OpenRC 清理覆盖 init 正常执行 `stop_post` 的停止路径。安装器共享锁可以避免并发写入，但不提供应对 SIGKILL 或掉电的持久化阶段日志（phase journal）；`supervise-daemon` 异常退出后，项目也不承诺自动清理残留 cgroup。这是 `2.8.0` 接受的运维限制：原生部署重新运行安装器或重启主机，容器部署重新创建容器。它们不阻塞发布。

任何修改请求解码、Xray 配置生命周期、RPC 消息、报告队列或依赖图的提交，都应重新执行该工程门禁并比较阶段峰值。该比较是独立于当前 M8 阻塞 profile 的维护约束。

## 关闭预算

| 层级 | 上限 | 语义 |
| --- | ---: | --- |
| Node 整体 | `25s` | 所有应用清理共享同一个 deadline，不是每项各 25 秒 |
| rw-core | `5s + 5s` | 对独立进程组先发 SIGINT，未退出再发 SIGKILL；整组清理成功后才删除插件 nft 表 |
| Plugin Close | `min(剩余预算, 15s)` | gate、nft 子命令和 worker join 共用剩余时间 |
| Unix server | `5s` | 收到根 context 取消后关闭，失败则 force close |
| HTTPS server | 整体剩余预算 | deadline 后 force close |
| systemd | `30s` | `TimeoutStopSec`，为 25 秒应用预算保留约 5 秒外层余量 |
| OpenRC | `TERM/30/KILL/5` | supervise-daemon 的外层兜底 |

整体 deadline 到期会返回聚合错误；外层 service manager 随后可以强杀，不能据此声称所有故障路径都在 25 秒内优雅完成。

core 或插件清理若快速返回瞬时错误，会等待 `100ms` 后在同一 deadline 内重试一次，重试不会创建新的 25 秒预算。公开 `xray/stop` 同样先确认 core 停止，再删除插件规则，避免运行中的 core 出现无过滤窗口。

`plugin sync/recreate` 与 `xray start/stop` 共用应用层 lifecycle gate。锁顺序固定为 `lifecycle gate -> plugin operation gate -> Manager`，不会在 core 配置启动期间提交不一致的插件快照。
