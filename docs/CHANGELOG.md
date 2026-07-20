# 变更日志

[返回文档首页](README.md)

格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。
仅记录面向用户/运维的 notable 变更；完整 diff 见 GitHub Releases。

## [2.8.0-rnl.1] - Unreleased

这是 `Luxiaba/remnanode-lite` 的首个自有版本线，兼容目标固定为官方 Node 2.8.0 与 Panel 2.8.1。

首个条目同时保存接管和架构整改摘要，因此比后续版本更详细；未来条目只记录用户、运维或维护者需要感知的变化。

### 新增

- 将开发门禁拆为可并行诊断的 Go、仓库、离线 installer 与 Linux 网络管理任务，由稳定的 `ci / gate` 汇总；漏洞扫描改为独立定时任务，所有 GitHub runner 固定为 Ubuntu 24.04。
- 新增 GHCR 多架构镜像发布链：`main` 先构建无业务 tag 的 manifest，生成 SBOM、BuildKit provenance 与 GitHub build attestation，再发布不可移动的 `sha-*` 候选和浮动 `edge`；量产单文件 Compose 变更同样触发候选构建。tag Release 直接验证 acceptance digest 及其源码 attestation，不依赖自动或手动候选 tag 的命名，再把同一镜像晋升为精确版本与 `latest`，全程不重新构建；`dev`/PR 独立验证容器构建。
- 新增官方 Node Release 定时监测；发现兼容基线变化时创建同步 Issue，但不会自动修改代码或发布镜像。
- 新增 amd64/arm64 多阶段 Docker 镜像与生产 Compose：固定并校验 rw-core/geo/ASN 资产，采用官方 host network 与能力模型，同时落实 448 MiB/no-swap/1 CPU/256 PID、只读 rootfs、健康检查和日志上限。
- 容器部署不再创建持久日志卷；rw-core 日志使用有界 tmpfs，Docker 日志严格轮转，容器重建即可回收全部运行日志。
- 固化官方 Node `2.8.0@596f015` 的 26 条路由、Zod 请求/响应、错误格式和副作用为可执行契约。
- 新增默认只读、需 mTLS/JWT 的 `contract-probe`，用于官方 Node 与 Go Node 的黑盒语义差分。
- 新增统一 Node API 边界，覆盖 Zod 等价的必填字段、联合类型、UUID/IP、枚举、nullable/default 和数组长度校验。
- 新增 Linux network namespace nftables 与 socket-kill 集成门禁，真实覆盖双栈规则替换、封禁、解封、重建、退出清理和 TCP 连接关闭。
- 新增固定 `ipverse/as-ip-blocks` commit 与归档摘要的流式 ASN 构建链，Release 同时发布 compact `asn-prefixes.bin` 与 `SHA256SUMS`。
- 新增 `448 MiB / 1 CPU / no-swap` 真实 rw-core 资源门禁；M6 工程基线的 50k 用户场景峰值为 `143.9 MiB`，M8 冻结候选仍须重跑。
- M8 发布证据新增 Compose 实测记录，绑定候选 manifest digest 与候选 Git object 中的部署模板；amd64/arm64 必须各自通过整机资源、cgroup、init/reaping、capability、tmpfs、health、优雅停止、zombie、日志轮转、磁盘余量和实际回滚镜像启动门禁。

### 安全

- rw-core 子进程环境会剥离 Panel Secret、Secret 文件路径、Node 配置路径和调用方提供的内部 token，只重新注入运行所需的资源路径与受控内部 webhook token；该 token 默认每次启动随机生成。
- JWT header 与 claims 必须各自只包含一个完整 JSON 值；签名有效但附带第二个 JSON 值的畸形 token 不再被接受。
- 外部传输最低版本收敛为 TLS 1.3，并禁用 HTTP/2；无效 JWT、未知路由和错误 method 与官方一致地直接销毁连接。
- systemd/OpenRC 改用专用 `remnanode` 用户，只保留 `CAP_NET_ADMIN` 与 `CAP_NET_BIND_SERVICE`；systemd 同时启用 capability bounding、sandbox、448 MiB/no-swap/1 CPU/256 tasks 限额。
- Release archive、rw-core、自定义 core 与 ASN 资产均在写盘前校验 SHA-256、结构和版本；固定 rw-core 摘要不可覆盖，GitHub Actions 固定到完整 commit SHA。
- systemd/OpenRC 通过空环境启动，`node.env` 与 Secret 均由 Go 使用 `O_NOFOLLOW|O_NONBLOCK` 的同一文件描述符有界读取；符号链接、FIFO、device、超限或读取期间变化会在启动前失败。
- 安装器拒绝受管路径中的不安全 owner、权限、符号链接和硬链接；日志 helper、rw-core、geo 与 ASN 使用同目录 staging 原子替换，service 更新则由外层升级事务备份和验证。
- 安装、升级、rw-core 安装与卸载共用固定内核锁；嵌套入口复用同一锁 FD。同步包管理、文件和 service mutation 持锁到子进程结束；下载、Node/rw-core 自检、状态查询和可能派生常驻服务的 OpenRC 启动链不继承该 FD。Alpine 入口显式依赖 `util-linux`。

