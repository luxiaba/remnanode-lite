<!-- translation: locale=zh-CN; source=docs/development/release-acceptance.md; source-sha256=7f19223ccf4b11ca0cb9b057a487a76329137c5c212e283d51f79df7be9132c0 -->
# M8 发布验收证据协议

> **翻译说明：** [英文原文](../../../development/release-acceptance.md)是唯一权威来源；本页用于中文阅读，并应随英文源同步。

[返回开发文档](README.md) · [通用发布流程](../release.md)

本协议定义 `2.8.0` 的机器可校验验收记录。它不替代真实测试；它保证所有结果绑定同一个代码候选，并防止验收后代码漂移。协议本身不代表已经验收；实际状态只能由完整的版本化 evidence 目录和对应正式 Release 共同证明。

这是首个版本线的版本化验收 profile，不是所有未来版本都可直接复用的通用模板。项目版本、官方契约、Panel、rw-core、路由数量、系统和资源策略变化时，必须在普通代码 PR 中同步更新验证器和本协议，再冻结新的候选。

## 候选冻结

先提交全部 Go、测试、脚本、workflow、部署和治理改动，得到 40 位 commit `C` 与 tree。所有 evidence 的 `candidateCommit` 必须是 `C`，测试开始时间不得早于该 commit 时间。候选容器构建完成后，将 registry 返回的不可变 manifest digest 记录为 `candidateImageDigest`；后续容器验收和发布必须使用这个 digest，而不是可移动的 tag。

用于验收的 Node 二进制必须由 `scripts/build-release-binaries.sh` 从干净的 `C` 构建。该脚本要求本地工具链精确为 `go1.26.5`，关闭 workspace 与自动工具链漂移，清空会改变产物的 Go 构建选项，并固定 `CGO_ENABLED=0`、架构级别、`-trimpath`、release ldflags 和 `-buildvcs=false`；最终 release gate 会用同一脚本重建两种架构并比较 SHA-256。

验收后只允许修改根 README 及其中俄译文、CHANGELOG、开发 roadmap 及其中文译文、`docs/development/acceptance/v2.8.0/` 和 `docs/releases/v2.8.0.md`。验证器要求 `C` 是最终 HEAD 的祖先，逐 commit、逐 parent 检查白名单，并拒绝发布最终化阶段的 merge；修改代码后再 revert 也不能绕过。

受保护分支下应先将全部代码通过 PR 合入 `main`，以合入后的 `main` commit 作为 `C`。验收资料从 `C` 创建独立分支，验收期间冻结 `main`；最终资料 PR 必须使用 squash merge，使 `C` 之后恰好产生一个 single-parent、仅包含白名单路径的提交。验证器会同时拒绝零个或多个最终化提交、普通 merge commit 和白名单外变化。

## 文件布局

```text
docs/development/acceptance/v2.8.0/
  manifest.json
  systemd.json
  openrc.json
  panel.json
  compose.json
  resource-fault.json
```

六个文件必须是 Git 跟踪、非可执行且不超过 `1 MiB` 的普通文件，工作树和 index 必须与 HEAD blob 完全一致。JSON key 大小写敏感，拒绝重复或未知字段。`manifest.json` 记录其余五个文件的 SHA-256。

## Manifest

Manifest 固定以下发布边界：

- `releaseVersion=2.8.0`、`releaseTag=v2.8.0`、`decision=pass`。
- `candidateCommit`、`candidateTree`、RFC3339 `acceptedAt`。
- `candidateImageDigest` 必须是 registry 返回的候选容器 manifest digest，格式严格为 `sha256:` 加 64 位小写十六进制字符。它把验收结论绑定到实际测试的多架构镜像；tag 名称或单个架构镜像的配置/层摘要不能替代该字段。
- 官方 Node `2.8.0@596f015a5c8f876dc9a9d61b6cb78d35bd8e379b`。
- Panel `2.8.1`。
- rw-core `v26.6.27@45cf2898ab12e97a55dd8f1f3d78d903340bdc9e`。
- 固定下载资产 SHA-256：amd64 `b3e5902d06d6282fe53cfa2fc426058b9aeaa429b2c812e20887cd47f26d08bf`，arm64 `13a251379bea366c2cf10363ad71e75734193d401f26f518bf0c25e5c8f8c931`。
- 策略：整机 512 MiB、service 448 MiB、1 CPU、2048 MiB disk、50k 用户、no swap、soak 至少 86400 秒。
- evidence 必须且只能包含 `systemd`、`openrc`、`panel`、`compose`、`resource-fault` 五类。

风险项的 severity 只能是 P1/P2/P3，status 只能是 open/closed。任何 `releaseBlocking=true`，或未关闭的 P1/P2，都会拒绝发布。

## 发行环境证据

`systemd.json` 与 `openrc.json` 记录：

