<!-- translation: locale=zh-CN; source=docs/configuration.md; source-sha256=e2c12fcc01830d089a6ac33b1086f8a513f7b589fecb55b25223f470232cdeaa -->

# 配置参考

> [!IMPORTANT]
> 英文是唯一权威来源；本页是便于阅读的简体中文翻译。请以[英文原文](../../configuration.md)为准。

[返回文档索引](README.md)

本文说明 Remnanode Lite 的配置来源、优先级和每个配置项的真实用途。配置分为 Node 运行时配置、Docker Compose 插值和安装器参数三类。名称相近不代表由同一个程序消费，不应混在同一张“环境变量”表中理解。

## 来源与优先级

Node 启动时按以下顺序选择配置文件：

1. 启动环境中的 `REMNANODE_ENV` 指定路径。
2. 已存在的 `/etc/remnanode/node.env`。
3. 当前工作目录中的 `.env`。

配置文件先被读取，随后进程环境中已知且非空的变量覆盖文件值。空环境变量不会清除文件值；`SECRET_KEY` 非空时优先于 `SECRET_KEY_FILE`。

systemd/OpenRC 固定使用 `REMNANODE_ENV=/etc/remnanode/node.env`，但不会 source 或导出整份文件。Node 将它作为受限数据文件自行解析，因此未知键和 Secret 不会自动进入进程环境。Docker Compose 则直接将选定的运行变量传入容器。

修改配置后必须重启 Node 或重新创建容器；当前不支持配置热加载。

## Node 运行时配置

下表中的键由 Go 进程直接读取。默认路径对应本项目容器镜像和原生安装器的标准布局。

| 变量 | 必需 | 默认值 | 作用与约束 |
| --- | --- | --- | --- |
| `NODE_PORT` | 是 | 无 | Panel 连接 Node 的 HTTPS 端口。所有启动方式都只接受 `1..65535`，非法值在监听前失败。 |
| `NODE_BIND_ADDR` | 否 | 空 | HTTPS 监听地址。空值表示所有本地地址；多网卡主机可填写 Panel 可达的指定 IP。 |
| `SECRET_KEY` | 条件必需 | 空 | Panel 下发的完整 Secret Key。非空时优先于 `SECRET_KEY_FILE`。 |
| `SECRET_KEY_FILE` | 条件必需 | 空 | 从普通文件读取 Secret。原生部署使用 `/etc/remnanode/secret.key`。 |
| `XRAY_BIN` | 否 | `/usr/local/lib/remnanode/rw-core` | rw-core 可执行文件路径。 |
| `GEO_DIR` | 否 | `/usr/local/share/remnanode/xray` | `geoip.dat`、`geosite.dat` 和可选 zapret 数据目录。 |
| `LOG_DIR` | 否 | `/var/log/remnanode` | rw-core stdout/stderr 日志目录。 |
| `ASN_DB_PATH` | 否 | `/usr/local/share/remnanode/asn/asn-prefixes.bin` | compact ASN 数据库。不可用时 `asList` 共享列表降级为空，其它核心能力继续运行。 |
| `INTERNAL_SOCKET_PATH` | 否 | `/run/remnanode/internal.sock` | Node 与 rw-core 间的 Unix Socket。生产部署通常不应修改。 |
| `INTERNAL_REST_TOKEN` | 否 | 每次启动随机生成 | 内部配置和 webhook token。留空最安全；固定值主要用于受控调试。 |
| `DISABLE_HASHED_SET_CHECK` | 否 | `false` | 为 true 时不再用配置 hash 跳过无变化启动，每次 start 都会重启 core。仅用于调试。 |
| `LOW_MEMORY` | 否 | `false` | 启用低内存策略。生产 Compose 默认开启；原生安装器在整机内存不超过 512 MiB 时自动开启。 |
| `BODY_LIMIT_MB` | 否 | `0`（自动） | 公开 `/node` HTTPS server 的额外请求体上限，允许 `0..1024`。低内存模式下显式值不能超过 16；内部 Unix webhook 固定为 8 KiB。 |
| `GOMEMLIMIT` | 否 | 空 | Go runtime 管理内存的软上限。支持纯字节数、`B/KiB/MiB/GiB/TiB` 和 `off`；显式值优先于低内存默认值。 |
| `NODE_CONTRACT_VERSION` | 否 | 编译时契约版本 | 覆盖向 Panel 上报的 `nodeVersion`。只用于契约调试或紧急兼容验证。 |
| `XRAY_CORE_VERSION` | 否 | 探测实际二进制 | 覆盖上报的 rw-core 版本。它不会安装、升级或校验对应二进制。 |

