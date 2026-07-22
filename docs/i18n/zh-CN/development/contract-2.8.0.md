<!-- translation: locale=zh-CN; source=docs/development/contract-2.8.0.md; source-sha256=33d3705483d51b902bbdb65504f599b2d7ae7260c9ce08e6d2cb9a875a4aad06 -->
# Remnawave Node 2.8.0 行为契约基线

> 这是中文译文；涉及契约细节时，请以[英文原文](../../../development/contract-2.8.0.md)为准。

[返回开发文档](README.md) · [架构说明](../architecture.md)

本文记录项目对官方 Node `2.8.0` 的兼容基线。官方契约发生变化时，应新增版本文档或明确说明迁移过程，不能静默替换这里固定的源码依据。

## 证据边界

本文件与 `internal/contract` 共同描述本项目的兼容目标。唯一官方代码基线是：

- 仓库：`https://github.com/remnawave/node.git`
- 版本：`2.8.0`
- 提交：`596f015a5c8f876dc9a9d61b6cb78d35bd8e379b`
- 集成验证使用的 Panel 版本：`2.8.1`（与项目版本号相互独立）

路由方法来自四个官方 controller，请求和响应结构来自 `libs/contract/commands` 下的 Zod schema，应用错误来自 `libs/contract/constants/errors` 与 `HttpExceptionFilter`。`internal/contract/official-source-manifest.json` 记录每个证据 blob 的 SHA-256，以及工具从源码提取的 26 组 method/path/controller decorator。

提取器直接读取固定提交中的原始 Git 对象，禁用 replace refs，也不信任暂存区、工作区或当前 `HEAD`。它会解析 `ROOT`、`REST_API`、controller/route 常量和 NestJS HTTP decorator。它还会遍历 Git tree，找到 controller 和 module，确认 `main.ts` 通过 `NestFactory.create(AppModule)` 启动，检查静态 import 和受支持的 metadata，将每个 route decorator 绑定到唯一导出的 controller class，并确认每个 controller 都由可达 module 注册。两个内部 controller 路径必须与同一次 `setGlobalPrefix` 调用的 exclusion 精确对应。

遇到条件表达式、spread、未知 dynamic module 或复合、别名、qualified decorator 等未支持语法时，提取器会直接失败。要支持新语法，必须先扩展并评审解析器。两套独立得出的官方路由清单最终都必须与本地 Go 契约完全一致。

这套提取器有意保持较窄的能力范围，不会把任意 TypeScript 或 Zod 自动翻译为 Go schema。请求和响应约束仍由维护者评审并提炼为契约数据。manifest 摘要用于暴露官方证据变化，schema 边界测试和 `contract-probe` 用于验证实现。升级契约时仍须评审官方 diff，不能把提取成功当作完整的语义等价证明。

