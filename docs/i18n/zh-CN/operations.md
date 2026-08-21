<!-- translation: locale=zh-CN; source=docs/operations.md; source-sha256=b8fb94f1665d1c37da012250b8041de8f92325a2de8d9f3b6d46a9e1249f0c34 -->

# 运维与故障排查

[英文原文](../../operations.md) · [文档索引](README.md) · [Docker 部署](deployment-docker.md) · [Native 部署](deployment-native.md) · [配置参考](configuration.md)

Remnanode Lite 的持久数据很少。Panel 仍是代理配置的真相源，日常运维主要确认四件事：Node 进程、Panel 连接、rw-core 状态和真实代理流量。

## 每项检查能证明什么

| 检查 | 能证明 | 不能证明 |
| --- | --- | --- |
| 容器或服务在运行 | supervisor 看见 Node 进程 | 内部 health 可用 |
| Docker health 或 `rnlctl status --json` 正常 | 私有 Unix socket 可响应，受管状态一致 | Panel 能访问公网端口 |
| Panel 显示 Node online | mTLS/JWT 与 Panel-to-Node 路径正常 | rw-core 已有可用代理配置 |
| Panel 显示 rw-core online | core 启动和内部 gRPC 正常 | 所有代理路径都可传输 |
| 客户端真实传输 | 当前测试路径端到端可用 | 所有协议、地址族和路由都可用 |

公开的 `/node/xray/healthcheck` 需要 mTLS 与 JWT，不是匿名监控端点。

## 例行检查

Docker：

```bash
docker compose ps
docker compose logs --tail=100 remnanode-lite
docker inspect remnanode-lite --format \
  'image={{.Config.Image}} status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}} oom={{.State.OOMKilled}} restarts={{.RestartCount}}'
docker exec remnanode-lite remnanode-lite version
ss -H -lntp 'sport = :2222'
```

Native：

```bash
sudo rnlctl overview
sudo rnlctl status
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl logs node --lines 100
sudo rnlctl logs core-errors --lines 100
ss -H -lntp 'sport = :2222'
```

`overview` 是人工巡检的首选入口。它汇总生命周期与服务状态、版本和 generation、
不含 Secret 的监听端点、已知问题，以及一组随当前状态变化的简短命令。它只读取本地
生命周期状态和允许管理员查看的安全配置，不会连接 Panel 或制造代理流量；没有 JSON
形式，human 排版也不是解析接口。

直接运行 `rnlctl status` 现在会输出一致、便于阅读的生命周期摘要，不再转发服务管理器的原始输出；human 排版不是解析接口。`status --json` 保持原有 schema，包含 current/previous generation、版本、服务管理器、启用与活动状态、repair 能力和待处理操作。状态为 degraded 或 recovery-required 时，两种形式都会返回非零。

`doctor` 会校验 manifest、文件摘要、链接、配置、Secret、服务、内部 health 和修复缓存。human 输出最后包含汇总以及针对已知故障的确定性 `Next` 建议；`doctor --json` 的 schema 保持不变。它不会连接 Panel 或制造代理流量。

底层服务视图：

```bash
sudo systemctl --no-pager --full status remnanode-lite.service
sudo systemctl show remnanode-lite.service \
  --property=ActiveState,SubState,MainPID,MemoryCurrent,MemoryPeak,TasksCurrent

# Alpine/OpenRC
sudo rc-service remnanode-lite status
```

需要完整的底层详情时直接使用这些服务管理器命令。此前解析裸 `rnlctl status` 输出的脚本应改用 `rnlctl status --json`。

## `rnlctl` 输出与自动化

`--quiet`/`-q`、`--no-color` 和 `--progress auto|plain|never` 都是全局选项，
可以放在 `rnlctl` 命令中的任意位置。进度写入 stderr，最终结果和命令数据写入 stdout；
错误也写入 stderr。

`auto` 是默认模式：stderr 是 TTY 时实时刷新，否则输出稳定的逐阶段文本；
`TERM=dumb` 也会选择 plain 输出。`plain` 强制使用不重写光标的逐行进度，`never`
只隐藏进度。只有能够确定总大小的传输才显示百分比、速率和预计剩余时间。生命周期阶段
只说明实际工作，不表示整体完成百分比，也不是稳定的解析接口。

`install`、`activate`、`upgrade`、`rollback` 和 `repair` 成功后会附带随状态变化的
`Next` 命令；生命周期变更失败时，stderr 可能附带安全的本地 `status`、`doctor` 或
Node 日志诊断命令。这些建议绝不会自动执行，取消类错误不会附加建议，quiet 模式也会
隐藏它们。

quiet 会覆盖显式选择的进度模式，并隐藏成功的变更提示、`config check` 成功提示和
human `overview`/`status`/`doctor`；不会隐藏 help、version、`config show`/`get`、日志、补全、
dry-run 计划、JSON 或错误。JSON 输出会关闭进度。

