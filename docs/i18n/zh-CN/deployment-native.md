<!-- translation: locale=zh-CN; source=docs/deployment-native.md; source-sha256=791dbf53538385f44345c2103197c965d186b14458f462db70c57feff757e8b4 -->

# 原生 Linux 部署

> 这是中文译文；涉及部署规则时，请以[英文原文](../../deployment-native.md)为准。

[返回文档索引](README.md)

本文说明如何使用 GitHub Release 二进制，在 systemd 或 OpenRC 主机上安装 Remnanode Lite。原生部署可以省去 Docker daemon 的开销，并由宿主机服务管理器直接运行 Node。如果机器已经安装 Docker，[Docker Compose](deployment-docker.md) 仍是更简单的选择。

二进制名称是 `remnanode-lite`。原生安装继续使用 `remnawave-node` 服务名，以兼容已有的升级、监控和服务管理命令。

## 支持边界

Release 二进制支持 Linux `amd64` 和 `arm64`。安装器支持 systemd 与 OpenRC；开发期间实际记录过以下安装：

| 平台 | 服务管理器 | 架构 |
| --- | --- | --- |
| Ubuntu 24.04 | systemd | arm64 |
| Alpine 3.22 | OpenRC | arm64 |

CI 会交叉构建两种架构，并在 Ubuntu 上运行 Linux 网络管理测试。`v2.8.0` 的阻断性生产 smoke 覆盖真实小内存 `linux/amd64` 主机上的 Docker；原生 systemd/OpenRC 安装、真实 `arm64` 运行、5 万用户负载、24 小时持续运行，以及故障和回滚注入仍是后续验证。大规模部署前，请先在目标发行版上测试原生安装。Debian/Ubuntu 之外的系统还需要提前安装脚本所需命令。

目标 tag 必须已经发布 GitHub Release，并包含二进制归档、support 文件、`SHA256SUMS` 和 ASN 数据库。`edge` 或 `sha-*` GHCR 候选镜像不能替代原生 Release 资产。

## 前置条件

- root 权限。
- Linux amd64 或 arm64。
- Panel 中已创建节点，并取得完整 Secret Key。
- Panel 配置的 Node 端口与主机 `NODE_PORT` 一致。
- 正确的系统时间和可用网络。
- 首次安装或同步 rw-core 前建议至少保留 1 GiB 可用磁盘；安装器会按下载、解压、目标 staging 和已有备份逐文件系统计算实际预算。
- bootstrap 前已经安装 Bash、curl 和 util-linux 的 `flock`。
- 宿主防火墙允许 Panel 访问 Node API 端口，并按实际代理配置开放入站端口。

systemd/OpenRC 模板都把服务限制为 `448 MiB RAM / 0 swap / 1 CPU / 256 tasks`。OpenRC 额外要求 cgroup v2 的 memory、cpu 和 pids controller 可写且实际生效；缺少任一 controller 时服务拒绝启动。

### Bootstrap 依赖

Ubuntu/Debian：

```bash
sudo apt-get update
sudo apt-get install --yes curl util-linux
```

Alpine：

```bash
apk add --no-cache bash curl util-linux
```

安装器随后会补齐 ca-certificates、tar、unzip、iproute2 和 nftables 等运行依赖。

## systemd 安装

先选择一个已经发布的精确 tag。项目允许纯正式版本和自主迭代版本：

```bash
release_tag='vX.Y.Z-rnl.N' # 或 vX.Y.Z
```

交互安装会询问端口和 Secret：

```bash
curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${release_tag}/scripts/install-node.sh" \
  | sudo env RNL_TAG="${release_tag}" bash
```

非交互安装推荐通过受限文件传入 Secret，避免把它留在 shell history：

```bash
umask 077
printf '%s' '粘贴 Panel 提供的完整 Secret Key' > /tmp/remnanode-secret.key

curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${release_tag}/scripts/install-node.sh" \
  | sudo env RNL_TAG="${release_tag}" bash -s -- \
      --install --yes --port 2222 --secret-file /tmp/remnanode-secret.key

rm -f /tmp/remnanode-secret.key
```

安装完成后检查：

```bash
sudo systemctl --no-pager status remnawave-node
sudo ss -H -lntp 'sport = :2222'
sudo remnanode-lite doctor
```

## Alpine/OpenRC 安装

Alpine 使用专用入口：

```bash
release_tag='vX.Y.Z-rnl.N' # 或 vX.Y.Z

curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${release_tag}/scripts/install-node-alpine.sh" \
  | env RNL_TAG="${release_tag}" bash
```

