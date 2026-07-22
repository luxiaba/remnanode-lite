<!-- translation: locale=zh-CN; source=docs/operations.md; source-sha256=5016f5e4ec1b7e7e3197d194941dea06af2efa3b719be96dbe6e3aa17aeb2e68 -->

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
ss -H -lntp 'sport = :38329'
```

Native：

```bash
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl logs node --lines 100
sudo rnlctl logs core-errors --lines 100
ss -H -lntp 'sport = :38329'
```

`status --json` 会给出 current/previous generation、版本、服务管理器、启用与活动状态、repair 能力和待处理操作。状态为 degraded 或 recovery-required 时返回非零。 `doctor` 会校验 manifest、文件摘要、链接、配置、Secret、服务、内部 health 和修复缓存，但不会连接 Panel 或制造代理流量。

底层服务视图：

```bash
sudo systemctl --no-pager --full status remnanode-lite.service
sudo systemctl show remnanode-lite.service \
  --property=ActiveState,SubState,MainPID,MemoryCurrent,MemoryPeak,TasksCurrent

# OpenRC（实验性）
sudo rc-service remnanode-lite status
```

## 日志

| 部署 | Node 日志 | 存储 |
| --- | --- | --- |
| Docker | `docker compose logs -f remnanode-lite` | Docker `json-file`，维护模板为 `2 MiB x 2` |
| Native systemd | `sudo rnlctl logs node --follow` | 宿主 journald 策略 |
| Native OpenRC | `sudo rnlctl logs node --follow` | `/var/log/remnanode-lite/openrc.log` 与 `.err.log` |

小型 systemd 主机应为 journald 设置合理的宿主机配额，并监控 `journalctl --disk-usage` 与 `df -h`。

Docker 的 rw-core 使用容器私有路径：

```bash
docker exec -it remnanode-lite \
  sh -c 'tail -n 50 -F "$LOG_DIR/xray.out.log" "$LOG_DIR/xray.err.log"'
```

Native 使用：

```bash
sudo rnlctl logs core --follow
sudo rnlctl logs core-errors --follow
```

Native 文件位于 `/var/log/remnanode-lite/xray.out.log` 和 `xray.err.log`。每条流保留当前文件和一个 `.1`，阈值为 4 MiB。Docker 把 core 日志目录放在 28 MiB tmpfs，重建容器即可清空。

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
3. 修改 `.env` 中的 `REMNANODE_IMAGE` 或内联的 `image:`。
4. pull 并重建容器。
5. 检查 health、Panel 和代表性流量。

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
sudo rnlctl upgrade --to 2.8.0-rnl.2
sudo rnlctl rollback
```

升级把完整 Node/runtime bundle 作为新 generation，并把旧 generation 保留为 previous。若状态显示 `recovery-required`：

```bash
sudo rnlctl status --json
sudo rnlctl repair
sudo rnlctl doctor
```

repair 使用已验证的缓存恢复已提交版本，绝不会自动升级。生命周期变更共用 `/run/remnanode-lite-installer/operation.lock`；等待当前操作完成，不要删除 lock 或 `/var/lib/remnanode-lite-installer/journal.json` 强行并发。

## 修改配置

Docker 修改 `.env` 或 Compose mapping 后重新校验并创建容器。Native 保持 `/etc/remnanode-lite/node.env` 与 `secret.key` 为 `root:remnanode-lite`、服务不可写，然后运行：

```bash
sudo rnlctl doctor
sudo rnlctl restart
```

Secret 轮换要原子替换 `/etc/remnanode-lite/secret.key`，见 [Native 部署](deployment-native.md#修改端口或-secret)。修改 `NODE_PORT` 时同步更新 Panel 与宿主机防火墙。两种部署都使用 host networking，没有端口转换层。

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

OpenRC 的 cgroup 为 `/sys/fs/cgroup/openrc.remnanode-lite`（位于检测到的 cgroup v2 root 下），启动时校验 memory、swap、CPU 和 PID 限制。不要在只有 2 GB 磁盘的生产主机上构建项目。

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
ss -H -lntp 'sport = :38329'
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

### OpenRC cgroup 检查失败

修复 cgroup v2 delegation，或改用受支持的 systemd/Docker。不要跳过资源检查。

### Native 提示需要 repair

保留 `status --json` 用于诊断并运行 `rnlctl repair`。不要手动删除 `/usr/local/lib/remnanode-lite` 或 `/var/lib/remnanode-lite-installer` 中的文件。

## 备份范围

- Docker：Compose、可选 `.env`、当前精确镜像 tag 或 digest。
- Native：`/etc/remnanode-lite/node.env`、`/etc/remnanode-lite/secret.key`、当前精确 Release 版本。
- Fleet：上一已知可用的精确版本或 digest。

按私钥保护 Secret 备份。不要备份 `/run`、Docker tmpfs 日志、Panel 下发的 runtime Xray 配置或 Native generation 目录来替代 Release 和 `rnlctl` 状态。