面向人的输出只在对应输出流是 TTY 时使用克制的颜色：overview、status 和 doctor 检查 stdout，
进度检查 stderr。指定 `--no-color`、`NO_COLOR` 为非空值或 `TERM=dumb` 时，两个输出流
都不使用颜色；被重定向的输出流也不会包含颜色转义序列。

第一次收到 `SIGINT`、`SIGTERM`、`SIGHUP` 或 `SIGQUIT` 时，当前操作会尝试安全取消，
清理或回滚所需的主机命令和健康检查受一分钟恢复期限约束。本地文件操作可能会先完成
当前这个有界步骤再退出。Ctrl-C 会在这次尝试后返回 `130`。第一个信号送达后会恢复
操作系统的默认处理方式，因此第二次信号可以立即强制退出；如有必要，随后检查
`status --json` 并运行 `repair`。

退出码通常为：成功 `0`，运行失败或结果不健康 `1`，用法错误 `2`。`status` 和 `overview` 都把 `absent` 视为有效状态并返回 `0`；安全配置摘要无法读取时，`overview` 返回 `1`。要求必须已安装的脚本应使用 `status --json`，并检查其中的 `installed` 或 `deployment`。`logs` 启动 `journalctl` 或 `tail` 后会透传其退出码，被信号终止时也可能返回 `128 + signal`。