非交互参数与 systemd 入口相同：

```bash
curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${release_tag}/scripts/install-node-alpine.sh" \
  | env RNL_TAG="${release_tag}" bash -s -- \
      --install --yes --port 2222 --secret-file /root/remnanode-secret.key
```

安装完成后检查：

```bash
rc-service remnawave-node status
ss -H -lntp 'sport = :2222'
remnanode-lite doctor
```

`doctor` 当前包含 systemd unit 检查，因此 OpenRC 上出现“systemd unit 未找到”的 WARN 属于预期；ERROR 才影响退出码和核心结论。Panel 端到端连通仍需在 Panel 中确认。

## 安装参数

两个入口提供相同的常用参数：

| 参数 | 说明 |
| --- | --- |
| `--install` | 全新安装。检测到完整安装时改走可回滚升级，并默认同步目标 Release 的 rw-core/geo/ASN；加 `--skip-xray` 才保留现有资产。 |
| `--upgrade` | 显式升级 Node/service/support，默认保留 rw-core。 |
| `--uninstall` | 打开卸载流程。 |
| `--yes`, `-y` | 跳过确认。没有 Secret 时安装完成但不启动服务。 |
| `--dry-run` | 预览操作，不修改系统。 |
| `--skip-xray` | 不安装 rw-core。仅适合已自行准备兼容 core 的高级场景。 |
| `--low-memory` | 强制写入 `LOW_MEMORY=1`。小内存节点建议开启。 |
| `--port PORT` | Node HTTPS 端口，范围 `1..65535`，默认 2222。 |
| `--secret-file PATH` | 从普通文件安全读取、规范化并验证 Secret。 |

整机 `MemTotal <= 512 MiB` 时，安装器会自动启用低内存模式。已经存在 `node.env` 时，未明确传入端口或低内存选项的配置会保留。

## 安装流程

安装器执行以下操作：

1. 获取全局 installer 锁，拒绝并发安装、升级、rw-core 更新或卸载。
2. 检查架构、磁盘预算和基础命令。
3. 创建专用 `remnanode:remnanode` 系统账号与受限目录。
4. 下载目标 Release 的 `SHA256SUMS` 和架构归档，验证摘要、结构与二进制版本。
5. 安装 Node、support、固定 rw-core、geo 和 compact ASN 数据库。
6. 验证并保存 Secret，安装 service 文件与日志辅助命令。
7. 启动服务，确认唯一目标 Node 进程实际持有配置的 TCP 端口。

在完整安装上再次执行 `--install`，会进入事务升级流程，并刷新目标 Release 的 rw-core、geo 和 ASN。显式 `--upgrade` 默认保留这些资产；需要刷新时再加 `--upgrade-xray`。如果只发现部分安装文件，脚本会进入恢复流程，而不是把它当作正常升级。

## 文件布局

| 路径 | 所有者/用途 |
| --- | --- |
| `/usr/local/bin/remnanode-lite` | Node 主程序。 |
| `/usr/local/bin/remnanode-xlogs` | 跟随 rw-core stdout。 |
| `/usr/local/bin/remnanode-xerrors` | 跟随 rw-core stderr。 |
| `/etc/remnanode/node.env` | `root:remnanode 0640`，运行时配置。 |
| `/etc/remnanode/secret.key` | `root:remnanode 0640`，Panel Secret。 |
| `/usr/local/lib/remnanode/rw-core` | 项目私有 rw-core。 |
| `/usr/local/lib/remnanode/support/<tag>` | 与已安装 Release 匹配的 service/installer support。 |
| `/usr/local/lib/remnanode/support-current` | 指向当前 support 的受控符号链接。 |
| `/usr/local/share/remnanode/xray` | geo 和可选 zapret 数据。 |
| `/usr/local/share/remnanode/asn/asn-prefixes.bin` | compact ASN 数据库。 |
| `/var/lib/remnanode` | 服务工作目录。Node 不在这里持久化 Panel Xray 配置。 |
| `/var/log/remnanode` | rw-core 日志；OpenRC 还保存 supervisor 日志。 |
| `/run/remnanode` | 重启即清空的 Unix Socket 目录。 |
| `/var/lib/remnanode-installer` | root-only 下载、解压和事务目录。 |
| `/run/lock/remnanode-installer.lock` | 所有变更型安装入口共享的锁。 |

项目不会接管或删除通用 `/usr/local/bin/xray`、`/usr/local/share/xray`。

## 服务安全模型

原生服务不以 root 运行。systemd 和 OpenRC 都使用专用 `remnanode` 用户，只授予：