### 修复

- `NODE_PORT` 现在在读取配置时统一拒绝 `0`、负数和大于 65535 的值，不再让直接运行路径意外绑定随机端口。
- `release-url` 与 `install-script-url` 会校验正式 tag、目标架构和脚本 allowlist，拒绝路径型或未知输入。
- 路由测试改为校验真实 dispatcher 注册表；`/node/xray/stop` 收敛为官方定义的仅 GET，不再错误接受 POST。
- stats、handler、plugin 与 Xray start 不再吞掉 JSON 解码和类型错误；畸形、尾随或不完整请求会在任何 provider、进程、nftables、连接和状态副作用前返回 400。
- 已知应用错误补齐官方要求的 `timestamp`、`path`、`message` 与 `errorCode`，底层 SDK 错误不再替换官方 A001/A010-A017 文案。
- 对齐官方边界细节：未知对象字段剥离、`forceRestart` 默认 false、空字符串与无最小长度数组、五种用户联合类型、数值型 nftables timeout。
- Xray 启动、停止、健康检查和自然退出改为显式四态生命周期；stop 可取消正在启动的 core，失败/超时不再提交配置或 hash，所有子进程均被回收。
- 移除非官方的 `last-start.json` 持久化与开机旧配置恢复；Node 重启后由 Panel 健康检查重新下发 start，`healthcheck` 只读缓存状态。
- Panel stop 固定先确认 core 停止再清理插件；停止失败时保留插件快照与 nft 规则，避免运行中的 core 出现无过滤窗口。
- Linux 将 rw-core 置于独立进程组，SIGINT、超时 SIGKILL 和 leader 自然退出后的兜底清理覆盖整个进程组；parent-death signal 保护直接子进程，Node 或 supervisor 自身被强杀后通过重启或重新部署恢复。
- 插件同步改为不可变 plan 的 `apply -> Xray reconcile -> commit` 事务；nft/Xray 失败不再提前提交状态，并会尝试恢复上一份 firewall plan。`plugin sync/recreate` 与 `xray start/stop` 共用应用层 lifecycle gate，消除 core 启动配置与插件快照竞态。
- nftables 初始化、双栈批处理、ingress/torrent 解封、recreate 重放、错误传播和退出清表统一收口；缺失元素的多种 nft 错误文案均按幂等成功处理。
- nft 不可用时合法配置仍按官方语义接受，但 torrent effective state 保持禁用；reset 不再丢弃未 collect reports，ASN/shared list 降级会写入明确日志。
- listener 异常不再从 goroutine 调用 `log.Fatalf` 跳过清理；统一关闭路径先停止 rw-core，再删除本项目 nftables 表。
- 用户热更新改为可取消的串行 mutation；整个请求持有绑定具体 rw-core 的 process lease，RPC、连接清理和 inbound hash 提交不会跨进程，清理失败不再继续添加该用户，批量部分失败会返回真实错误并保持可重试。
- 连接踢除会规范化并去重 IP，保护非法、特殊、本机和白名单地址；缺少 capability、IP 查询失败或任一 `NETLINK_SOCK_DIAG` socket destroy 失败不再伪报成功。
- `get-users-ip-list` 优先使用单次批量 RPC；旧 core 只在 `UNIMPLEMENTED` 时降级到最多 8 个固定 worker，并缓存 capability，消除 N+1 无界 goroutine。
- 所有内部 Handler/Stats unary gRPC 调用增加取消传播和有界 deadline；默认 5 秒，健康探测 3 秒，批量 legacy 查询共享总预算。
- Xray webhook 改为 64 条有界等待队列和单 worker；容量超时、取消或关闭会明确返回 503，插件关闭使用不可逆 admission fence，超时或 nft 清理失败后拒绝新 mutation 并允许 Close 重试。
- 整机退出改为共享 25 秒预算；后台版本探测可取消并等待，rw-core 确认停止后才清理 nft 表，避免独立 timeout 累加越过 service manager 的 TERM grace。
- 用户 mutation 的 panic 不再静默降级：客户端继续收到官方 A001，受限日志记录 operation、panic 类型、有界 value 和 stack；process lease 与 mutation gate 仍保证释放，后续请求可继续执行。
- 公开 `xray/stop` 串行化 start/stop，并只在 core 停止成功后 reset 插件；停止失败不再提前撤销 nft 过滤。
- 重复执行安装脚本会进入同一可回滚升级事务；坏 systemd/OpenRC service、binary/support/node.env/rw-core 写入失败均恢复升级前文件、开机注册和运行状态，恢复不完整时保留唯一备份并明确失败。
- rw-core 安装按 installer、core、geo 与 ASN 的实际目标文件系统分别聚合 staging/备份峰值；任一挂载空间不足会在替换资产前失败。
- CLI 只有零参数会进入 daemon；未知或多余参数直接失败。Unix socket 启动拒绝 live、symlink 与非 socket 路径，退出时只删除当前实例实际拥有的 socket。
- 卸载不再按进程名终止任意 `rw-core`，也不再删除通用 Xray 路径，只清理本项目私有进程、socket、nftables 表与 `/usr/local/{lib,share}/remnanode`。
- 非交互安装未提供 Secret Key 时会完成落盘但保持服务停止，不再错误等待未启动服务的端口。
- 所有安装/升级包装入口的 `--dry-run` 保持零写入；路径型 Release tag 在 bootstrap 和事务开始前拒绝，service/core 始终取自目标 Release 的已校验 support。
- 旧版通用 Xray/geo/ASN 路径仅在对应私有资产安装成功后迁移；默认保留 core 的升级不再把可用配置改向空路径。

