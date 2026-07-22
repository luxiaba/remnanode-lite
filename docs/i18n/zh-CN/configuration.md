<!-- translation: locale=zh-CN; source=docs/configuration.md; source-sha256=8413f3e87bdafa768f4e28d360fb369e781b1689cd197bf31e57ac759bf08693 -->

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
NODE_PORT=38329
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

安装器验证后以 `root:remnanode-lite 0640` 写入 Secret。安装或激活时使用 `--secret-file`，不要把 Secret 作为命令行参数。

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

## 修改配置

Docker：

```bash
docker compose config --quiet
docker compose up -d --no-build --force-recreate
```

Native：

```bash
sudo rnlctl doctor
sudo rnlctl restart
```

修改 `NODE_PORT` 时同时更新 Panel 和宿主机防火墙。Host networking 不会替你做端口转换。

## 维护者变量

`REMNANODE_OFFICIAL_SOURCE`、`REMNANODE_CONTRACT_CA`、`REMNANODE_CONTRACT_CERT`、`REMNANODE_CONTRACT_KEY`、`RNL_ASSET_CACHE_DIR`、`RNL_OFFLINE_BUILD`、`SOURCE_REVISION` 和 `SOURCE_DATE_EPOCH` 只用于构建、契约测试和 CI，不是生产安装器变量。runtime 资产版本与摘要统一锁定在 [`release/runtime-assets.lock.json`](../../../release/runtime-assets.lock.json) 中。
