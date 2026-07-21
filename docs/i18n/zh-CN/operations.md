<!-- translation: locale=zh-CN; source=docs/operations.md; source-sha256=1dc34590c75273956e2c0278d3932d1c9c3c5668367b164dd86801b858480118 -->

# 运维手册

> 这是中文译文；涉及运维规则时，请以[英文原文](../../operations.md)为准。

[返回文档索引](README.md)

本文介绍节点上线后的日常操作：检查状态、查看日志、更新或回滚，以及排查常见故障。

## 运行状态模型

Remnanode Lite 同时管理两个不同层次的进程状态：

- Node 是长期运行的 HTTPS 服务，负责认证 Panel 请求、统计、插件和 rw-core 生命周期。
- rw-core 只在 Panel 下发 `/node/xray/start` 后运行。

Node 不持久化 Panel 下发的完整 Xray 配置。容器、服务或主机重启后，Node 先上线并报告 core 离线；Panel 健康循环随后重新下发配置。这是与官方 Node 对齐的正常恢复路径。

“进程运行”“容器 healthy”“Panel 在线”和“rw-core online”不是同一状态：

| 层次 | 能证明什么 | 不能证明什么 |
| --- | --- | --- |
| service/container running | Node 主进程仍存在 | 监听端口、认证和 core 状态 |
| Compose `healthy` | PID 1 存在且内部 Unix Socket 已建立 | Panel 可达、mTLS/JWT 或 rw-core online |
| TCP 端口由 Node 持有 | Node 已监听配置端口 | Secret、Panel 网络和认证正确 |
| Panel 节点在线 | Panel 已通过 mTLS/JWT 与 Node 通信 | 所有代理入站端口均可达 |
| Panel 显示 core online | rw-core 已启动且内部 gRPC readiness 通过 | 每个代理协议和外部网络均正常 |

`/node/xray/healthcheck` 是受 mTLS 和 JWT 保护的 Panel API，不是匿名 HTTP 探活端点。不要在外部监控中直接发送普通 curl 请求。

## 日常状态检查

### Docker Compose

```bash
docker compose ps
docker compose logs --tail=100 remnanode
ss -H -lntp 'sport = :38329'
```

将 `38329` 换成实际 `NODE_PORT`。Compose healthcheck 会在容器内运行：

```text
remnanode-lite healthcheck
```

该命令会在 2 秒超时内连接 `INTERNAL_SOCKET_PATH` 对应的 Unix socket，确认 Node 正在接受内部连接，而不只是检查 socket 文件。它不会测试 Panel 网络、mTLS/JWT 或注册状态。如果 Compose 显示 `healthy`，但 Panel 仍显示离线，请继续检查端口、防火墙、Secret 和 Panel 配置。

### systemd

```bash
sudo systemctl --no-pager status remnawave-node
sudo systemctl show remnawave-node \
  --property=ActiveState,SubState,MainPID,MemoryCurrent,TasksCurrent
sudo ss -H -lntp 'sport = :2222'
sudo remnanode-lite doctor
```

### OpenRC

```bash
rc-service remnawave-node status
ss -H -lntp 'sport = :2222'
remnanode-lite doctor
```

`doctor` 检查配置、Secret 格式、rw-core、geo、ASN、nft、ss 和当前进程能力。它不连接 Panel，也不证明 core 已启动。当前实现还会检查 systemd unit，因此 OpenRC 上缺少 systemd unit 的 WARN 可以忽略；ERROR 需要处理。

指定其它原生配置文件：

```bash
sudo remnanode-lite doctor --env /path/to/node.env
```

容器部署通常不使用 `doctor` 作为健康检查：默认镜像没有 `/etc/remnanode/node.env`，而配置来自环境变量。以 Compose health、Node 日志和 Panel 状态为准。

## CLI 命令速查

`remnanode-lite` 不带参数时启动 daemon。其它子命令用于只读诊断、安装器内部校验或明确的管理操作：

| 命令 | 用途 | 注意事项 |
| --- | --- | --- |
| `remnanode-lite version` | 显示项目版本与编译时默认契约版本 | 不解析 `node.env`，也不读取进程环境；可用于二进制 smoke test。daemon 实际上报值仍可由已校验配置中的 `NODE_CONTRACT_VERSION` 覆盖 |
| `remnanode-lite doctor [--env PATH]` | 检查原生配置、Secret、资产、工具和 capability | 不连接 Panel，也不启动 rw-core |
| `remnanode-lite validate-secret` | 从 stdin 校验并规范化 Secret，但不输出内容 | 适合写盘或重启前验证；成功退出码为 0 |
| `remnanode-lite canonicalize-secret <path\|->` | 将规范化 Secret 写到 stdout | 输出仍是完整敏感数据，只能重定向到权限受限文件，不能进入日志 |
| `remnanode-lite kill-sockets` | 交互读取一个 IP，并销毁本地或远端地址匹配该 IP 的 connected TCP socket | 需要 `CAP_NET_ADMIN`；CLI 直接调用内核适配器，不经过业务层的本机地址保护 |
| `remnanode-lite release-url <tag> <arch>` | 生成受校验的 Release 归档 URL | 供安装器使用，tag 和架构不合法时失败 |
| `remnanode-lite install-script-url <tag> <script>` | 生成受校验的安装脚本 URL | 只接受允许的脚本名，主要供 bootstrap 使用 |