插件 `config` 的 schema 不在 Node 仓库内定义，而是来自官方 `package.json`/`package-lock.json` 固定的 `@remnawave/node-plugins@0.4.5`。人工审计使用 npm tarball `node-plugins-0.4.5.tgz`，SHA-1 `3bfc3988278790ec40a93d6e6169f893c31bf62d`，SHA-512 integrity `sha512-r9Lce/l/kHQATNhWbcutApFSJ5hH/Yu6Kv0+/qjpUDIEa1+DFb54Q8IwuvqWzxxbGkG9oO0cAeN4busBzz0a5Q==`；Go 端插件校验以其中 `build/backend/models/node-plugins.schema.js` 为准。当前源码证据测试不下载或校验这个外部 tarball，复核步骤见[测试指南](testing.md#外部插件-schema-证据)。

## 通用语义

- 外部 API 使用双向 TLS；官方最低版本为 TLS 1.3。
- 所有 `/node` 路由使用 RS256 Bearer JWT。官方在认证失败时销毁 socket，不返回 HTTP body。
- 官方对未知路由同样销毁 socket。
- 有请求 DTO 的接口使用 Zod 校验；对象的未知字段会被剥离，而不是拒绝。
- DTO 校验失败返回 HTTP 400：`statusCode=400`、`message="Validation failed"`、`errors=[...]`。
- 成功响应统一返回 HTTP 200，顶层为 `{ "response": ... }`。
- 已知应用错误包含 `timestamp`、`path`、`message` 和 `errorCode`；当前已定义 A001-A017 中的相关错误。
- 未映射的 Nest 异常使用通用 `statusCode`、`message`、可选 `error` 响应。
- 本项目可因资源保护设置更小的请求上限，但偏差必须明确、可观测且不得在校验前产生副作用。

## Go API 边界实现

20 个带请求 DTO 的路由都通过 `internal/nodeapi`。解码器只接受一个 JSON 文档，保留 Zod 对未知字段的剥离语义，并为缺少字段、类型错误、联合类型判别字段、UUID/IP、枚举、nullable/default 和 `minItems` 生成统一的验证响应。6 个没有请求 DTO 的官方路由允许空请求体；如果请求声明 `application/json` 并携带内容，内容仍须是单个合法对象或数组。格式错误的 JSON 会在产生副作用前被拒绝。

`internal/httpserver` 负责认证后的容量控制、请求解码和校验、跨 Xray/plugin 生命周期协调、命令映射和响应 envelope。stats、用户 handler 与 plugin 服务不再接收 `http.ResponseWriter`、`*http.Request`，也不自行解码 JSON。Xray 配置直接解码为一份 map 后交给 Manager，不再经过 `RawMessage` 和第二次反序列化。

传输层测试为 provider、连接 dropper、plugin service 和 Xray Manager 注入计数型测试替身，要求每类非法请求都产生零次调用。合法请求会经过真实 dispatcher，再用独立的官方响应 schema 校验结果。

## Go Xray 生命周期实现

生命周期行为以官方 `src/modules/xray-core/xray.service.ts`、`xray-process.service.ts`、`xray.module.ts` 和应用关闭钩子为依据。Node 启动时，Xray 缓存状态为离线，不从磁盘恢复旧 Panel 配置；`healthcheck` 只读取缓存状态；Panel stop 同时负责停止 core 和清理插件。Go 实现会先确认 core 已停止，再重置插件，避免停止失败时提前撤销过滤规则。应用退出采用相同顺序。

Go manager 使用单一显式状态，而不是多个可形成非法组合的布尔量：

| 当前状态 | 事件 | 后续状态 | 提交语义 |
| --- | --- | --- | --- |
| `stopped` | 接受 start | `starting` | 只暂存供新进程拉取的 pending config |
| `running` | 接受需重启的 start | `starting` | 先回收旧进程，再暂存新配置 |
| `starting` | gRPC 就绪且进程仍存活 | `running` | 原子提交 active config、hash 和 inbound tag |
| `starting` | 取消、超时、spawn/进程失败 | `stopped` / `stopping` | 清理成功后回到 stopped；终止失败则保留进程所有权和 stopping，供后续 stop 重试 |
| `starting` | stop | `stopping` | operation epoch 失效并取消 start，由 stop 接管进程 |
| `running` | stop 或自然退出 | `stopping` / `stopped` | 回收进程并清理配置与 hash 状态 |

Manager 用 operation mutex 串行修改生命周期，用 manager mutex 发布状态并转移所有权。HTTP 协调器向 start 发放共享租约，并用两个独立的 handler 槽限制同时驻留的大配置。因此第二个并发 start 可以进入 Manager，并立即得到与官方一致的 `Request already in progress`。

stop、plugin sync/recreate、用户变更和带 reset 的 stats 操作使用独占租约。已有独占操作等待时，后续 start 不能插队。每个被接受的生命周期操作都有单调递增的 `operationEpoch`，旧 start 无法覆盖后来的 stop；每个实际 rw-core 进程另有唯一的 `process epoch + abstract socket` 身份。

每个成功启动的子进程只由一个 `Wait` goroutine 回收。Linux 将 rw-core 放入独立进程组，并设置 parent-death signal。正常 stop 的 SIGINT、超时后的 SIGKILL，以及进程组长自然退出后的兜底清理都作用于整个进程组。parent-death signal 只直接保护组长；如果 Node 或 supervisor 自身被强杀，项目不保证自动回收所有后代。此时应重启服务或主机，或者重新创建容器。

进程级测试覆盖 pending 到 active 的提交边界、并发 start、start/stop 交错、取消、启动超时、就绪前后退出、并发及重复 stop，以及 SIGINT 到 SIGKILL 的升级。Linux 测试额外覆盖独立进程组、整组信号和后代清理。路由测试覆盖 start 共享准入、stop/plugin 独占等待、等待取消、Panel stop 的 `Stop -> ResetPlugins` 顺序，以及 Stop 失败时保留插件快照和 nft 规则。

## Go 插件与 nftables 实现

插件行为以官方 `plugin.service.ts`、`nft.service.ts`、`plugin-state.service.ts`、torrent blocker 状态和 webhook handler，以及 `@remnawave/node-plugins@0.4.5` 为依据。每次变更先从已校验配置构建不可变计划，一次完成 shared list 和 ASN 展开、连接踢除白名单、Torrent 有效状态和防火墙规则。

启用或更新时，先应用防火墙规则，再协调 Xray；关闭、清理和破坏性 recreate 则先协调 Xray，再重置防火墙。两侧都成功后才提交快照。失败不会发布不匹配的新状态，可回滚路径还会重放上一份防火墙计划，让同一个 Panel 请求可以安全重试。

Initialize、sync、reset、block、unblock 与 recreate 通过容量为 1、支持 `context` 取消的操作闸门串行执行。HTTP 应用层使用 start 共享、变更独占的生命周期协调器，锁顺序固定为 `Xray lifecycle lease -> Plugin operation gate -> Manager`，防止 core 读取启动配置时插件快照发生变化。未来新增绕过 HTTP 的内部入口时，也必须复用该协调器。

关闭不带 `includeRuleTags` 的 Torrent blocker 时，会热删除 `RW_TB_OUTBOUND_BLOCK`，不会停止在线 core。健康状态下的 `recreate-tables` 只重建 nftables；只有从降级防火墙恢复并让 Torrent blocker 重新生效时，才会停止 core。

webhook 接收不会直接等待操作闸门，而是在内部请求的 30 秒截止时间内等待容量为 64 的有界队列。单 worker 随后取得同一闸门，执行 nft 和报告副作用。容量没有恢复、请求被取消或服务关闭时，接口返回 `503 + Retry-After`，不会把未接纳事件报告为成功。collect 只会在持有 State 锁时原子取走报告。

nftables 初始化与 Go 对象构造相互独立。缺少 `CAP_NET_ADMIN` 或 nft 初始化失败时，合法插件配置仍会被接受，但 ingress、egress 和 Torrent 过滤保持不可用，Torrent 状态也不会错误注入 Xray。reset 只替换插件快照，不丢弃 Panel 尚未收集的 Torrent 报告；recreate 会重放当前已提交的过滤计划，而不是创建空表。

Close 先设置不可逆的 mutation admission fence 并停止 webhook worker；此前已经接纳的 mutation 可以完成，新 mutation 一律拒绝。等待 gate、删除 nft 表和 join worker 共享调用方 deadline，并由服务再限制为最多 15 秒；清理失败保留已提交快照，只允许后续 Close 重试，不会重新开放业务操作。

nft backend 在单个 `nft -f` 原子事务内替换 IPv4/IPv6 私有表和过滤元素，批量处理 block，unblock 同时覆盖 torrent/ingress 的双栈 set。进程退出先停止 rw-core，再删除 `remnanode`/`remnanode6`，listener 失败也会进入同一清理路径。Linux network namespace 集成测试真实覆盖初始化、两次 plan 替换、双栈 block、重复 block/unblock、recreate 和 close；该门禁已在 Linux arm64 6.8 内核与 nftables 上通过，amd64 测试二进制也已交叉编译并纳入 CI。

## Go 用户、连接与统计实现

所有用户变更都通过一个支持取消的串行闸门。每次 add/remove 在读取 inbound/IP 状态前，会取得绑定当前 rw-core `process epoch + abstract socket` 的租约。Handler RPC、连接清理和本地 inbound 哈希提交都在同一租约内执行；`Start` 和 `Stop` 等待租约释放，因此一次变更不会跨越两个 rw-core 进程。

清理失败时，操作不会继续为该用户添加替代账号。批量请求中的任何失败都会返回 `success=false` 和第一个明确错误。多个远端 RPC 无法组成真正的跨调用事务，已成功的前序操作不会被描述成已回滚；但本地状态不会领先于 rw-core，同一个 Panel 请求仍可安全重试。

连接踢除会先规范化和去重 IP，跳过白名单，并拒绝非法地址、unspecified、loopback、link-local、multicast、IPv4 广播和本机接口地址。每批通过 `NETLINK_SOCK_DIAG` 分别枚举 IPv4/IPv6 socket，并逐条校验 `SOCK_DESTROY` ACK；仅 `ENOENT` 作为幂等成功，缺少 `CAP_NET_ADMIN`、用户 IP 查询失败或任一销毁失败都会返回真实的 `success=false`。CI 在独立 Linux network namespace 中建立真实 TCP 连接并验证 socket 被关闭；该门禁已在 Linux arm64 6.8 内核上通过。

`get-users-ip-list` 优先使用 rw-core 的单次 `GetUsersStats` 扩展 RPC；旧 core 返回 `UNIMPLEMENTED` 后会缓存 capability 并降级为最多 8 个固定 worker，不再为每个在线用户创建 goroutine。所有 Handler/Stats unary RPC 默认共享最多 5 秒 deadline，健康探测为 3 秒，调用方已有的更早 deadline 和取消信号保持生效；legacy 批量查询使用单一总预算，不会为每个用户重新续期。

## Go 资源预算实现

资源设计在保持官方 HTTP 契约的同时，对常驻和临时数据都设定了明确上限。Xray 配置仅在启动阶段保留解码树和规范 JSON；rw-core 就绪后，只留下哈希、inbound tag 和运行状态。Torrent 报告使用容量为 1024 的有界环形队列，zstd 输入、窗口、解码并发、请求体和 gRPC 响应也都有上限。

完整的 Xray Go module 已被最小 protobuf wire 客户端替代，并与官方生成类型校准。五种账号类型、Handler 请求、Stats 消息和确定性的 wire golden 测试共同固定兼容性。

`LOW_MEMORY=1` 时，公开 `/node` server 的默认请求体上限为 16 MiB，Go runtime 的内存软上限为 180 MiB。显式 `BODY_LIMIT_MB` 允许 `1..1024`；非法、负数或溢出值会让进程启动失败，而不是静默回退。内部 Unix webhook 仍使用独立的 8 KiB 固定上限。Debian 与 Alpine 安装器会在整机内存不超过 512 MiB 时自动启用该模式。

生产 init 只读取 `/etc/remnanode-lite/node.env`，不会回退到服务账号可写的工作目录。配置必须是普通的非符号链接文件，总大小不超过 1 MiB，最多 4096 行和 256 个赋值。配置与 Secret 都通过同一个带 `O_NOFOLLOW|O_NONBLOCK` 的文件描述符完成检查和有界读取。systemd/OpenRC 不会导出整份配置环境，`GOMEMLIMIT` 与版本覆盖值由同一个 Go 解析器验证并应用。

真实 rw-core `v26.6.27` 的 1 CPU / 448 MiB / no-swap 门禁覆盖 1k 用户启动、无变化同步、50k 用户重启、热增删与统计 RPC，带日期的 M6 工程实测 cgroup 峰值为 143.9 MiB。这是一份可复现的工程基线，不保证任意负载都得到相同结果。复现条件和阶段数据见 [`resource-budget.md`](resource-budget.md)。

## Go 传输、系统与供应链实现

公开服务要求 TLS 1.3 或更高版本，并关闭 Go 的 HTTP/2 自动协商，以保持官方连接处理模型。无效 JWT、未知路由和错误 HTTP method 会直接关闭底层连接，而不是返回可枚举的 401/404/405 响应。请求头上限为 64 KiB。真实 TLS 客户端测试覆盖正常连接复用，以及认证失败或未知请求后的连接关闭。

systemd 与 OpenRC 均使用专用 `remnanode-lite:remnanode-lite` 账号，配置为 `root:remnanode-lite 0640`，状态和日志目录为 `remnanode-lite:remnanode-lite 0750`。服务只获得 `CAP_NET_ADMIN` 与 `CAP_NET_BIND_SERVICE`；systemd 同时将 bounding set 收紧到这两项，并启用 `NoNewPrivileges`、只读系统、namespace/syscall/address-family 限制、`448 MiB` 内存、零 swap、1 CPU 和 256 tasks。Alpine 3.22 的 supervise-daemon 实测 `CapInh/Prm/Eff/Amb=0x1400`、`NoNewPrivs=1`，且由服务派生的 `nft` 子进程可创建私有表。

Native 项目资产位于 `/usr/local/lib/remnanode-lite` 的 generation 中；Docker 在容器私有镜像路径中使用相同项目名称。两种方式都不接管通用 Xray 路径。Release 归档、rw-core zip、自定义 core 与 ASN 数据必须通过 SHA-256、结构和版本检查后才能安装。固定 rw-core `v26.6.27` 的已审计摘要不能被覆盖。

升级会备份 binary、service、support、`node.env` 和可选 rw-core 资产。刷新后的服务或端口检查失败时，所有内容都会恢复。Ubuntu/systemd 与 Alpine/OpenRC 的错误 service 注入验证了摘要和运行状态恢复。完整卸载测试也确认：无关的同名进程不会被终止，通用 Xray 文件不会被删除。

上述带日期的 M7 systemd/OpenRC 与错误 service 回滚观测属于工程基线，只适用于当时记录的提交和环境。

整机退出使用一个共享的 25 秒应用预算，而不是为各组件串行重置 timeout。HTTPS/Unix intake、日志轮转和后台版本探测先收到取消；rw-core 最多使用 5 秒 SIGINT 加 5 秒 SIGKILL，确认 core 停止后，插件再使用剩余预算清理私有 nft 表。core 或插件的瞬时清理错误会在同一 deadline 内重试一次。公开 `xray/stop` 也串行化 start/stop，只有 core 确认停止后才 reset 插件；Stop 失败会保留规则与快照。systemd 提供 30 秒 TERM grace，OpenRC 提供 `TERM/30/KILL/5` 外层兜底；deadline 或清理失败会形成聚合错误，不能被记录为优雅退出成功。

## 路由清单

表中只列核心约束；完整类型、nullable、枚举、UUID、IP、日期和数组长度约束以 `internal/contract/official_schemas.go` 的可执行 schema 为准。

| 方法 | 路径 | 请求核心 | 响应核心 | 主要副作用或错误 |
| --- | --- | --- | --- | --- |
| POST | `/node/xray/start` | `internals.hashes`、`xrayConfig`；`forceRestart` 默认 false | `isStarted`、nullable `version/error`、节点和系统信息 | 启动或替换 rw-core，替换配置和 hash 状态；失败仍返回 HTTP 200、`isStarted=false` 和 nullable `error`。RN-001 是官方未就绪日志诊断，不是响应字段 |
| GET | `/node/xray/stop` | 无 body | `isStopped` | 停止 rw-core，并清理插件状态和规则 |
| GET | `/node/xray/healthcheck` | 无 body | `isAlive`、缓存状态、nullable Xray 版本、Node 版本 | 只读缓存和进程状态 |
| POST | `/node/stats/get-user-online-status` | `username` | `isOnline` | 查询在线状态；SDK 错误降级为 false |
| POST | `/node/stats/get-users-stats` | `reset` | `users[]` 流量 | `reset=true` 清零计数；A011 |
| GET | `/node/stats/get-system-stats` | 无 body | nullable `xrayInfo`、插件和系统统计 | 查询 rw-core/宿主机；A010 |
| POST | `/node/stats/get-inbound-stats` | `tag`、`reset` | inbound 流量 | 可清零计数；A012 |
| POST | `/node/stats/get-outbound-stats` | `tag`、`reset` | outbound 流量 | 可清零计数；A013 |
| POST | `/node/stats/get-all-outbounds-stats` | `reset` | `outbounds[]` | 可清零计数；A016 |
| POST | `/node/stats/get-all-inbounds-stats` | `reset` | `inbounds[]` | 可清零计数；A015 |
| POST | `/node/stats/get-combined-stats` | `reset` | `inbounds[]`、`outbounds[]` | 可清零计数；A017 |
| POST | `/node/stats/get-user-ip-list` | `userId` | `ips[]`，含 ISO date-time | 查询并重置单用户 IP 统计 |
| GET | `/node/stats/get-users-ip-list` | 无 body | `users[].ips[]` | 查询已知用户 IP 统计 |
| POST | `/node/handler/add-user` | `data[]` 联合类型、`hashData.vlessUuid` | `success`、nullable `error` | 增加用户并更新 inbound hash |
| POST | `/node/handler/remove-user` | `username`、UUID hash | `success`、nullable `error` | 先读取 IP，再删除所有相关 inbound 用户/hash；全部成功后才踢连接 |
| POST | `/node/handler/get-inbound-users-count` | `tag` | `count` | 查询 rw-core；A014 |
| POST | `/node/handler/get-inbound-users` | `tag` | `users[]` | 查询 rw-core；A014 |
| POST | `/node/handler/add-users` | `affectedInboundTags[]`、`users[]` | `success`、nullable `error` | 批量增加用户并替换受影响 hash |
| POST | `/node/handler/remove-users` | `users[]`，每项含 userId/UUID | `success`、nullable `error` | 逐用户读取 IP 并删除相关 inbound 用户/hash；仅对删除成功者批量踢连接 |
| POST | `/node/handler/drop-users-connections` | 非空 `userIds[]` | `success` | 查询 IP 后终止宿主机连接 |
| POST | `/node/handler/drop-ips` | 非空 `ips[]` | `success` | 终止宿主机连接；官方不要求元素是合法 IP |
| POST | `/node/plugin/sync` | nullable `plugin`；非空时含 config/UUID/name | `accepted` | 替换或清空插件状态，协调 nftables 和 rw-core |
| POST | `/node/plugin/torrent-blocker/collect` | 无 body | 完整 `reports[]` | 原子取走并清空报告队列 |
| POST | `/node/plugin/nftables/block-ips` | `ips[]`，元素为合法 IP 和数值 timeout | `accepted` | 写入定时封禁并踢连接 |
| POST | `/node/plugin/nftables/unblock-ips` | 合法 IP 数组 | `accepted` | 删除插件表内的封禁 |
| POST | `/node/plugin/nftables/recreate-tables` | 无 body | `accepted` | 重建并重新填充插件 nftables 表 |

## 请求联合类型

`handler/add-user` 的 `data[]` 只接受以下 discriminant：

- `trojan`：tag、username、password
- `vless`：tag、username、uuid、flow；flow 只能是 `xtls-rprx-vision` 或空字符串
- `shadowsocks`：tag、username、password、cipherType、ivCheck
- `shadowsocks22`：tag、username、password
- `hysteria`：tag、username、password

`handler/add-users` 的 `inboundData[]` 使用同样五种类型；VLESS 额外要求 flow。每个 `userData` 必须包含 userId、hashUuid、vlessUuid、trojanPassword 和 ssPassword。

## 当前已知偏差

此前记录的 TLS/socket 与系统供应链偏差已经关闭。当前没有已知的静态 `/node` 契约 P1/P2。

`v2.8.0` 候选必须使用生产 Compose 模板在原生 `x86_64`/`amd64` 上验证不可变的 `sha-<main-commit>` 镜像，确认版本输出、真实 Panel 2.8.1 连接和真实代理流量。该人工判断不作为运行数据提交到仓库。容器仍必须限制为 448 MiB 内存、不得获得额外容器 swap、1 CPU 和 256 PIDs。

annotated tag 必须指向当前 `main` HEAD。Release workflow 会解析该提交的 `sha-*` 候选，校验两个可运行 Linux manifest、对应的 BuildKit attestation manifest 和 GitHub 源码 attestation，再在不重建镜像的情况下把同一 digest 晋升为 `2.8.0` 与 `latest`。Release notes 由 GitHub 自动生成。

原生 `arm64` 运行、systemd/OpenRC 安装、50,000 用户负载复测、长时间 soak 和故障注入仍是有价值的后续验证。没有实际运行时不得描述为已经通过。

Docker Compose 与官方一样使用 host network 和 `NET_ADMIN`，同时保留低端口监听
能力；Go Manager 直接拥有 rw-core 生命周期，因此无需复制官方双进程 s6 运行结构。
systemd/OpenRC 继续作为等价的原生部署入口。

两份受维护的生产 Compose 模板都使用 `remnanode-lite` 作为 service、container 和
hostname。它们从 `.env` 插值同一组显式运行变量，应用生产默认值，并在创建容器前拒绝
缺失或为空的 `SECRET_KEY`；`.env` 不会被整份注入容器。

运行期 `dump-config` 是已接受的后置差异：Manager 只在 rw-core 启动期间保留完整规范配置，ready 后释放该副本并让 `CurrentConfigJSON` 返回 `{}`。这是面向 512 MiB 节点的内存取舍，不影响 `/node` 或 rw-core 启动契约；后续如恢复该诊断能力，必须采用有界方案，不能常驻第二份大配置。

## 本地验证

常规可执行契约测试：

```bash
go test ./internal/contract
```

该命令始终把已提交的机器提取 manifest 与本地 `OfficialRoutes` 对照，因此 method/path
的人工漂移不需要官方 checkout 也会失败。

连同固定官方源码证据一起验证：

```bash
go run ./cmd/contract-source-check \
  -source /tmp/remnawave-node-official-2.8.0-codex
```

需要更新固定契约时，在确认 `OfficialNodeCommit` 和证据目录清单后显式加 `-write` 重建
manifest，再评审 manifest diff 并运行常规契约测试。工具允许 checkout 为 dirty 或 HEAD
指向其他提交，因为唯一输入是固定 commit object；如果仓库不含该 object 则直接失败。

在具备 root/unshare/nft 的 Linux 主机上运行隔离防火墙测试：

```bash
sudo env "PATH=$PATH" REMNANODE_NFT_INTEGRATION=1 \
  go test ./internal/plugin -run '^TestNFTManagerInNetworkNamespace$' -count=1 -v
```

测试会确认机器提取的 26 条官方 method/path 与本地契约和真实 dispatcher 完全一致。覆盖范围包括所有合法请求样例、缺少字段、类型错误、额外字段、未知联合类型判别值、UUID/IP/`minItems` 约束、真实 Go handler 的完整成功响应 schema，以及官方统一错误 schema。

## 黑盒差分入口

列出路由及默认安全级别：

```bash
go run ./cmd/contract-probe -list
```

准备由同一 CA 签发的 Panel 客户端证书，并用第一个 target 作为官方基准：

```bash
export REMNANODE_CONTRACT_CA=/secure/ca.pem
export REMNANODE_CONTRACT_CERT=/secure/panel-client-cert.pem
export REMNANODE_CONTRACT_KEY=/secure/panel-client-key.pem

go run ./cmd/contract-probe \
  -token-file /secure/panel.jwt \
  -target official=https://127.0.0.1:2222 \
  -target candidate=https://127.0.0.1:3222
```

如果证书只包含 DNS 名称而 target 使用 IP，需额外传入 `-server-name <证书名称>`；探针不提供跳过证书验证的选项。

默认只执行 11 条无破坏性请求：健康检查、`reset=false` 的统计和 inbound 用户只读查询。探针比较状态、响应类别、应用错误码、schema 和去除动态字段后的 `SemanticSHA256`；它会记录 raw body 大小与 SHA-256 供审计，但不会用二者判定两个 target 是否语义一致，也不比较机器指标、流量值或耗时。报告不包含 JWT 和原始响应 body。

启动/停止、用户增删、连接踢除、IP 统计重置、报告 drain 和 nftables 操作必须同时显式指定 `-routes` 与 `-allow-mutating`，并只应在隔离测试环境执行。