- `schemaVersion`、kind、candidate、pass 状态、开始/结束时间和实际命令。
- OS、版本、init、arch、kernel、内存、CPU 和磁盘。
- Node version output 与安装后二进制 SHA-256。
- rw-core version、commit、固定下载资产 SHA-256 和安装后二进制 SHA-256。
- 全新安装、重复安装、启停/重启、成功升级、失败升级回滚、reboot 后 Panel 重同步、capability 边界、卸载隔离、nft namespace 和 socket-kill namespace 检查。
- `checks.rwCoreProcessGroupCleanup=true`：使用 wrapper + child 验证独立 PGID、正常停止的整组 SIGINT/SIGKILL，以及 leader 自然退出后的残余组清理；不测试 Node 或 supervisor 自身被强杀后的自动恢复。

环境固定为 Ubuntu 24.04/systemd 与 Alpine 3.22/OpenRC；两条记录的架构并集必须覆盖 amd64 和 arm64。

## Panel 证据

`panel.json` 固定 Panel `2.8.1`，targets 必须覆盖 systemd/openrc。`artifacts` 为两个 target 分别记录架构、Node binary SHA-256 和 rw-core binary SHA-256，并必须与对应发行环境证据完全一致。26 条路由必须全部通过，semantic mismatch 为 0，并覆盖节点注册、Xray 生命周期、统计、用户 mutation 与插件同步。`checks.lifecyclePluginSerialization=true` 必须由 `start/stop` 与 `sync/recreate` 的并发交错场景证明：transport lifecycle gate 始终在最外层，Plugin operation gate 与 Manager ownership 不发生反向重叠，等待期间的取消可以终止请求。

## Docker Compose 证据

`compose.json` 证明最终用于大量小节点部署的 Compose 模板和最终 GHCR 候选确实共同运行过。它不是对 YAML 的静态复述。验收必须从 `C` 导出 `deploy/compose.single-file.yaml`，只注入真实 Secret、节点端口并将 `image` 替换为 `ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}`；不得放宽模板中的隔离或资源约束。Secret 和展开后的 Compose 不进入 evidence。

证据必须记录并通过以下门禁：

- `candidateImageDigest` 与 manifest 顶层字段逐字一致。`source.path` 固定为 `deploy/compose.single-file.yaml`；验证器直接读取 `candidateCommit` 的 Git object 计算文件字节 SHA-256，并与 `source.sha256` 比较，不信任当前 checkout。
- 顶层 `manifestPlatforms` 在过滤 provenance/SBOM descriptor 后必须严格为 `linux/amd64`、`linux/arm64`。`runs` 必须且只能有两个完整记录，`environment.arch` 精确覆盖 `amd64`、`arm64` 各一次；不能用 manifest 声明代替任一架构的实际运行。
- 每个 run 的 `environment` 记录实际 Docker Engine 与 Docker Compose 版本。版本仅要求非空，避免把无关的补丁版本升级变成发布阻塞条件。
- 每个 run 的 `hostResources` 来自 Linux 可见的真实宿主资源：memory 必须在 `480..512 MiB`，CPU 必须为 `1`，Docker 所在文件系统总量必须在 `1792..2048 MiB`，swap 必须显式为 `0`。测得项目峰值时仍须至少有 `256 MiB` 可用磁盘；所有字段为必填，合法零值不会被缺字段替代。这个容差用于吸收内核保留页和十进制/二进制磁盘差异，不把更大宿主上的 448 MiB cgroup 测试伪装成整机小规格测试。
- 每个 run 的 `limits` 使用 Docker inspect/cgroup 的实际值，而不是 Compose 源码值：memory `469762048` bytes、memory+swap `469762048` bytes、`nanoCPUs=1000000000`、`pidsLimit=256`。
- 每个 run 的 `isolation` 证明 read-only rootfs、no-new-privileges、`init: true`、`docker-init`/`tini` 位于 PID 1 且实际完成 orphan reaping。能力必须先 drop `ALL`，配置和进程有效能力都只能包含 `NET_ADMIN`、`NET_BIND_SERVICE`；从内核位图采集时先去掉常见的 `CAP_` 前缀再写入 JSON。实际 mount 必须包含 `/run/remnanode=4 MiB`、`/tmp=16 MiB`、`/var/log/remnanode=28 MiB` 三个 writable tmpfs，且都有 `noexec,nosuid,nodev`。
- 每个 run 的 `health` 至少观察一次 `healthy` 和 exit code `0`。`restart: unless-stopped` 不会因为 unhealthy 自动重启，因此健康通过和故障恢复必须分别验证。
- 每个 run 的 `lifecycle` 记录运行基线、受控压力峰值和恢复后的 PIDs；峰值必须高于基线但不得超过 256，恢复后必须低于峰值，zombie 为 0。这里不要求精确回到基线，以容纳 Go/runtime 和 rw-core 的正常线程波动。正常 `docker compose stop` 必须在 35 秒窗口内由应用完成，exit code 为 0，事件流中不得出现 SIGKILL，停止后剩余 PIDs 为 0。
- 每个 run 的 `logs` 必须实测 `json-file`、`2 MiB x 2` 和轮转。允许 active file 在轮转边界短暂超出阈值，所有项目容器 log file 的观测峰值不得超过 `6 MiB`。
- 每个 run 都必须实际 pull、启动并确认一个回滚镜像健康；首发允许 `docker.io/remnawave/node`，后续版本也可使用 `ghcr.io/luxiaba/remnanode-lite` 的上一正式版本。两条 run 的回滚 repository 和多架构 manifest digest 必须完全相同；`rollbackImageDigest` 必须有效且不同于候选，测量磁盘峰值时该镜像仍在本机。`projectDiskPeakMiB` 包含候选、回滚镜像、writable layer、Docker json logs 与项目临时文件，最多 `1024 MiB`；它与峰值可用字节之和不得超过实测总盘。磁盘验收只能在专用、无无关容器/镜像的 Docker 环境完成，不得在共享 daemon 上猜测 layer 归属，也不得由验收脚本自动 prune 未知对象。

