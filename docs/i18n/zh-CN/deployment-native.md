<!-- translation: locale=zh-CN; source=docs/deployment-native.md; source-sha256=dba090d727b843193d91bac9991d8d69f4c1d5702258022ef6421191c38936df -->

# 原生 Linux 部署

> 中文译文；部署规则有变化时，以[英文原文](../../deployment-native.md)为准。

[返回文档索引](README.md) · [配置参考](configuration.md) · [运维手册](operations.md) · [版本策略](versioning.md)

原生部署直接由宿主机的 systemd 或 OpenRC 运行 `remnanode-lite`，适合无法安装 Docker，或不适合承担 Docker Engine daemon 与容器运行时开销的小型服务器。Native 并不表示没有后台服务：`remnanode-lite` 仍由 systemd 或 OpenRC 守护。Docker Compose 仍是大多数节点的默认方式。自包含的 Native 生命周期 bundle 会作为带精确版本号的 GitHub Release 资产发布。

每个已发布的 bundle 都包含 Node、`rnlctl`、rw-core、GeoIP、GeoSite、ASN 数据、服务定义、许可证与 SPDX SBOM，并用 manifest 记录每个文件的摘要。安装器会先校验归档摘要，再校验 bundle 内容，之后才修改主机。

Native 安装和升级只接受包含 Native 生命周期资产的 Release 的精确版本。只有同时提供 `install.sh`、`SHA256SUMS` 和对应主机架构归档的 Release 才可用于 Native；`latest`、`preview`、`edge` 和 `sha-*` 等移动名称不能用于 Native。

## 支持范围

| 主机 | 服务管理器 | 支持级别 |
| --- | --- | --- |
| Rocky Linux 9 | systemd | 主要支持目标 |
| Rocky Linux 8 | systemd 239 | 兼容；较新的 hardening drop-in 会自动省略 |
| Debian 12 | systemd | 兼容 |
| 其他较新的 systemd 发行版 | systemd | 预计可用，批量部署前请先实测 |
| 带可写 cgroup v2 controller 的 OpenRC | OpenRC | 实验性支持 |

Native 生命周期 bundle 面向 Linux `amd64` 和 `arm64` 构建。服务默认限制为 `448 MiB RAM`、不额外使用 swap、`1 CPU`、`256 tasks`，为 `512 MiB / 1 vCPU / 2 GB` 主机保留余量。OpenRC 还要求 `supervise-daemon`、`checkpath`、`rc-update`、cgroup v2、可写的 memory/CPU/PID controller 和 `cgroup.kill`；缺少这些条件时服务会拒绝启动。

安装器不会替你修改系统软件源、sysctl、防火墙、SELinux 或时间同步。这些仍由主机管理员负责。

## 前置条件

以 root 在 Linux 上运行安装器。在线安装需要：

- systemd，或上面所述的实验性 OpenRC 环境；
- `nft`（nftables）和 `ss`（iproute2）；
- 当专用 `remnanode-lite` 账号尚不存在时，提供 `useradd` 和 `groupadd`；
- 可信 CA、`curl` 或 `wget`；
- GNU tar 和 gzip；
- Panel 可访问的 Node 端口，以及 Panel 配置的代理入站端口。

Rocky Linux 8/9：

```bash
sudo dnf install -y ca-certificates curl nftables iproute
```

Debian 12：

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl nftables iproute2
```

请保持系统时间同步；时间错误会导致 mTLS 或 JWT 认证失败。

## 安装精确版本

先在 GitHub Releases 页面选择一个已经发布的版本，再从该精确 Release 下载 installer 和摘要清单，先验证 installer，再执行安装。源码版本和候选镜像都不是可下载的 Native bundle：

```bash
VERSION="<published-version>" # 例如：X.Y.Z 或 X.Y.Z-rnl.N
BASE="https://github.com/luxiaba/remnanode-lite/releases/download/${VERSION}"

workdir="$(mktemp -d /var/tmp/remnanode-lite-download.XXXXXX)"
trap 'rm -rf "$workdir"' EXIT
cd "$workdir"
curl -fLO "${BASE}/install.sh"
curl -fLO "${BASE}/SHA256SUMS"
grep '  install.sh$' SHA256SUMS | sha256sum --check --strict -