布尔值不区分大小写，接受 `true/false`、`1/0` 或 `yes/no`。非法布尔值、数值或版本字符串会使 Node 在监听前退出，而不是静默回退。

### 低内存模式

`LOW_MEMORY=1` 同时改变以下运行边界：

| 项目 | 普通模式 | 低内存模式 |
| --- | ---: | ---: |
| Go 软内存上限 | Go 默认策略 | 180 MiB |
| Node API TCP 连接上限 | 128 | 16 |
| 活动 HTTP handler | 32 | 4 |
| 自动请求体上限 | 256 MiB | 16 MiB |
| rw-core readiness 等待 | 20 秒 | 90 秒 |

这些值不是容器或 cgroup hard limit。`GOMEMLIMIT` 约束 Go runtime 管理的内存，不等同于仅限制 heap 或整个进程 RSS；rw-core、Go 进程的非 runtime 内存、tmpfs 和其它内存仍共同计入 Compose/systemd/OpenRC 的 448 MiB 总限制。

公开路由还有更小的逐路由上限。即使把 `BODY_LIMIT_MB` 设置得更大，当前单个公开请求的有效上限也不会超过 16 MiB。

### Secret Key

Secret 是 base64 或 base64url 编码 JSON，支持有或无 padding，编码内容最大 256 KiB。解码后必须包含：

```text
caCertPem
jwtPublicKey
nodeCertPem
nodeKeyPem
```

原生部署推荐保存为独立文件：

```bash
sudo install -d -o root -g remnanode -m 0750 /etc/remnanode
printf '%s' '粘贴完整 Secret Key' \
  | sudo tee /etc/remnanode/secret.key >/dev/null
sudo chown root:remnanode /etc/remnanode/secret.key
sudo chmod 0640 /etc/remnanode/secret.key
```

Secret 文件必须是普通非符号链接文件。内容可以没有换行，或只带一个 LF/CRLF 结尾；内部空白会被拒绝。

Docker 单文件部署应使用 YAML mapping：

```yaml
environment:
  NODE_PORT: "38329"
  SECRET_KEY: "粘贴完整 Secret Key"
  LOW_MEMORY: "1"
```

不要使用 `- SECRET_KEY="..."` 列表写法。列表中的引号会成为变量值的一部分，导致 base64 解码失败。包含 Secret 的 Compose 或 `.env` 文件应设置为 `0600`，且不得提交到 Git。

## node.env 语法与边界

`node.env` 使用受限 dotenv 语法，而不是 shell 脚本：

```env
NODE_PORT=2222
SECRET_KEY=
SECRET_KEY_FILE=/etc/remnanode/secret.key
LOW_MEMORY=1
BODY_LIMIT_MB=
```

解析规则：

- 允许空行、以 `#` 开头的注释和可选的 `export KEY=value` 前缀。
- 值可以不加引号，也可以使用一对单引号或双引号。
- 不执行命令、变量展开或 shell substitution。
- 同一键重复出现时最后一个值生效；安装器会合并其管理的重复键。
- 文件最多 1 MiB、4096 行和 256 个赋值。
- Linux 上使用 `O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC` 打开，并在同一文件描述符上比较读取前后状态。
- 未知键计入文件限制，但不会进入 Node 配置或自动传给 rw-core。

原生标准文件的所有者和权限为 `root:remnanode 0640`。

### 原生低内存示例

```env
NODE_PORT=2222
NODE_BIND_ADDR=
SECRET_KEY=
SECRET_KEY_FILE=/etc/remnanode/secret.key

XRAY_BIN=/usr/local/lib/remnanode/rw-core
GEO_DIR=/usr/local/share/remnanode/xray
LOG_DIR=/var/log/remnanode
ASN_DB_PATH=/usr/local/share/remnanode/asn/asn-prefixes.bin
INTERNAL_SOCKET_PATH=/run/remnanode/internal.sock
INTERNAL_REST_TOKEN=

DISABLE_HASHED_SET_CHECK=false
LOW_MEMORY=1
BODY_LIMIT_MB=
```

一般不需要再写 `GOMEMLIMIT=180MiB`，因为 `LOW_MEMORY=1` 已提供相同的 Go 默认软限制。只有完成资源测量后才应覆盖它。

## Docker Compose 插值

仓库根目录的 `.env.example` 服务于 Compose CLI。它不是容器内的 `node.env`，也不会被 Go 进程自行发现。