- `CAP_NET_ADMIN`：管理项目 nftables 表和执行 `NETLINK_SOCK_DIAG` socket destroy。
- `CAP_NET_BIND_SERVICE`：允许 rw-core 监听 1-1023 端口。

systemd 还启用 capability bounding、`NoNewPrivileges`、只读系统目录、namespace/syscall/address-family 限制和私有临时目录。OpenRC 使用 `supervise-daemon`、`no_new_privs` 和 cgroup v2 限额。

`node.env` 不由服务管理器导出。Node 启动 rw-core 前会过滤 Panel Secret、Secret 文件路径和 Node 配置文件路径，并注入 core 所需的资产路径与内部 token。

## 服务管理

systemd：

```bash
sudo systemctl status remnawave-node
sudo systemctl restart remnawave-node
sudo systemctl stop remnawave-node
sudo journalctl -u remnawave-node -f
```

OpenRC：

```bash
rc-service remnawave-node status
rc-service remnawave-node restart
rc-service remnawave-node stop
tail -F /var/log/remnanode/openrc.log
```

rw-core 日志在两种平台上都可以使用：

```bash
remnanode-xlogs
remnanode-xerrors
```

服务重启后 Node 会先报告 rw-core 离线，等待 Panel 重新下发 start。这是预期行为，不表示本地配置丢失或服务启动失败。

## 升级

选择目标 Release tag：

```bash
target_tag='vX.Y.Z-rnl.N' # 或 vX.Y.Z

curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${target_tag}/scripts/upgrade.sh" \
  | sudo env RNL_TAG="${target_tag}" bash -s -- --yes
```

默认只升级 Node、service 和 support，保留现有 rw-core。目标 Release 明确要求同步 core，或需要刷新 geo/ASN 时：

```bash
curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${target_tag}/scripts/upgrade.sh" \
  | sudo env RNL_TAG="${target_tag}" bash -s -- --yes --upgrade-xray
```

升级事务会：

1. 记录服务运行状态；由 install 委托时还记录开机启用状态。
2. 备份 binary、service、support、`node.env`、`secret.key` 和可选 rw-core/geo/ASN。
3. 停止并确认 Node 与配置指向的 rw-core 已全部退出。
4. 原子替换目标文件并迁移受支持的旧配置。
5. 只在升级前运行中或 install 委托要求启动时恢复运行。
6. 验证二进制版本，并确认唯一目标进程实际持有配置端口后提交。

显式升级不会启动原本已经停止的服务。如果验证失败，脚本会尝试恢复原文件、开机注册和服务状态。回滚无法完成时，备份会保留在仅 root 可访问的 installer 目录中，脚本以错误退出。

修改 `node.env` 或 Secret 不需要重新安装。按 [配置参考](configuration.md) 修改权限正确的文件后重启服务即可。

## 回退到旧版本

只使用项目确实发布过的旧 tag：

```bash
old_tag='vX.Y.Z-rnl.N' # 或 vX.Y.Z

curl -fsSL \
  "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${old_tag}/scripts/upgrade.sh" \
  | sudo env RNL_TAG="${old_tag}" bash -s -- --yes
```

如果旧版本要求对应 rw-core，追加 `--upgrade-xray`。回退前先阅读两个版本的 Release notes，确认配置和契约基线兼容。

## 卸载

优先使用当前安装随附的 support 脚本：

```bash
sudo bash /usr/local/lib/remnanode/support-current/scripts/uninstall.sh
```

非交互模式：

| 模式 | 命令 | 保留内容 |
| --- | --- | --- |
| 保留配置 | `--keep-config --yes` | `node.env`、Secret、日志、数据、rw-core/geo/ASN。 |
| 清除运行数据 | `--purge --yes` | 保留 rw-core/geo/ASN。 |
| 完全卸载项目资产 | `--full` | 不保留项目配置、日志、数据、rw-core/geo/ASN。 |
| 预览 | 加 `--dry-run` | 不实际修改。 |

卸载只有在确认 service manager 已停止，且目标 Node/rw-core 进程全部退出后才删除文件。它还会清理项目私有 nftables 表，但不会终止无关的同名进程或删除通用 Xray 路径。

即使执行 `--full`，以下系统状态仍会保留：

- `remnanode` 系统用户和组。
- 安装器安装的通用系统软件包。
- `/var/lib/remnanode-installer` 的 root-only marker 目录。

保留这些项目可以让以后重装更安全。因此，`--full` 会删除所有项目文件，但不会把主机完全恢复到安装前的状态。

## 后续运维

健康检查、日志预算、更新策略和故障处理见 [运维手册](operations.md)。