### 维护

- 重建文档信息架构，新增项目背景、总体架构、完整配置、Docker/原生部署、运维排障、开发测试、版本发布、贡献与安全专题；README 收敛为清晰的项目入口。
- 新增可执行文档门禁，检查 Markdown 标题、围栏、文件、锚点和入口可达性；新增固定 `protoc 35.1`/`protoc-gen-go v1.36.11` 的 wire 再生成入口。
- 项目 `Version` 与官方 `ContractVersion` 正式解耦：`X.Y.Z-rnl.N` 表示独立项目迭代，纯 `X.Y.Z` 表示完成官方对齐的正式版本。
- 候选镜像先完成 attestation，再发布不可移动的 commit tag；验收 manifest 绑定实际 digest，Release 只允许把该 digest 晋升为精确版本和 `latest`，并拒绝覆盖不同内容；发布 tag 必须指向当前 `main` HEAD。
- 候选镜像 OCI version 使用项目版本而不是 commit 别名；发布资料严格 squash 为一个提交，Release note 记录候选和 digest，最终提交由 Git tag 解析，避免不可实现的 commit 自引用。
- Go module、安装脚本、发布地址和文档归属切换到本仓库。
- 建立行为兼容、架构修复和 512 MiB 小内存验收路线。
- 契约 CI 直接读取固定官方 commit 的 Git object，校验 58 个登记 blob（含依赖锁文件）的 SHA-256，并以 fail-closed 规则绑定 Nest bootstrap、静态 import、module/controller metadata、decorator ownership、global prefix exclusions 和 26 条路由；升级官方 pin 时仍须人工评审提取结果与不支持的新语法。
- 发布门禁绑定冻结候选 commit、严格 JSON 验收证据、兼容/资源/故障结果和只允许发布文档变化的两阶段流程。
- HTTP transport 与 stats、用户 handler、plugin 业务服务分离，业务层不再依赖 `net/http` 或自行解码 JSON。
- `main` 成为单一组合根，显式创建并注入网络监控器、系统采集器、版本、请求体预算和应用服务；删除 import 即启动 goroutine、进程级可变 body limit 和环境变量回写等隐藏全局状态。
- 固定并校准外部 `@remnawave/node-plugins@0.4.5` schema 证据，覆盖显式 null、AS number、`ext:` 与数值边界。
- 用最小 rw-core protobuf wire client 替换完整 Xray Go module，双架构二进制缩小约 30%。
- M7 已在 Ubuntu 24.04/systemd 与 Alpine 3.22/OpenRC 完成全新安装、升级回滚、启停、专用用户/capability、日志、磁盘和卸载隔离工程基线；它不替代冻结候选的 M8 验收。
