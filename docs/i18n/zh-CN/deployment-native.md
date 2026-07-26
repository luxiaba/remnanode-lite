<!-- translation: locale=zh-CN; source=docs/deployment-native.md; source-sha256=677c3ee233af2a465ca8508cd60c6cdf9496f87e618ec9c668afbb850275a974 -->

# 原生 Linux 部署

> 中文译文；部署规则有变化时，以[英文原文](../../deployment-native.md)为准。

[返回文档索引](README.md) · [配置参考](configuration.md) · [运维手册](operations.md) · [版本策略](versioning.md)

原生部署直接由宿主机的服务管理器运行 `remnanode-lite`，适合无法安装 Docker，或不适合承担 Docker Engine daemon 与容器运行时开销的小型服务器。Native 并不表示没有后台服务：`remnanode-lite` 仍由 systemd 运行；符合条件的 Alpine 主机则由发行版 OpenRC 运行。Docker Compose 仍是大多数节点的默认方式。自包含的 Native 生命周期 bundle 会作为带精确版本号的 GitHub Release 资产发布。

每个已发布的 bundle 都包含 Node、`rnlctl`、rw-core、GeoIP、GeoSite、ASN 数据、服务定义、许可证与 SPDX SBOM，并用 manifest 记录每个文件的摘要。安装器会先校验归档摘要，再校验 bundle 内容，之后才修改主机。

Native 安装和升级只接受包含 Native 生命周期资产的 Release 的精确版本。只有同时提供 `install.sh`、`SHA256SUMS` 和对应主机架构归档的 Release 才可用于 Native；`latest`、`preview`、`edge` 和 `sha-*` 等移动名称不能用于 Native。

## 支持范围

| 主机 | 服务管理器 | 支持级别 |
| --- | --- | --- |
| Rocky Linux 9 | systemd | 主要支持目标 |
| Rocky Linux 8 | systemd 239 | 兼容；较新的 hardening drop-in 会自动省略 |
| Debian 12 | systemd | 兼容 |
| 其他较新的 systemd 发行版 | systemd | 预计可用，批量部署前请先实测 |
| Alpine Linux 3.22.x（持久化 `sys` 安装） | 发行版 OpenRC | 满足前置条件时支持 |

Native 生命周期 bundle 面向 Linux `amd64` 和 `arm64` 构建。服务默认限制为 `448 MiB RAM`、不额外使用 swap、`1 CPU`、`256 tasks`，为 `512 MiB / 1 vCPU / 2 GB` 主机保留余量。

Alpine 这一行有意写得很具体，并不表示泛化的 OpenRC 支持。主机必须是 `amd64` 或 `arm64` 上持久化安装的 Alpine Linux 3.22.x `sys` 系统，由发行版 OpenRC 作为 PID 1 运行，内核不低于 Linux 5.14，并挂载统一的 cgroup v2 层级。`cpu`、`memory`、`pids` controller、`memory.swap.max`、父级 `cgroup.procs` 和服务 cgroup 的 `cgroup.kill` 都必须可用。受管服务会在 `start_pre` 中应用并核验精确的资源限制和 cgroup 成员关系；任何条件不满足都会拒绝启动。

Docker 容器和没有 init 的镜像不属于受支持的 Alpine Native 主机。以完整系统运行的嵌套或虚拟化环境只有通过同一套运行时检查才符合条件，不能只看发行版名称。不要为了让受限环境启动而绕过或削弱服务检查。

安装器不会替你修改系统软件源、sysctl、防火墙、SELinux 或时间同步。这些仍由主机管理员负责。

## 前置条件

以 root 在 Linux 上运行安装器。在线安装需要：

- systemd，或上面所述且符合条件的 Alpine/OpenRC 环境；
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

Alpine Linux 3.22.x（root shell）：

```sh
apk add --no-cache ca-certificates curl openrc shadow nftables iproute2 tar
rc-update add cgroups boot
rc-service cgroups start
```

在 Alpine 上，`shadow` 提供 `useradd` 和 `groupadd`，`tar` 包提供 GNU tar；Native bundle 的严格解压流程不能使用 BusyBox tar。`checkpath` 是 OpenRC 包内置的辅助程序，由 `openrc-run` 服务环境提供，但通常不在用户的 `PATH` 中；用户无需单独安装或检查它。

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
grep '  install.sh$' SHA256SUMS | sha256sum -c -