Bash、Zsh 和 Fish 补全由 `rnlctl completion <shell>` 生成，用户级安装步骤见 [Native 部署指南](deployment-native.md#shell-补全)。命令只向 stdout 输出脚本，不会修改补全目录或 shell 启动文件。

## 日志

| 部署 | Node 日志 | 存储 |
| --- | --- | --- |
| Docker | `docker compose logs -f remnanode-lite` | Docker `json-file`，维护模板为 `2 MiB x 2` |
| Native systemd | `sudo rnlctl logs node --follow` | 宿主 journald 策略 |
| Native Alpine/OpenRC | `sudo rnlctl logs node --follow` | `/var/log/remnanode-lite/openrc.log` 与 `.err.log` |

小型 systemd 主机应为 journald 设置合理的宿主机配额，并监控 `journalctl --disk-usage` 与 `df -h`。

Docker 的 rw-core 使用容器私有路径：

```bash
docker exec -it remnanode-lite \
  sh -c 'tail -n 50 -F "$LOG_DIR/xray.out.log" "$LOG_DIR/xray.err.log"'
```

Native 使用：

```bash
sudo rnlctl logs node --since 15m --lines 100
sudo rnlctl logs core --follow
sudo rnlctl logs core-errors --follow
```

`--lines` 默认 `50`，范围为 `1..100000`。`--since` 接受 `15m`、`2h` 这类正数 Go duration，不接受绝对日期或 `1d`；它可以与 `--lines`、`--follow` 组合，但只支持 systemd 的 `node` 日志。OpenRC Node 日志和文件型 `core`/`core-errors` 会拒绝该选项。

systemd 将 `--lines N` 应用于整个 unit；OpenRC 从 `openrc.log` 与 `openrc.err.log` 各读 N 行，core source 各读取一个当前文件。Native core 文件位于 `/var/log/remnanode-lite/xray.out.log` 和 `xray.err.log`，每条流保留当前文件和一个 `.1`，阈值为 4 MiB，但普通读取不会从 `.1` 回补。`--follow` 使用 `tail -F`，可继续跟随后续轮转。Docker 把 core 日志目录放在 28 MiB tmpfs，重建容器即可清空。

## 启停

Docker：

```bash
docker compose restart remnanode-lite
docker compose stop remnanode-lite
docker compose up -d --no-build
docker compose down
```

Native：

```bash
sudo rnlctl restart
sudo rnlctl stop
sudo rnlctl start
```

使用 `--prepare-only` 创建的安装必须先运行 `rnlctl activate`。正常运维不要使用 `kill -9`，否则会绕过 HTTP drain、rw-core 进程组关闭和 nftables 清理。

## Docker 更新与回滚

当前稳定目标是 `3.3.2`。该 Release 的 Compose 模板选择匹配的镜像，并包含 Release
固定的 GeoCheck 资产。保留 `.env`，记录当前 digest，重建后检查 Panel 连接、GeoCheck、
rw-core 和代表性代理流量。

| 引用 | 用途 |
| --- | --- |
| `name@sha256:<digest>` | 最强 production 固定和回滚身份 |
| `X.Y.Z` | 精确稳定版 |
| `X.Y.Z-rnl.N` | 精确预览版 |
| `latest` | 可选稳定移动通道 |
| `preview` | 可选预览移动通道，不用于 production 回滚 |
| `sha-<40-character-commit>` | main 候选验证 |
| `edge` | 短期 main 开发观察 |

受控更新流程：

1. 记录当前精确 tag 或 manifest digest。
2. 阅读目标 Release notes。
3. 获取并校验目标 Release 的 Compose 模板，同时保留 `.env` 和有意设置的本地 override。从 `3.0.0` 升级到 `3.2.2` 时必须这样做，因为部署增加了 `remnanode-state`。
4. 修改 `.env` 中的 `REMNANODE_IMAGE` 或内联的 `image:`。
5. pull 并重建容器。
6. 检查 health、Panel 和代表性流量。

```bash
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode-lite
```

`latest` 和 `preview` 不会自动更新运行容器，`docker compose restart` 也不会 pull。回滚时恢复之前记录的精确 tag 或 digest，再重复 pull/recreate。

## Native 更新、回滚与修复

Native 只接受精确版本：

```bash
sudo rnlctl upgrade --to 2.8.0-rnl.2 --dry-run
sudo rnlctl upgrade --to 2.8.0-rnl.2 --dry-run --json
sudo rnlctl upgrade --to 2.8.0-rnl.2
sudo rnlctl rollback
```

dry-run 需要 root、已有且状态干净的安装，并且不能存在待处理的生命周期 journal。使用 `--to` 时，它会完整下载并静态校验候选，然后短暂持有生命周期锁，检查当前状态和已知宿主机前置条件。它不会创建 generation、cache 或事务 journal，不会修改服务、执行候选二进制、运行目标健康检查或保留 bundle。`--json` 只能与 `--dry-run` 同时使用。检查会使用临时磁盘，但不会预留或保证真实升级的空间；预检成功也不代表后续升级一定成功。本地 `--bundle` 加 `--sha256` 和 `--bundle-root` 同样支持 dry-run。

普通文本模式的预检和升级会报告精确 Release 选择、下载、校验以及实际执行的生命周期阶段。
只有总大小已知的下载才显示百分比和 ETA；JSON dry-run 从不包含进度输出。

升级把完整 Node/runtime bundle 作为新 generation，并把旧 generation 保留为 previous。若状态显示 `recovery-required`，先检查具体问题；仅当状态列出可读的 `pending` 事务时运行 repair，否则先用 doctor 检查不可读的生命周期元数据。不要手工修改链接或状态文件：

```bash
sudo rnlctl status --json
sudo rnlctl repair
sudo rnlctl doctor
```

repair 使用已验证的缓存恢复已提交版本，绝不会自动升级。生命周期变更共用 `/run/remnanode-lite-installer/operation.lock`；等待当前操作完成，不要删除 lock 或 `/var/lib/remnanode-lite-installer/journal.json` 强行并发。

## 修改配置

Docker 修改 `.env` 或 Compose mapping 后重新校验并创建容器。Native 的运行参数只以 `/etc/remnanode-lite/node.env` 为准；`rnlctl config` 直接读取和修改它，只显示 6 个不含 Secret 的管理员字段，不会显示 Secret 或内部受管字段。

查看、校验和修改 active 安装：

```bash
sudo rnlctl config show
sudo rnlctl config check
sudo rnlctl config set NODE_PORT=2222 --apply
```

`config show` 和 `get` 展示的是 `node.env` 中保存的值，不是程序计算后的默认值。`config check` 只读，并检查受管 `node.env`/`secret.key` 的权限。`set` 和 `unset` 会先校验完整候选配置再写入；加上 `--apply` 后会重启 active 服务并等待内部健康。如果修改后失败，命令会尽力尝试恢复旧文件和服务；这不是持久化或崩溃安全的事务。stopped 或 prepared 安装会在写文件前拒绝 `--apply`，此时先不带 `--apply` 修改，再分别运行 `rnlctl start` 或 `rnlctl activate`。

手工编辑仍受支持：保持 `root:remnanode-lite 0640`，运行 `rnlctl config check`，active 服务再运行 `rnlctl config apply`。由于没有保存手工编辑前的快照，`config apply` 无法回滚手工改动。

Secret 只从受保护的普通文件轮换，不接受直接把值作为参数，也不会打印：

```bash
sudo rnlctl secret set --file /root/new-node-secret.key --apply
```

操作后自行删除源文件。Secret 修改与 `config set --apply` 有相同的 active 状态限制和尽力恢复行为。完整步骤见 [Native 部署](deployment-native.md#修改端口或-secret)。`config check` 与 `config apply` 都不会连接 Panel 或测试代理流量，仍需另外确认 Panel 状态和代表性流量。修改 `NODE_PORT` 时同步更新 Panel 与宿主机防火墙。两种部署都使用 host networking，没有端口转换层。

## 资源检查

维护的 Docker 与 Native 配置限制为 `448 MiB RAM`、不额外使用 swap、`1 CPU`、`256 PIDs/tasks`。整机 `512 MiB / 1 vCPU / 2 GB` 是工程目标，不保证任意用户数和协议组合。

Docker：

```bash
docker stats --no-stream remnanode-lite
docker inspect remnanode-lite --format \
  'oom={{.State.OOMKilled}} restarts={{.RestartCount}}'
docker system df
df -h
```

systemd：

```bash
systemctl show remnanode-lite.service \
  --property=MemoryCurrent,MemoryPeak,TasksCurrent,CPUUsageNSec
journalctl --disk-usage
df -h
```

在 Alpine/OpenRC 服务路径上，服务会在检测到的统一 cgroup v2 根目录下创建 `openrc.remnanode-lite`。启动前会核验 `memory.max=469762048`、`memory.swap.max=0`、`cpu.max=100000 100000`、`pids.max=256`、自身的 cgroup 成员关系、可写的父级 `cgroup.procs`，以及可写的服务 `cgroup.kill`。不要在只有 2 GB 磁盘的生产主机上构建项目。

## 网络与安全边界

两种部署都运行在宿主网络命名空间中。`CAP_NET_ADMIN` 用于项目私有 nftables 表和选择性 TCP socket destroy；`CAP_NET_BIND_SERVICE` 允许 rw-core 监听 1024 以下端口。

- 只运行可信精确版本或已验证 digest。
- 不要使用 `privileged: true`、root Native 服务或额外 capability。
- 条件允许时只让 Panel 地址访问 Node API 端口。
- 按 Panel 下发配置开放代理端口。
- 保护 Docker socket、root 权限、Compose 目录和 `/etc/remnanode-lite`。
- 项目只拥有自己的运行时 nftables 表，不拥有宿主机全局 firewall 或 sysctl。

## 常见问题

### `illegal base64 data at input byte 0`

Secret 不是有效 base64/base64url、被截断、含空白，或 Compose list 中的引号进入了值。重新从 Panel 获取完整 Secret，并使用配置文档中的 mapping 格式。

### `SECRET_KEY missing required fields`

值可以解码，但不是完整 Node Secret。JWT、证书或私钥片段都不够。

### `address already in use`

```bash
ss -H -lntp 'sport = :2222'
```

停止冲突服务，或同时修改 Panel、主机配置和防火墙。不要让官方容器与 Lite 使用同一宿主端口。

### 本地 healthy，Panel offline

依次确认端口与 Panel 一致、正确进程持有端口、firewall/路由可达、Secret 属于该 Node、系统时间正确、日志无 TLS/JWT/listen 错误。本地 health 不会覆盖这些外部链路。

### Node online，rw-core offline

读取 core error 日志，检查端口冲突和 Panel 下发配置。低内存模式对大配置允许更长 readiness 时间，不要只凭重启后的几秒钟判定失败。

### `CAP_NET_ADMIN not available`

恢复仓库提供的 capability 配置或运行 repair。不要用 privileged 容器或 root 服务掩盖错误。

### ASN database unavailable

Node 继续运行，但 `asList` 为空。Docker 和 Native bundle 都包含锁定版本的数据库；重建已验证镜像，或执行 `rnlctl repair`/精确版本升级，不要向当前 generation 下载未固定的数据。

### Alpine/OpenRC 宿主机资格检查失败

宿主机使用的 Alpine 版本必须列在 [Native 主机矩阵](deployment-native.md#native-主机矩阵)中，并采用持久化 `sys` 安装和标准 init/OpenRC 启动栈。主机内核不得低于 Linux 5.14，统一 cgroup v2 还需提供可用的 CPU、memory、PID、swap 限制、父级成员迁移和服务级清理控制。`--prepare-only` 不会启动服务，因此成功安装并不能证明这些运行条件已经满足。如果 `activate` 或 `start` 拒绝当前主机，应换用能够提供完整契约的环境，或改用受维护的 systemd 主机或 Docker 部署。不要绕过检查，否则文档中的资源限制和清理行为都无法成立。

### Native 提示需要 repair

保留 `status --json` 用于诊断并运行 `rnlctl repair`。不要手动删除 `/usr/local/lib/remnanode-lite` 或 `/var/lib/remnanode-lite-installer` 中的文件。

## 备份范围

- Docker：Compose、可选 `.env`、当前精确镜像 tag 或 digest。
- Native：`/etc/remnanode-lite/node.env`、`/etc/remnanode-lite/secret.key`、当前精确 Release 版本。
- Fleet：上一已知可用的精确版本或 digest。

按私钥保护 Secret 备份。不要备份 `/run`、Docker tmpfs 日志、Panel 下发的 runtime Xray 配置或 Native generation 目录来替代 Release 和 `rnlctl` 状态。
