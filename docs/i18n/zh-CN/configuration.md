<!-- translation: locale=zh-CN; source=docs/configuration.md; source-sha256=0821228c1d80c814e27d40ea72c767175ac9234efe5514c31550cd4f15a9cd53 -->

# 配置参考

[英文原文](../../configuration.md) · [文档索引](README.md) · [Docker 部署](deployment-docker.md) · [Native 部署](deployment-native.md) · [运维手册](operations.md)

大多数节点只需要两个值：Panel 中为该 Node 配置的端口，以及该 Node 的完整 Secret Key。维护的 Docker 和 Native 模板已经设置好小机器所需的路径和资源策略。

## 配置来源

守护进程先读取一个有界的环境风格数据文件，再用已知且非空的进程环境变量覆盖：

1. 进程显式设置的 `REMNANODE_ENV`；
2. 存在时的 `/etc/remnanode-lite/node.env`；
3. 当前工作目录中的 `.env`。

文件按 `KEY=value` 数据解析，绝不会由 shell 执行。未知键不会产生作用，也不会因为写入文件就进入子进程环境。Native 服务使用干净的启动环境，只保留 `REMNANODE_ENV=/etc/remnanode-lite/node.env` 和必要的身份变量。

Docker Compose 的 `.env` 是另一套机制：Compose 在创建容器前用于 YAML 插值，导出的 shell 变量优先于 `.env`；Compose 文件只传入 `environment` mapping 中明确列出的值，不会把整个 `.env` 注入容器。

## 运行时变量

| 变量 | 必需 | 默认值 | 作用 |
| --- | --- | --- | --- |
| `NODE_PORT` | 是 | 模板使用 `2222` | Panel 访问 Node 的 HTTPS 端口，必须与 Panel 一致 |
| `NODE_BIND_ADDR` | 否 | 空 | 监听的 IPv4/IPv6 地址；空值表示所有本地地址 |
| `SECRET_KEY` | 条件必需 | 空 | Docker 使用的完整 Panel Secret；非空时优先于 `SECRET_KEY_FILE` |
| `SECRET_KEY_FILE` | 条件必需 | 空 | 从普通文件读取完整 Secret；Native 使用 `/etc/remnanode-lite/secret.key` |
| `XRAY_BIN` | 否 | `/usr/local/lib/remnanode-lite/current/lib/rw-core` | 受管 rw-core 可执行文件 |
| `GEO_DIR` | 否 | `/usr/local/lib/remnanode-lite/current/share/xray` | `geoip.dat` 与 `geosite.dat` 所在目录 |
| `LOG_DIR` | 否 | `/var/log/remnanode-lite` | rw-core 输出目录 |
| `ASN_DB_PATH` | 否 | `/usr/local/lib/remnanode-lite/current/share/asn/asn-prefixes.bin` | 插件 `asList` 使用的 ASN 数据库 |
| `INTERNAL_SOCKET_PATH` | 否 | `/run/remnanode-lite/internal.sock` | rw-core 与本地 healthcheck 使用的私有 Unix socket |
| `INTERNAL_REST_TOKEN` | 否 | 启动时随机生成 | 私有 Unix HTTP 服务的 token；通常留空 |
| `DISABLE_HASHED_SET_CHECK` | 否 | `false` | 调试开关；开启后每次 start 都重启 rw-core |
| `LOW_MEMORY` | 否 | 模板为 `1` | 512 MiB 配置：Go 软上限 180 MiB、请求预算 16 MiB、较长 readiness 等待 |
| `BODY_LIMIT_MB` | 否 | 自动 | 请求体预算；低内存模式自动为 16 MiB，否则为 256 MiB |
| `GOMEMLIMIT` | 否 | 自动 | Go runtime 软上限，可用 `KiB/MiB/GiB/TiB` 或 `off` |
| `NODE_CONTRACT_VERSION` | 否 | 编译时 `ContractVersion` | 向 Panel 报告的契约版本，仅用于兼容性调试 |
| `XRAY_CORE_VERSION` | 否 | 从 rw-core 探测 | 无法探测时的调试覆盖值 |

布尔值接受 `true/false`、`1/0`、`yes/no`。`NODE_PORT` 范围为 `1..65535`。`BODY_LIMIT_MB` 接受 `1..1024`，但 `LOW_MEMORY=1` 时不能超过 `16`。空值或 `0` 使用自动值。

`GOMEMLIMIT` 只是 Go runtime 的软上限，不是整个进程或宿主机的 RSS 限制；维护的服务/容器上限仍为 `448 MiB`。

## Panel Secret

Secret 是 Panel 为单个 Node 签发的完整值，包含 mTLS 和 JWT 所需材料。JWT、证书、私钥或截短字符串都不能替代它。

### Docker