sudo sh ./install.sh --version "$VERSION" --port 2222
```

把 `2222` 换成 Panel 为该 Node 配置的端口。如果主机上没有有效 Secret，安装器会在终端中无回显地读取它，并在写入系统前请求确认。在线 installer 只下载当前架构对应的精确 `${VERSION}` 归档，不会跟随 GitHub Latest 或容器移动通道。

### 自动化安装

自动化时把完整 Panel Secret 放入临时普通文件，通过 `--secret-file` 传入。`--yes` 只跳过确认，不会生成或下载 Secret：

```bash
umask 077
printf '%s\n' 'PASTE_THE_COMPLETE_PANEL_SECRET_KEY' >/root/remnanode-lite.secret

sudo sh ./install.sh \
  --version "$VERSION" \
  --port 2222 \
  --secret-file /root/remnanode-lite.secret \
  --yes

rm -f /root/remnanode-lite.secret
```

不要把 Secret 直接写进命令行；进程列表和 shell history 可能暴露它。

### 只准备、不启动

`--prepare-only` 会安装并验证版本，但不启用、不启动服务；Secret 可以稍后提供：

```bash
sudo sh ./install.sh --version "$VERSION" --port 2222 --prepare-only --yes
sudo rnlctl activate --secret-file /root/remnanode-lite.secret
```

准备状态不能直接用 `rnlctl start` 启动；`activate` 会校验配置、启用服务、启动服务并等待内部健康检查。

`--prepare-only` 只校验并铺设 Release 文件，不会启动服务，因此即使主机不符合 Alpine/OpenRC 的 cgroup 契约也可能成功。`rnlctl activate` 是第一次对受管服务运行时 cgroup 与资源限制契约进行权威校验：OpenRC 会执行 `start_pre`，应用并核验限制；这些控制不可用时会直接失败。Alpine 版本、持久化 `sys` 安装、OpenRC 作为 PID 1 和内核版本仍需由操作者确认，并纳入发布验收；`activate` 不会代替操作者识别这些条件。

## 离线或分阶段安装

从一个确定的 GitHub Release 下载以下三个文件并保持原名：

```text
install.sh
remnanode-lite_<version>_linux_<architecture>.tar.gz
SHA256SUMS
```

在联网机器校验后再传到目标主机：

```bash
VERSION="<已发布版本>"
ARCH="<amd64或arm64>" # 目标主机架构
ARCHIVE="remnanode-lite_${VERSION}_linux_${ARCH}.tar.gz"
awk '$2 == "install.sh"' SHA256SUMS | sha256sum -c -
awk -v archive="$ARCHIVE" '$2 == archive' SHA256SUMS | sha256sum -c -
```

目标主机上执行：

```bash
VERSION="<已发布版本>"
ARCH="<amd64或arm64>"
ARCHIVE="remnanode-lite_${VERSION}_linux_${ARCH}.tar.gz"
sudo sh ./install.sh \
  --bundle "./${ARCHIVE}" \
  --port 2222
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
sudo rnlctl status
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl logs node --lines 100
sudo rnlctl logs core-errors --lines 100
remnanode-lite version
```

直接运行 `rnlctl status` 现在会输出一致、便于阅读的生命周期摘要，不再转发原始的 `systemctl status` 或 `rc-service status` 输出；human 排版不是可解析接口。已有自动化应使用 schema 不变的 `status --json`。需要底层细节时，直接运行前文的服务管理器命令。

status 会检查 generation、配置、服务、权限、修复缓存和内部 health socket。`doctor` 会展开各子系统结果，并在末尾给出汇总以及针对已知故障的确定性 `Next` 建议；`doctor --json` 的现有 schema 保持不变。它们不能证明 Panel 可达或代理流量正常，仍需在 Panel 和实际客户端连接中确认。

状态含义：

| 状态 | 含义 |
| --- | --- |
| `absent` | 没有受管 Native 安装 |
| `prepared` | 已验证但明确禁用、停止 |
| `installed` | 文件、服务状态和 health 一致 |
| `degraded` | 安装存在，但至少一个检查失败 |
| `recovery-required` | 有未完成 journal 或状态不可读，需要 repair |

## 命令行体验

全局选项可以放在命令或子命令前后任意位置：

```bash
sudo rnlctl --quiet config set LOW_MEMORY=1
sudo rnlctl status --no-color
```

`--quiet`（或 `-q`）会隐藏成功的生命周期/配置变更提示、`config check` 的 `configuration ok`，以及 human `status`/`doctor` 输出。它不会隐藏 help、version、`config show`/`get`、日志、补全脚本、升级 dry-run 计划、JSON 或错误。

status 和 doctor 只在 stdout 连接 TTY 时使用克制的颜色。输出被重定向、指定 `--no-color`、`NO_COLOR` 为非空值或 `TERM=dumb` 时，都不会出现 ANSI 转义序列。

退出码通常为：成功 `0`，运行失败或检查结果不健康 `1`，用法错误 `2`。`absent` 是 status 的有效状态，会返回 `0`；要求必须已安装的自动化还要检查 JSON 中的 `installed` 或 `deployment`。`logs` 启动 `journalctl` 或 `tail` 后，会透传该读取器的退出码；被信号终止时也可能返回 `128 + signal`。

### Shell 补全

`rnlctl completion bash|zsh|fish` 只把补全脚本写到 stdout，不会安装文件，也不会修改 shell 启动文件。

Bash 配合 `bash-completion` 时，安装到用户级 XDG 目录：

```bash
bash_dir="${BASH_COMPLETION_USER_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/bash-completion}/completions"
mkdir -p "$bash_dir"
/usr/local/sbin/rnlctl completion bash >"$bash_dir/rnlctl"
```

加载 `bash-completion` 后重新打开 Bash。当前会话可直接使用下面的 fallback；如需长期使用，也可以自行把同一行加入 `.bashrc`：

```bash
source <(/usr/local/sbin/rnlctl completion bash)
```

Zsh 将 `_rnlctl` 放入用户自己的 `fpath` 目录：

```zsh
mkdir -p ~/.zfunc
/usr/local/sbin/rnlctl completion zsh > ~/.zfunc/_rnlctl
```

在已有的 `compinit` 之前把目录加入 `fpath`；如果当前配置尚未初始化补全，再执行后两行：

```zsh
fpath=(~/.zfunc $fpath)
autoload -Uz compinit
compinit
```

Fish 会直接读取用户级补全目录：

```fish
mkdir -p ~/.config/fish/completions
/usr/local/sbin/rnlctl completion fish > ~/.config/fish/completions/rnlctl.fish
```

生成的补全是静态脚本，不会查询 Release、generation ID 或服务状态，也不包含 Secret 值和内部配置名。升级 `rnlctl` 后应重新生成。`sudo rnlctl ...` 是否能补全取决于用户自己的 shell/framework，这些脚本本身不作保证。

## 服务与日志

```bash
sudo rnlctl start
sudo rnlctl stop
sudo rnlctl restart
sudo rnlctl logs node --follow
sudo rnlctl logs core --follow
sudo rnlctl logs core-errors --follow
```

systemd 的 Node 输出进入 journald，可以用正数的 Go duration 筛选，并和行数、持续跟随组合：

```bash
sudo rnlctl logs node --since 15m --lines 100
sudo rnlctl logs node --since 15m --follow
```

`--lines` 默认 `50`，范围为 `1..100000`。`--since` 只支持 systemd 的 Node 日志，接受 `15m`、`2h` 这类正数 Go duration，不接受绝对日期或 `1d`。OpenRC Node 日志以及 `core`/`core-errors` 文件没有统一可靠的时间格式，因此会拒绝该选项。

systemd 的 `--lines N` 是该 unit 合计最多 N 条；OpenRC 则从 `openrc.log` 和 `openrc.err.log` 各读 N 行。rw-core 的 `xray.out.log` 与 `xray.err.log` 每个 source 只对应一个文件。文件日志只读取当前路径；当前文件不足 N 行时不会从轮转的 `.1` 回补。`--follow` 使用 `tail -F`，后续发生轮转时仍能继续跟随。

## 升级与回滚

修改安装前，先校验已发布的精确候选：

```bash
VERSION="<published-version>"
sudo rnlctl upgrade --to "$VERSION" --dry-run
sudo rnlctl upgrade --to "$VERSION" --dry-run --json
```

预检需要 root、已有且状态干净的安装，并且不能存在待处理的生命周期 journal。使用 `--to` 时，它会把完整候选下载到私有临时 workspace 并完成静态校验，然后短暂持有生命周期锁，检查当前状态和已知的宿主机前置条件。它不会创建 generation、cache 或事务 journal，不会切换或重启服务、执行候选二进制或运行目标健康检查，也不会保留下载的 bundle。`--json` 只能与 `--dry-run` 同时使用。

dry-run 会使用临时磁盘，但不会预留或保证真实升级所需的空间，也不能保证宿主状态保持不变或后续升级一定成功。相同选项也可检查本地 `--bundle` 加 `--sha256`，或 `--bundle-root`。确认计划后，再明确执行升级：

```bash
sudo rnlctl upgrade --to "$VERSION"
```

在线升级从对应 GitHub Release 下载归档和摘要，验证全部文件后创建新 generation；离线升级可传入已验证归档：

```bash
VERSION="<已发布版本>"
ARCH="<amd64或arm64>"
ARCHIVE="remnanode-lite_${VERSION}_linux_${ARCH}.tar.gz"
sudo rnlctl upgrade \
  --bundle "./${ARCHIVE}" \
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