查看参数摘要：

```bash
remnanode-lite --help
```

不要在运行中的生产容器里再启动第二个 daemon；进程和 nftables 所有权按单实例设计。

`kill-sockets` 是管理工具，不是健康检查。它会在整个 network namespace 中匹配 local **或** remote address，不按 PID 或容器过滤。输入宿主机本地地址可能关闭无关连接，因此只能在隔离节点上使用，并且必须先确认目标地址不是本机地址。

## 日志

### Node 日志

| 部署方式 | 查看命令 | 保存方式 |
| --- | --- | --- |
| Docker | `docker compose logs -f remnanode` | Docker `json-file`，生产模板为 `2 MiB x 2`。 |
| systemd | `journalctl -u remnawave-node -f` | 由宿主 journald 配额管理。 |
| OpenRC | `tail -F /var/log/remnanode/openrc.log` | 文件，由 Node 每 10 秒检查轮转。 |

systemd 的 `LogRateLimitIntervalSec=30s`、`LogRateLimitBurst=200` 只限制单位时间消息数，不是长期磁盘上限。2 GB 磁盘主机还应合理配置宿主 journald 总配额，并定期检查：

```bash
journalctl --disk-usage
df -h
```

### rw-core 日志

rw-core 的 stdout 和 stderr 分开保存：

```text
/var/log/remnanode/xray.out.log
/var/log/remnanode/xray.err.log
```

Docker：

```bash
docker exec -it remnanode \
  tail -n 50 -F /var/log/remnanode/xray.out.log

docker exec -it remnanode \
  tail -n 50 -F /var/log/remnanode/xray.err.log
```

原生部署：

```bash
remnanode-xlogs
remnanode-xerrors
```

每条 rw-core 日志保留当前文件和一个 `.1` 文件，两者都限制为 4 MiB。正常情况下两条日志合计占用 16 MiB；轮转时临时文件可能短暂再增加约 8 MiB。

Docker 把 `/var/log/remnanode` 放在 28 MiB tmpfs 中，重建容器即可清空，不占用持久磁盘。OpenRC 还会写入 `openrc.log` 和 `openrc.err.log`，每 10 秒检查一次，并在 4 MiB 时执行 copy-truncate，因此文件可能在两次检查之间略微超过阈值。

## 启停和重建

Docker：

```bash
docker compose restart remnanode
docker compose stop remnanode
docker compose up -d --no-build
docker compose down
```

systemd：

```bash
sudo systemctl restart remnawave-node
sudo systemctl stop remnawave-node
sudo systemctl start remnawave-node
```

OpenRC：

```bash
rc-service remnawave-node restart
rc-service remnawave-node stop
rc-service remnawave-node start
```

Node 收到 SIGTERM/SIGINT 后使用共享的 25 秒应用关闭预算：停止接收请求、停止 rw-core 进程组，再清理插件和私有 nft 表。Compose 提供 35 秒 grace，systemd 提供 30 秒外层 timeout，OpenRC 使用 `TERM/30/KILL/5`。不要用 `kill -9` 作为日常重启方式。

## Docker 更新与回滚

### 镜像引用

| 引用 | 特性 | 推荐用途 |
| --- | --- | --- |
| `latest` | 随最新稳定发布移动 | 主动 pull 后统一跟随稳定版的小节点。 |
| `X.Y.Z` | 完成对应官方版本对齐时的项目正式版本 | 固定官方对齐版本。 |
| `X.Y.Z-rnl.N` | 项目自主迭代版本 | 精确部署和问题定位。 |
| `sha-<commit>` | main 候选构建 | 正式发布前服务器验收。 |
| `candidate-sha-<commit>` | 手动触发的独立候选构建 | 自动候选缺失或需要重建时的验收入口。 |
| `name@sha256:<digest>` | Registry 内容寻址 | 最严格的不可变固定和回滚。 |

精确 tag 和 `sha-*` 按项目发布策略不应移动，但 Registry tag 本身不是技术上的不可变对象。需要严格复现时固定 manifest digest。

### 受控更新