sudo sh ./install.sh --version "$VERSION" --port 38329
```

把 `38329` 换成 Panel 为该 Node 配置的端口。如果主机上没有有效 Secret，安装器会在终端中无回显地读取它，并在写入系统前请求确认。在线 installer 只下载当前架构对应的精确 `${VERSION}` 归档，不会跟随 GitHub Latest 或容器移动通道。

### 自动化安装

自动化时把完整 Panel Secret 放入临时普通文件，通过 `--secret-file` 传入。`--yes` 只跳过确认，不会生成或下载 Secret：

```bash
umask 077
printf '%s\n' 'PASTE_THE_COMPLETE_PANEL_SECRET_KEY' >/root/remnanode-lite.secret

sudo sh ./install.sh \
  --version "$VERSION" \
  --port 38329 \
  --secret-file /root/remnanode-lite.secret \
  --yes

rm -f /root/remnanode-lite.secret
```

不要把 Secret 直接写进命令行；进程列表和 shell history 可能暴露它。

### 只准备、不启动

`--prepare-only` 会安装并验证版本，但不启用、不启动服务；Secret 可以稍后提供：

```bash
sudo sh ./install.sh --version "$VERSION" --port 38329 --prepare-only --yes
sudo rnlctl activate --secret-file /root/remnanode-lite.secret
```

准备状态不能直接用 `rnlctl start` 启动；`activate` 会校验配置、启用服务、启动服务并等待内部健康检查。

## 离线或分阶段安装

从一个确定的 GitHub Release 下载以下三个文件并保持原名：

```text
install.sh
remnanode-lite_<version>_linux_<architecture>.tar.gz
SHA256SUMS
```

在联网机器校验后再传到目标主机：

```bash
grep -E '  (install\.sh|remnanode-lite_.*_linux_(amd64|arm64)\.tar\.gz)$' \
  SHA256SUMS | sha256sum --check --strict -
```

目标主机上执行：

```bash
sudo sh ./install.sh \
  --bundle "./remnanode-lite_${VERSION}_linux_amd64.tar.gz" \
  --port 38329
```

省略 `--sha256` 时，installer 会读取归档旁边 `SHA256SUMS` 中唯一匹配的条目；也可以显式传入 64 位十六进制摘要。生产环境推荐使用归档和独立下载的摘要清单，而不是只运行解压目录里的脚本。

## 文件布局

```text
/usr/local/sbin/rnlctl
/usr/local/bin/remnanode-lite -> /usr/local/lib/remnanode-lite/current/bin/remnanode-lite

/usr/local/lib/remnanode-lite/
├── current -> generations/<generation-id>
├── previous -> generations/<previous-id>       # 首次升级后出现
└── generations/<generation-id>/

/etc/remnanode-lite/
├── node.env
└── secret.key

/var/lib/remnanode-lite/
/var/log/remnanode-lite/
/run/remnanode-lite/