把 Secret 写入同目录且权限为 `0600` 的 `.env`：

```env
NODE_PORT=2222
SECRET_KEY=PASTE_THE_COMPLETE_PANEL_SECRET_KEY
```

Compose 推荐 mapping 形式：

```yaml
environment:
  SECRET_KEY: "${SECRET_KEY:?set SECRET_KEY in .env}"
```

不要使用 `- SECRET_KEY="..."` 的 list 形式；引号可能被当作值的一部分，造成 base64 解码失败。Docker 会把注入的环境值写入本地容器元数据，因此要保护 Compose 目录和 Docker socket。

### Native Linux

Native 生命周期将 Secret 与 `node.env` 分开：

```env
SECRET_KEY=
SECRET_KEY_FILE=/etc/remnanode-lite/secret.key
```

安装器验证后以 `root:remnanode-lite 0640` 写入 Secret。安装或激活时使用 `--secret-file`，不要把 Secret 作为命令行参数。节点安装后可用 `rnlctl secret set --file PATH [--apply]` 轮换 Secret；命令只读取有大小限制的普通文件，不接受直接传入 Secret，也不会把它写入输出。

## Compose 插值变量

维护的 Compose 文件只把以下值传入容器：

| 变量 | Compose fallback | 是否传入 Node | 说明 |
| --- | --- | --- | --- |
| `REMNANODE_IMAGE` | 发行模板为精确版本；单文件模板默认 `latest` | 否 | 镜像 tag 或 `name@sha256:...`，生产优先精确版本或 digest |
| `NODE_PORT` | `2222` | 是 | Panel 到 Node 的端口 |
| `NODE_BIND_ADDR` | 空 | 是 | 可选绑定地址 |
| `SECRET_KEY` | 无 | 是 | 缺失或为空时 Compose 插值失败 |
| `LOW_MEMORY` | `1` | 是 | 小机器配置 |
| `DISABLE_HASHED_SET_CHECK` | `false` | 是 | 仅调试 |
| `BODY_LIMIT_MB` | 空 | 是 | 空值使用 daemon 自动值 |
| `GOMEMLIMIT` | 空 | 是 | 空值使用低内存默认值 |

插值优先级是 shell 环境、`.env`、YAML 中的 `${NAME:-fallback}`。运行 `docker compose config --quiet` 可校验模板而不打印展开后的 Secret。

Docker 镜像中的以下路径位于容器私有文件系统中。它们与 Native 路径使用相同的项目名称，但并不属于宿主机布局：

```text
XRAY_BIN=/usr/local/lib/remnanode-lite/rw-core
GEO_DIR=/usr/local/share/remnanode-lite/xray
ASN_DB_PATH=/usr/local/share/remnanode-lite/asn/asn-prefixes.bin
LOG_DIR=/var/log/remnanode-lite
INTERNAL_SOCKET_PATH=/run/remnanode-lite/internal.sock
```

这些路径只属于发布镜像，不会与 Native 宿主机目录冲突。维护的 Compose tmpfs 与日志命令已经与之对应；如有覆盖，必须保持一致。

## Native `node.env`

`/etc/remnanode-lite/node.env` 是 Native 运行参数的唯一事实源。`rnlctl config` 直接读取和修改这个文件，不会另外维护配置文件或数据库。Secret 仍单独保存在 `secret.key`；代理配置则仍以 Panel 下发的内容为准。

模板见 [`deploy/node.env.example`](../../../deploy/node.env.example)：

```env
NODE_PORT=2222
SECRET_KEY=
SECRET_KEY_FILE=/etc/remnanode-lite/secret.key
XRAY_BIN=/usr/local/lib/remnanode-lite/current/lib/rw-core
GEO_DIR=/usr/local/lib/remnanode-lite/current/share/xray
LOG_DIR=/var/log/remnanode-lite
ASN_DB_PATH=/usr/local/lib/remnanode-lite/current/share/asn/asn-prefixes.bin
INTERNAL_SOCKET_PATH=/run/remnanode-lite/internal.sock
LOW_MEMORY=1
```

`rnlctl` 会在安装时重写受管路径键，并拒绝重复的受管赋值。管理员可在同一文件中设置 `NODE_BIND_ADDR`、`BODY_LIMIT_MB` 和 `GOMEMLIMIT`，但不要把受管路径改到系统共用的 Xray 安装。`node.env` 与 Secret 必须是普通、非符号链接文件。

### Native 配置命令

`rnlctl config show` 和 `get` 只会显示以下 6 个允许管理员修改的键：

- `NODE_PORT`
- `NODE_BIND_ADDR`
- `LOW_MEMORY`
- `BODY_LIMIT_MB`
- `GOMEMLIMIT`
- `DISABLE_HASHED_SET_CHECK`