1. 记录当前 Compose 和镜像引用。
2. 阅读目标 Release notes，确认契约、rw-core 和配置变化。
3. 修改 `image:` 为新精确 tag 或 digest。
4. 拉取并强制重新创建。
5. 检查容器、端口、日志和 Panel。

```bash
cp -p docker-compose.yaml docker-compose.yaml.rollback

docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode
```

使用 `latest` 时同样必须显式执行 pull/recreate。仅运行 `docker compose restart` 不会检查新镜像。

### 回滚

恢复上一个 Compose，或把 `image:` 改回已验证的精确 tag/digest：

```bash
cp -p docker-compose.yaml.rollback docker-compose.yaml
chmod 600 docker-compose.yaml

docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
```

不要通过覆盖旧 tag 实现回滚。清理前记录一个已验证的旧版本 tag 或 manifest digest，并确认对应镜像仍在本机；始终至少保留这一个明确的回滚镜像：

```bash
docker system df
docker image prune
```

`docker image prune` 默认只删除 dangling image。不要使用会删除所有未运行镜像的批量清理参数，除非已经逐项确认不会删除上述回滚镜像。

不要在 2 GB 生产主机上从源码构建镜像。Go 工具链、基础层和 BuildKit cache 可能明显超过运行时磁盘预算。

## 原生更新与回滚