生命周期和配置变更都会持有锁文件 `/run/remnanode-lite-installer/operation.lock`。generation 与服务生命周期切换还会写入持久化 journal；配置和 Secret 变更使用原子文件替换及下文说明的进程内恢复，不会创建崩溃安全的 journal。如果生命周期命令提示需要修复，不要手动删除锁、journal、generation 或缓存：

```bash
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl repair
```

缓存损坏时，可传入与记录身份一致的归档和 `--expected-version`。repair 只恢复已记录的 generation，不会悄悄升级。

## 修改端口或 Secret

`/etc/remnanode-lite/node.env` 是 Native 运行参数的唯一事实源。`rnlctl config` 只是这个文件之上的安全操作层，不会维护另一份配置。它只开放[配置说明](configuration.md)列出的 6 个管理员字段，不显示 Secret 或内部受管字段。

active 节点可以一次完成端口修改与应用：

```bash
sudo rnlctl config set NODE_PORT=2222 --apply
```

同时把 Panel 中的节点端口和宿主机防火墙改为相同值；host networking 没有端口转换层。

轮换 Secret 时，先把完整的新 Secret 放入 root-only 普通临时文件，再交给 `rnlctl` 校验和安装：

```bash
umask 077
sudo install -m 0600 /dev/null /root/new-node-secret.key
sudoedit /root/new-node-secret.key
sudo rnlctl secret set --file /root/new-node-secret.key --apply
sudo rm -f /root/new-node-secret.key
```