| 变量 | 消费方 | 说明 |
| --- | --- | --- |
| `REMNANODE_IMAGE` | Compose | 镜像 tag 或 `name@sha256:...`，不会传入 Node。 |
| `NODE_PORT` | Compose -> Node | 传入容器运行时。 |
| `NODE_BIND_ADDR` | Compose -> Node | 传入容器运行时。 |
| `SECRET_KEY` | Compose -> Node | 传入容器环境，会出现在本机 Docker 元数据中。 |
| `LOW_MEMORY` | Compose -> Node | 生产模板默认 `1`。 |
| `DISABLE_HASHED_SET_CHECK` | Compose -> Node | 生产模板默认 `false`。 |
| `BODY_LIMIT_MB` | Compose -> Node | 留空使用低内存默认值。 |
| `GOMEMLIMIT` | Compose -> Node | 留空由 `LOW_MEMORY` 决定。 |

大量独立节点可以完全不创建 `.env`，直接把运行变量写入 Compose 的 `environment` 映射。请从受维护的 [`deploy/compose.single-file.yaml`](../../../deploy/compose.single-file.yaml) 开始，并遵循 [Docker Compose 部署](deployment-docker.md)。

`latest` 只改变下次 pull 解析到的镜像，不会自动替换运行容器：

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

## 安装器和升级器配置

下列变量由 shell 脚本消费，不属于 daemon runtime。部分资产设置可以保存在 `node.env`，供下次 `install-xray.sh` 使用。

| 变量/选项 | 消费方 | 作用 |
| --- | --- | --- |
| `RNL_REPO` | 所有安装脚本 | Release 来源仓库，默认 `luxiaba/remnanode-lite`。 |
| `RNL_TAG` | 安装/升级/卸载 | 精确 tag，例如 `vX.Y.Z` 或 `vX.Y.Z-rnl.N`。 |
| `RNL_INSTALL_XRAY=0` / `--skip-xray` | install | 全新安装时跳过 rw-core。常规生产安装不建议使用。 |
| `RNL_UPGRADE_XRAY=1` / `--upgrade-xray` | upgrade | 同步升级 rw-core/geo/ASN。默认保留现有 rw-core。 |
| `RNL_INSTALL_ASN=0` | install-xray | 跳过 ASN 数据库；`asList` 降级为空。 |
| `RNL_TMP_ROOT` | installer | 高级选项，覆盖 root-only 事务目录；默认 `/var/lib/remnanode-installer`。 |
| `CUSTOM_CORE_URL` | install-xray | 自定义 Linux core 二进制 HTTPS URL。 |
| `CUSTOM_CORE_SHA256` | install-xray | 自定义 core 的必需 SHA-256。 |
| `ASN_DB_URL` | install-xray | 自定义 RWASNDB HTTPS URL。 |
| `ASN_DB_SHA256` | install-xray | 自定义 ASN 数据库的必需 SHA-256。 |
| `GEO_ZAPRET_FILE` | install | 将本地文件原子复制为 `geo-zapret.dat`。 |
| `IP_ZAPRET_FILE` | install | 将本地文件原子复制为 `ip-zapret.dat`。 |
| `XRAY_CORE_VERSION` / `--version` | install-xray | 选择 rw-core Release。非项目固定版本还必须提供 SHA-256。 |
| `XRAY_CORE_SHA256` / `--sha256` | install-xray | 非固定 rw-core Release 的必需摘要。 |

`RNL_ENSURE_SERVICE_STARTED`、`RNL_ENSURE_SERVICE_ENABLED` 和 `RNL_EXTERNAL_ASSET_ROLLBACK` 属于安装器内部事务协议，不应手工设置。

自定义 core 仍使用所选 rw-core Release 中的 geo 数据，然后以已校验的自定义二进制替换 core。所有自定义 URL 必须同时提供 SHA-256；缺少摘要时会在写入目标路径前失败。

入口语义需要特别区分：在完整安装上重复执行 `install-node*.sh --install` 会默认同步目标 Release 的 rw-core/geo/ASN；显式 `--upgrade` 和直接运行 `upgrade.sh` 默认保留这些资产，只有 `--upgrade-xray` 或 `RNL_UPGRADE_XRAY=1` 才同步。

## 版本配置不是版本实现

项目版本、官方契约版本和 rw-core 版本是三个独立概念：

- 项目版本决定 Release、二进制和镜像 tag。
- 契约版本表示当前实际实现并向 Panel 报告的官方 Node API 基线。
- rw-core 版本表示实际捆绑或安装的 core。

不要根据项目版本推断契约版本，也不要通过 override 提前伪报兼容性。查看当前二进制声明：

```bash
remnanode-lite version
```

## 下一步

- Docker 配置：[Docker Compose 部署](deployment-docker.md)
- systemd/OpenRC：[原生 Linux 部署](deployment-native.md)
- 健康、日志和故障处理：[运维手册](operations.md)