原生升级由事务脚本完成，不要在运行中手工覆盖二进制。完整命令和事务语义见 [原生 Linux 部署](deployment-native.md#升级)。

日常原则：

- 固定目标 Release tag，不使用分支下载地址。
- 显式 `--upgrade` 默认保留 rw-core；Release notes 要求时才加 `--upgrade-xray`。不要用重复 `--install` 代替，因为完整安装上的 `--install` 默认会同步 rw-core/geo/ASN。
- 升级前 stopped 的服务在显式 upgrade 后仍保持 stopped。
- 只有目标版本进程实际持有配置端口，事务才会提交。
- 失败时先阅读脚本给出的备份目录和回滚结果，不要删除保留的唯一备份。
- 回退只选择真实存在的旧 Release；不兼容时同步恢复对应配置和 core。

安装、升级、rw-core 更新和卸载共用 `/run/lock/remnanode-installer.lock`。看到 installer 正在运行时应等待现有操作结束，不要删除锁文件或并行启动另一个变更入口。

## Secret 和端口变更

### Docker

修改 Compose mapping 并重新创建：

```bash
chmod 600 docker-compose.yaml
docker compose config --quiet
docker compose up -d --no-build --force-recreate
```

不要运行无 `--quiet` 的 `docker compose config`，否则展开后的 Secret 会打印到终端或采集日志。

### 原生部署

先把新 Secret 写入仅当前用户可读的临时文件并校验，再原子替换正式文件：

```bash
umask 077
secret_tmp="$(mktemp)"
printf '%s' '新的完整 Secret Key' >"$secret_tmp"
remnanode-lite validate-secret <"$secret_tmp"

sudo install -o root -g remnanode -m 0640 \
  "$secret_tmp" /etc/remnanode/secret.key.new
sudo mv -f /etc/remnanode/secret.key.new /etc/remnanode/secret.key
rm -f "$secret_tmp"
```

然后检查 `/etc/remnanode/node.env` 的有效赋值。非空 `SECRET_KEY` 的优先级高于 `SECRET_KEY_FILE`；若从旧版内联配置迁移，必须清空前者，并让后者指向刚替换的文件：

```env
SECRET_KEY=
SECRET_KEY_FILE=/etc/remnanode/secret.key
```

同一键有重复赋值时最后一个值生效，因此应删除旧的重复项。确认配置后再检查并重启：

```bash
sudo remnanode-lite doctor
sudo systemctl restart remnawave-node
```

OpenRC 将最后一行替换为：

```bash
rc-service remnawave-node restart
```

如果校验、安装或配置编辑中途失败，先删除本次临时文件，不要重启服务。不要只覆盖 `secret.key` 而保留非空的内联 `SECRET_KEY`，否则 Node 仍会使用旧值。

修改 `NODE_PORT` 后还必须同步 Panel 节点配置和宿主防火墙。host network 下不能依靠 Compose `ports:` 修正端口不一致。

## 资源与磁盘

生产配置面向整机 `512 MiB RAM / 1 vCPU / 2 GB disk`，但这是工程预算，不是任意宿主环境的性能保证。Docker daemon、内核和其它系统服务均在 448 MiB 容器限制之外占用资源。

Docker：

```bash
docker stats --no-stream remnanode
docker system df
df -h
```

systemd：

```bash
systemctl show remnawave-node \
  --property=MemoryCurrent,MemoryPeak,TasksCurrent,CPUUsageNSec
journalctl --disk-usage
df -h
```

OpenRC/cgroup v2：

```bash
service_cgroup=/sys/fs/cgroup/openrc.remnawave-node
cat "${service_cgroup}/memory.current"
cat "${service_cgroup}/memory.peak"
cat "${service_cgroup}/pids.current"
```

不同环境的 cgroup 根也可能是 `/sys/fs/cgroup/unified`。OpenRC service 启动前会验证实际路径和全部资源限制。

## 网络和安全边界

Docker 使用 host network，`CAP_NET_ADMIN` 作用于宿主网络命名空间。它是 nftables 插件和 socket destroy 所需能力，同时意味着容器必须被视为受信任的网络管理组件：

- 只运行本项目发布并验证过的镜像。
- 不使用 `privileged: true`，不要增加无关 capabilities。
- 宿主防火墙只对 Panel 地址开放 Node API 端口。
- 代理入站端口仍需按 Panel 实际下发配置开放。
- 限制 Docker socket 和主机管理员权限；内联 Secret 可通过 Docker inspect 被主机管理员读取。

原生服务使用非 root 用户和相同的两个最小 capability。`CAP_NET_ADMIN` 缺失时 Node 基本连接可能仍正常，但 nftables 插件和连接销毁会降级。

## 常见故障

### `illegal base64 data at input byte 0`

常见原因是 Compose 写成列表形式并把引号带入值：

```yaml
- SECRET_KEY="..."
```

改为 mapping：

```yaml
SECRET_KEY: "..."
```

如果仍失败，重新从 Panel 获取完整 Secret，确认没有前后空格、截断或多行包装。

### `SECRET_KEY missing required fields`

base64 可以解码，但内容不是当前 Panel 节点页提供的完整 Secret JSON。重新生成或复制节点 Secret，不要只提供 JWT、公钥或单个证书。

### `address already in use`

host network 下已有宿主进程占用端口：

```bash
ss -H -lntp 'sport = :38329'
```

停止冲突服务，或同步修改 Node 配置、Panel 节点端口和防火墙。

### 容器 healthy，但 Panel 离线

依次确认：

1. `NODE_PORT` 与 Panel 完全一致。
2. 宿主端口由目标 Node 进程监听。
3. Panel 到节点的防火墙和路由可达。
4. Secret 来自当前节点，且系统时间正确。
5. Node 日志中没有 TLS、JWT 或监听错误。

Compose health 只检查内部 Socket，不覆盖以上链路。

### Node 在线，但 rw-core 离线

刚重启时先等待 Panel 下一次健康循环。若持续离线：

```bash
docker exec -it remnanode \
  tail -n 100 /var/log/remnanode/xray.err.log
```

原生部署使用 `remnanode-xerrors`。检查 rw-core 二进制、geo 数据、端口冲突和 Panel 下发配置。低内存模式允许 readiness 最多等待 90 秒，不应在大配置启动数秒后立即判定失败。

### `CAP_NET_ADMIN not available`

恢复仓库提供的 Compose capabilities 或原生 service 文件并重启。不要为了消除警告改用 `privileged: true`。缺少该能力时 nftables 和 `NETLINK_SOCK_DIAG` socket destroy 不可用。

### `ASN database unavailable`

Node 会继续运行，但插件 `asList` 共享列表为空。Docker 镜像应已包含数据库；原生部署可按目标 Release 重新执行带 `--upgrade-xray` 的升级，或使用经过 SHA-256 校验的 `ASN_DB_URL`/`ASN_DB_SHA256`。

### OpenRC 报告 cgroup controller

确认主机使用 cgroup v2，memory、cpu、pids controller 已委托给 OpenRC，且 service cgroup 的以下值生效：

```text
memory.max=469762048
memory.swap.max=0
cpu.max=100000 100000
pids.max=256
```

不建议绕过校验启动。修复宿主 cgroup 配置，或改用受支持的 Docker/systemd 部署。

### 升级或卸载拒绝继续

脚本在无法可靠确认 service 状态、Node/rw-core 退出、锁所有权或文件安全边界时会保守失败。先处理日志指出的具体状态；不要手工删除 installer lock、事务备份，或在进程仍运行时覆盖文件。

## 备份范围

需要备份的持久配置很少：

- 单文件部署：权限为 `0600` 的 Compose 文件，或 Compose + `.env`。
- 原生部署：`/etc/remnanode/node.env` 和 `/etc/remnanode/secret.key`。
- 回滚记录：当前镜像 digest 或项目 Release tag。

无需备份 `/run/remnanode`、Docker tmpfs 日志或 Panel 下发的 Xray runtime 配置。Secret 备份应使用与其它私钥相同的加密、访问控制和销毁策略。