字段结构如下；尖括号内容必须替换为实测值：

```json
{
  "schemaVersion": 1,
  "kind": "compose",
  "candidateCommit": "<40-lowercase-hex>",
  "status": "pass",
  "startedAt": "<RFC3339>",
  "finishedAt": "<RFC3339>",
  "command": ["<repeatable-compose-acceptance-runner>"],
  "candidateImageDigest": "sha256:<64-lowercase-hex>",
  "source": {
    "path": "deploy/compose.single-file.yaml",
    "sha256": "<64-lowercase-hex>"
  },
  "manifestPlatforms": ["linux/amd64", "linux/arm64"],
  "runs": [
    {
      "environment": {
        "dockerEngineVersion": "<actual-amd64-version>",
        "dockerComposeVersion": "<actual-amd64-version>",
        "arch": "amd64"
      },
      "hostResources": {
        "memoryTotalBytes": 524288000,
        "cpuCount": 1,
        "diskTotalBytes": 2097152000,
        "diskAvailableAtPeakBytes": 536870912,
        "swapTotalBytes": 0
      },
      "limits": {
        "memoryLimitBytes": 469762048,
        "memorySwapLimitBytes": 469762048,
        "nanoCPUs": 1000000000,
        "pidsLimit": 256
      },
      "isolation": {
        "readOnlyRootfs": true,
        "noNewPrivileges": true,
        "initEnabled": true,
        "initPid": 1,
        "initProcess": "docker-init",
        "orphanReapingPassed": true,
        "capDrop": ["ALL"],
        "capAdd": ["NET_ADMIN", "NET_BIND_SERVICE"],
        "effectiveCapabilities": ["NET_ADMIN", "NET_BIND_SERVICE"],
        "tmpfs": [
          {"target": "/run/remnanode", "sizeBytes": 4194304, "writable": true, "noexec": true, "nosuid": true, "nodev": true},
          {"target": "/tmp", "sizeBytes": 16777216, "writable": true, "noexec": true, "nosuid": true, "nodev": true},
          {"target": "/var/log/remnanode", "sizeBytes": 29360128, "writable": true, "noexec": true, "nosuid": true, "nodev": true}
        ]
      },
      "health": {"status": "healthy", "checkExitCode": 0, "consecutiveSuccesses": 3},
      "lifecycle": {
        "gracefulStop": true,
        "forcedKill": false,
        "exitCode": 0,
        "pidsBaseline": 8,
        "pidsPeak": 15,
        "pidsAfterRecovery": 8,
        "pidsAfterStop": 0,
        "zombiesAfterRecovery": 0
      },
      "logs": {"driver": "json-file", "maxSizeBytes": 2097152, "maxFiles": 2, "rotationObserved": true, "peakBytes": 3145728},
      "storage": {
        "rollbackImageRepository": "docker.io/remnawave/node",
        "rollbackImageDigest": "sha256:<different-64-lowercase-hex>",
        "rollbackImagePulled": true,
        "rollbackImageStarted": true,
        "rollbackImageHealthy": true,
        "rollbackImagePresentAtPeak": true,
        "projectDiskPeakMiB": 350
      }
    },
    {
      "environment": {
        "dockerEngineVersion": "<actual-arm64-version>",
        "dockerComposeVersion": "<actual-arm64-version>",
        "arch": "arm64"
      },
      "hostResources": {
        "memoryTotalBytes": 524288000,
        "cpuCount": 1,
        "diskTotalBytes": 2097152000,
        "diskAvailableAtPeakBytes": 536870912,
        "swapTotalBytes": 0
      },
      "limits": {
        "memoryLimitBytes": 469762048,
        "memorySwapLimitBytes": 469762048,
        "nanoCPUs": 1000000000,
        "pidsLimit": 256
      },
      "isolation": {
        "readOnlyRootfs": true,
        "noNewPrivileges": true,
        "initEnabled": true,
        "initPid": 1,
        "initProcess": "docker-init",
        "orphanReapingPassed": true,
        "capDrop": ["ALL"],
        "capAdd": ["NET_ADMIN", "NET_BIND_SERVICE"],
        "effectiveCapabilities": ["NET_ADMIN", "NET_BIND_SERVICE"],
        "tmpfs": [
          {"target": "/run/remnanode", "sizeBytes": 4194304, "writable": true, "noexec": true, "nosuid": true, "nodev": true},
          {"target": "/tmp", "sizeBytes": 16777216, "writable": true, "noexec": true, "nosuid": true, "nodev": true},
          {"target": "/var/log/remnanode", "sizeBytes": 29360128, "writable": true, "noexec": true, "nosuid": true, "nodev": true}
        ]
      },
      "health": {"status": "healthy", "checkExitCode": 0, "consecutiveSuccesses": 3},
      "lifecycle": {
        "gracefulStop": true,
        "forcedKill": false,
        "exitCode": 0,
        "pidsBaseline": 8,
        "pidsPeak": 15,
        "pidsAfterRecovery": 8,
        "pidsAfterStop": 0,
        "zombiesAfterRecovery": 0
      },
      "logs": {"driver": "json-file", "maxSizeBytes": 2097152, "maxFiles": 2, "rotationObserved": true, "peakBytes": 3145728},
      "storage": {
        "rollbackImageRepository": "docker.io/remnawave/node",
        "rollbackImageDigest": "sha256:<different-64-lowercase-hex>",
        "rollbackImagePulled": true,
        "rollbackImageStarted": true,
        "rollbackImageHealthy": true,
        "rollbackImagePresentAtPeak": true,
        "projectDiskPeakMiB": 350
      }
    }
  ]
}
```