Secret、受管运行时路径赋值、内部 token 和版本覆盖字段都不会通过 `rnlctl config` 暴露，也不能由它修改。`show --json` 的顶层包含 `schemaVersion`、`path`（受管 `node.env` 文件的位置）和 `values`；只有 `values` 保存配置赋值，且仅限上述 6 个键。`show` 和 `get` 展示的是 `node.env` 中保存的值，不是 daemon 计算后的默认值；因此空的可选值在实际运行时仍可能有默认效果。

```bash
sudo rnlctl config show
sudo rnlctl config show --json
sudo rnlctl config get NODE_PORT
```

这些读取命令只解析赋值文件，不能代替完整的配置校验。

`check` 使用与 Node 相同的解析和运行规则校验已安装的文件，包括低内存模式与请求体上限的组合限制，以及受管 `node.env`/`secret.key` 的权限。它不会写文件、重启服务、连接 Panel 或测试代理流量：

```bash
sudo rnlctl config check
```

校验会遵循生命周期状态：prepared 安装在激活前可以暂时没有 Secret；已经安装且处于 stopped 或 active 状态的服务则必须有有效 Secret。

`set` 可以一次写入一个或多个值，`unset` 用于移除可选值。两者都会先校验完整候选配置；确实需要写文件时，会把属主和权限设为 `root:remnanode-lite 0640`：

```bash
sudo rnlctl config set NODE_PORT=2222 LOW_MEMORY=1
sudo rnlctl config unset BODY_LIMIT_MB GOMEMLIMIT
```

通过 `rnlctl config set` 传入的值必须为空，或是一个不含空白和控制字符的单个值。这 6 个可编辑字段都不需要引号；保持单个值可以避免命令的校验规则与服务读取环境文件时的解析规则不一致。

配置和 Secret 变更都需要 root、干净的生命周期状态，并共用生命周期操作锁。不带 `--apply` 时不会重启服务；active 进程会继续使用之前已加载的设置，直到运行 `config apply` 或重启。

只能使用上述 6 个键。`NODE_PORT` 是必填项，不能保持为未设置状态。加上 `--apply` 后，命令会在一次操作中完成校验、写入、重启 active 服务，并等待内部健康检查：

```bash
sudo rnlctl config set NODE_PORT=2222 --apply
```

如果修改后的重启或健康检查失败，`set --apply` 与 `unset --apply` 会尝试恢复原来的 `node.env`、属主权限以及使用旧配置运行的服务。这只是命令进程内的尽力恢复，不是持久化或崩溃安全的事务。服务已停止或安装仍处于 prepared 状态时，带 `--apply` 的命令会在写文件前拒绝执行；此时先不带 `--apply` 修改，再按情况运行 `rnlctl start` 或 `rnlctl activate`。

`rnlctl config apply` 适合 active 安装中已经不带 `--apply` 修改过的文件。它会先校验内容和受管文件权限，再重启并等待私有内部健康检查，但不会测试 Panel 连接或代理流量。手工编辑没有旧快照，因此该命令无法回滚手工改动：

```bash
sudo rnlctl config apply
```

## 修改配置

Docker：

```bash
docker compose config --quiet
docker compose up -d --no-build --force-recreate
```

Native 日常修改优先使用安全操作层：

```bash
sudo rnlctl config set NODE_PORT=2222 --apply
```

仍然可以手工编辑 `/etc/remnanode-lite/node.env`。保持 `root:remnanode-lite 0640`，先运行 `rnlctl config check`；active 安装再运行 `rnlctl config apply`，已停止或 prepared 的安装则分别使用 `rnlctl start` 或 `rnlctl activate`。

Secret 要从受保护的临时文件轮换。使用 `--apply` 时，active 服务会重启并接受内部健康检查；如果修改后失败，命令会尝试恢复旧 Secret 和服务：

```bash
sudo rnlctl secret set --file /root/new-node-secret.key --apply
```

用完后自行删除源文件。服务已停止或安装处于 prepared 状态时，Secret 命令同样不能使用 `--apply`。

修改 `NODE_PORT` 时同时更新 Panel 和宿主机防火墙。Host networking 不会替你做端口转换。

## 维护者变量

`REMNANODE_OFFICIAL_SOURCE`、`REMNANODE_CONTRACT_CA`、`REMNANODE_CONTRACT_CERT`、`REMNANODE_CONTRACT_KEY`、`RNL_ASSET_CACHE_DIR`、`RNL_OFFLINE_BUILD`、`SOURCE_REVISION` 和 `SOURCE_DATE_EPOCH` 只用于构建、契约测试和 CI，不是生产安装器变量。runtime 资产版本与摘要统一锁定在 [`release/runtime-assets.lock.json`](../../../release/runtime-assets.lock.json) 中。