Secret 值不会进入 `node.env`、命令参数或命令输出。如果 `set --apply`、`unset --apply` 或 `secret set --apply` 在修改文件后的重启或内部健康检查失败，`rnlctl` 会尝试恢复原文件，并尝试用旧配置恢复 active 服务。这只是当前命令中的尽力恢复，不是持久化或崩溃安全的事务。

`--apply` 只适用于当前 active 的受管服务。服务已停止时，先不带 `--apply` 修改，再运行 `rnlctl start`。prepared 安装也先不带 `--apply` 修改，再运行 `rnlctl activate`；也可以用 `rnlctl activate --secret-file PATH` 在激活时提供 Secret。stopped 或 prepared 状态下，带 `--apply` 的命令会在写文件前拒绝执行。

仍然可以手工编辑 `node.env`。保持 `root:remnanode-lite 0640`，先运行 `sudo rnlctl config check`；active 安装再运行 `sudo rnlctl config apply`。`config apply` 会校验、重启并等待内部健康，但手工编辑没有旧快照，因此无法由它回滚。`check` 和 `apply` 都不会验证 Panel 连接或代理流量。

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

两种卸载都会删除受管 unit 和受管的
`20-remnanode-lite-hardening.conf` drop-in。预期的 drop-in 目录为空时也会删除；如其中有
`90-local.conf` 等本地 override，或目录对象异常，则会刻意保留，不会触碰管理员数据。

## 安全提示

- `/etc/remnanode-lite` 目录保持 `root:remnanode-lite 0750`，配置和 Secret 为 `0640`。
- Native `node.env` 不应放非空 `SECRET_KEY`，使用 `SECRET_KEY_FILE`。
- 受管 Node 进程以 `remnanode-lite` 用户运行，只获得 `CAP_NET_ADMIN` 与 `CAP_NET_BIND_SERVICE`。OpenRC 中以 root 身份运行的 `supervise-daemon` 属于服务管理器基础设施；不要改成 root Node 进程来掩盖 capability 配置问题。
- 批量更新前保留上一代精确版本，完成 Panel 和流量检查后再清理。