建议从下列客观来源生成记录，而不是手工抄写：

```bash
git show "${C}:deploy/compose.single-file.yaml" | sha256sum
docker version --format '{{.Server.Version}}'
docker compose version --short
docker buildx imagetools inspect "ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}"
docker inspect remnanode
docker events --filter container=remnanode
```

每个架构都应使用相同版本的可重复 runner，读取 cgroup v1/v2、容器 PID namespace、`/proc`、mount options、effective capabilities、Docker health/events、Docker log 文件和 DockerRootDir 所在文件系统。orphan 探针应确认短时子进程被 PID 1 收养并回收；PID 压力只需产生可观察的有限峰值，不要逼近 256；优雅停止必须综合事件、exit code、OOM 状态与停止后的 PID/cgroup 状态判定。向主进程 stdout 生成日志轮转负载时只能写固定无敏感填充内容，不能重放真实请求或日志。

`command` 数组必须记录实际 runner 及非敏感参数，Secret 只能经受限 FD 或本机 `0600` 临时文件注入，不得出现在命令、evidence、终端输出或原始采集包中。机器验证器能证明 JSON 结构、候选绑定和阈值一致，不能凭空证明命令确实执行；因此发布负责人必须保留两台专用验收机的原始 allowlist 采集包及 SHA-256，在提交 evidence 前由第二位审阅者复核。`["true"]`、手工抄写布尔值或只读取 Compose 源码都不构成 M8 证据。

## 资源与故障证据

`resource-fault.json` 使用与发行环境相同的 Node/rw-core 身份字段，并记录 50k 用户、soak 秒数、cgroup 峰值、OOM kill、项目磁盘峰值。门禁要求：

- soak 不少于 86400 秒。
- peak memory 不超过 448 MiB。
- OOM kill 为 0，项目磁盘不超过 2048 MiB，no swap。
- core kill（包含 process-group 后代清理）、Node restart、Panel disconnect、nft failure/retry、日志故障风暴和失败升级回滚全部通过。

## 数据边界

只记录白名单指标、命令和摘要。不得提交 Secret Key、JWT、CA、客户端证书、私钥、IP、hostname、Panel URL、原始请求/响应或可还原用户的数据。命令参数中的敏感值必须先脱敏。

## 验证

```bash
go run ./cmd/release-evidence-check \
  -manifest docs/development/acceptance/v2.8.0/manifest.json \
  -tag v2.8.0
```

`scripts/release-check.sh` 会调用同一验证器，并继续检查 release note、版本、完整 Go 门禁、供应链和 tag 位置。在真实证据尚未生成前，release gate 失败是预期行为。