/var/lib/remnanode-lite-installer/
├── state.json
├── journal.json                                # 操作中或恢复时存在
├── retained.json                               # 非 purge 卸载后可能保留
├── bundles/
└── tmp/                                        # root-only 磁盘型临时根
```

installer 优先使用经过安全检查的显式 `TMPDIR`；否则使用 `/var/lib/remnanode-lite-installer/tmp`，无法准备时才回退 `/var/tmp`。每次操作的 workspace 都是 `0700` 并在退出时删除，避免 512 MiB 主机把大归档展开到可能由 tmpfs 承载的 `/tmp`。

`rnlctl` 是独立的 root-owned 普通文件，不是指向当前 generation 的软链接。即使 generation 链接损坏，修复工具仍可运行。服务使用不可登录的 `remnanode-lite` 用户和组；`uninstall --purge` 只会删除安装器创建且身份未被改变的账号对象。

systemd 和 OpenRC 的服务名分别是 `remnanode-lite.service` 与 `remnanode-lite`：

```bash
systemctl status remnanode-lite.service
rc-service remnanode-lite status
```

## 安装后检查

```bash
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl logs node --lines 100
sudo rnlctl logs core-errors --lines 100
remnanode-lite version
```

`status --json` 检查 generation、配置、服务、权限、修复缓存和内部 health socket。`doctor` 会展开各子系统结果。它们不能证明 Panel 可达或代理流量正常；仍需在 Panel 和实际客户端连接中确认。

状态含义：

| 状态 | 含义 |
| --- | --- |
| `absent` | 没有受管 Native 安装 |
| `prepared` | 已验证但明确禁用、停止 |
| `installed` | 文件、服务状态和 health 一致 |
| `degraded` | 安装存在，但至少一个检查失败 |
| `recovery-required` | 有未完成 journal 或状态不可读，需要 repair |

## 服务与日志

```bash
sudo rnlctl start
sudo rnlctl stop
sudo rnlctl restart
sudo rnlctl logs node --follow
sudo rnlctl logs core --follow
sudo rnlctl logs core-errors --follow
```

systemd 的 Node 输出进入 journald；OpenRC 使用 `/var/log/remnanode-lite/openrc.log` 和 `openrc.err.log`。rw-core 输出统一位于 `/var/log/remnanode-lite/xray.out.log` 与 `xray.err.log`，`rnlctl logs` 会选择正确的后端并跟随轮转文件。

## 升级与回滚

升级只能选择已经发布的精确版本：

```bash
VERSION="<published-version>"
sudo rnlctl upgrade --to "$VERSION"
```

在线升级从对应 GitHub Release 下载归档和摘要，验证全部文件后创建新 generation；离线升级可传入已验证归档：

```bash
sudo rnlctl upgrade \
  --bundle "./remnanode-lite_${VERSION}_linux_amd64.tar.gz" \
  --sha256 '<64-character-sha256>' \
  --expected-version "$VERSION"
```

升级保留服务之前的启用/运行状态，并在活动服务恢复后等待内部 health。只保留 current 和 previous 两个 generation；不要直接覆盖 `/usr/local/bin/remnanode-lite`。

回滚到保留的上一代：

```bash
sudo rnlctl rollback
sudo rnlctl rollback --to '<previous-generation-id>'
```

## 中断恢复

变更操作使用 root-only journal 和锁文件 `/run/remnanode-lite-installer/operation.lock`。如果命令提示需要修复，不要手动删除锁、journal、generation 或缓存：

```bash
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl repair
```

缓存损坏时，可传入与记录身份一致的归档和 `--expected-version`。repair 只恢复已记录的 generation，不会悄悄升级。

## 修改端口或 Secret

`/etc/remnanode-lite/node.env` 是 root 管理的数据文件，不是 shell 脚本。Secret 应放在 `/etc/remnanode-lite/secret.key`，权限为 `root:remnanode-lite 0640`；轮换时写入临时 root-only 文件，再原子替换并重启：

```bash
umask 077
secret_tmp="$(mktemp)"
printf '%s\n' 'PASTE_THE_NEW_COMPLETE_SECRET_KEY' >"$secret_tmp"
remnanode-lite validate-secret <"$secret_tmp"
sudo install -o root -g remnanode-lite -m 0640 \
  "$secret_tmp" /etc/remnanode-lite/secret.key.new
sudo mv -f /etc/remnanode-lite/secret.key.new /etc/remnanode-lite/secret.key
rm -f "$secret_tmp"
sudo rnlctl restart
```

修改 `NODE_PORT` 时同时更新 Panel 和主机防火墙，然后运行 `sudo rnlctl doctor` 与 `sudo rnlctl restart`。

## 卸载

普通卸载删除服务、二进制、generation、运行状态、日志和 installer 缓存，但保留 `/etc/remnanode-lite` 以便安全重装：

```bash
sudo rnlctl uninstall
```

明确 purge 才会删除配置和 installer 元数据：

```bash
sudo rnlctl uninstall --purge --yes
```

Purge 不会删除主机软件包、防火墙策略、sysctl、无关 Xray 安装或其他管理员数据。

## 安全提示

- `/etc/remnanode-lite` 目录保持 `root:remnanode-lite 0750`，配置和 Secret 为 `0640`。
- Native `node.env` 不应放非空 `SECRET_KEY`，使用 `SECRET_KEY_FILE`。
- 服务只需要 `CAP_NET_ADMIN` 与 `CAP_NET_BIND_SERVICE`；不要用 root 服务掩盖能力配置问题。
- 批量更新前保留上一代精确版本，完成 Panel 和流量检查后再清理。
