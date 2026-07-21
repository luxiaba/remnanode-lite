<!-- translation: locale=zh-CN; source=docs/development/release-acceptance.md; source-sha256=926ff50d2b1a9cbf3b0bd4942d3f2262d0ea1e98df149cf5b5c28d279dab554e -->
# v2.8.0 发布验收证据协议

> 这是中文译文；验收字段和规则以[英文原文](../../../development/release-acceptance.md)为准。

[返回开发文档](README.md) | [通用发布流程](../release.md)

这是 `v2.8.0` 专用的验收协议，定义 schema version 2 和
`docker-production-smoke-v1` profile。它不表示每个已打包平台或部署方式都已经在生产
环境运行过。

验收绑定一个冻结源码候选、一个带 attestation 的多架构镜像 digest，以及一次在原生
amd64/x86_64 上运行的真实低内存 Docker smoke。运行时观测由 Release Owner 签字
确认。验证器可以检查记录、Git 历史、摘要、产物、阈值和签注，但不能独立证明所记录的
Panel 会话或流量确实发生。因此，这份证据可以审计，但并非不可伪造。

## 候选冻结

在受保护 `main` 上冻结 40 位候选提交 `C` 前，先提交全部 Go、测试、脚本、
workflow、部署和治理改动。记录它的 Git tree，以及候选镜像由 registry 返回的不可变
OCI manifest digest。所有 smoke 时间戳都不得早于 `C` 的提交时间。

候选镜像流水线必须在同一个 manifest 内构建 `linux/amd64` 与
`linux/arm64`，并生成 SBOM、provenance 和 GitHub build attestation。这是两种
架构的构建与供应链要求；本 profile 只有 `linux/amd64` 的生产运行时验证会阻断
发布。

使用 `scripts/build-release-binaries.sh` 从干净的 `C` 构建 Release Node
二进制，并把两个架构的 SHA-256 写入 manifest。最终发布门禁会重新构建两种架构并
比较摘要；它不会在 amd64 release runner 上执行 arm64 二进制。

smoke 完成后，只允许修改根 README 及其中俄译文、`CHANGELOG.md`、英文和中文
roadmap、两份 acceptance JSON，以及 `docs/releases/v2.8.0.md`。验证器要求
`C` 后恰好一个 single-parent 最终化提交，并拒绝白名单以外的路径。

## 文件布局

```text
docs/development/acceptance/v2.8.0/
  manifest.json
  docker-smoke.json
```

两份文件都必须是 Git 跟踪、非可执行且不超过 `1 MiB` 的普通文件，工作树和 index
必须与 `HEAD` blob 一致。JSON key 大小写敏感，拒绝重复或未知字段。

## Manifest

`manifest.json` 固定发布与产物身份：

- `schemaVersion=2` 与
  `acceptanceProfile=docker-production-smoke-v1`。
- `releaseVersion=2.8.0`、`releaseTag=v2.8.0`、`decision=pass`。
- candidate commit、tree、OCI manifest digest 和 RFC3339 验收时间；
  `acceptedAt` 不得早于 smoke evidence 的完成时间。
- 从 `C` 构建的 amd64、arm64 Node 二进制 SHA-256。
- 官方 Node `2.8.0@596f015a5c8f876dc9a9d61b6cb78d35bd8e379b`。
- Panel `2.8.1`。
- rw-core `v26.6.27@45cf2898ab12e97a55dd8f1f3d78d903340bdc9e`
  和经过审计的 amd64/arm64 资产 SHA-256。
- 下列严格固定的 deferred validation 列表。
- 唯一一条 kind 为 `docker-production-smoke` 的 pass evidence 引用；path 必须是
  `docs/development/acceptance/v2.8.0/docker-smoke.json`，其 SHA-256 覆盖
  完整文件字节。

deferred 列表顺序和内容必须精确如下：

```json
[
  "arm64-production-runtime",
  "native-systemd-install",
  "native-openrc-install",
  "50000-user-load",
  "24h-soak",
  "fault-and-rollback-injection"
]
```

这些项目是 `v2.8.0` profile 明确记录的限制，不阻断发布，也不得描述成已经通过。
带日期的 M6 50,000 用户与 M7 原生 init 工程测量仍是有用的历史基线，但不是 `C`
的运行时 evidence。

风险 severity 只能是 `P1`、`P2`、`P3`，status 只能是 `open` 或
`closed`，并且必须包含 `releaseBlocking` 布尔值。任何 release-blocking
风险或未关闭的 P1/P2 都会使验证失败；开放且不阻断的 P3 可以记录 deferred 范围及
缓解措施。

Manifest 的规范结构如下：

```json
{
  "schemaVersion": 2,
  "acceptanceProfile": "docker-production-smoke-v1",
  "releaseVersion": "2.8.0",
  "releaseTag": "v2.8.0",
  "candidateCommit": "<40-lowercase-hex>",
  "candidateTree": "<40-lowercase-hex>",
  "candidateImageDigest": "sha256:<64-lowercase-hex>",
  "candidateNodeSha256": {
    "amd64": "<64-lowercase-hex>",
    "arm64": "<64-lowercase-hex>"
  },
  "acceptedAt": "<RFC3339>",
  "decision": "pass",
  "officialNode": {
    "version": "2.8.0",
    "commit": "596f015a5c8f876dc9a9d61b6cb78d35bd8e379b"
  },
  "panelTarget": {
    "version": "2.8.1"
  },
  "rwCore": {
    "version": "v26.6.27",
    "commit": "45cf2898ab12e97a55dd8f1f3d78d903340bdc9e",
    "sha256": {
      "amd64": "b3e5902d06d6282fe53cfa2fc426058b9aeaa429b2c812e20887cd47f26d08bf",
      "arm64": "13a251379bea366c2cf10363ad71e75734193d401f26f518bf0c25e5c8f8c931"
    }
  },
  "deferredValidation": [
    "arm64-production-runtime",
    "native-systemd-install",
    "native-openrc-install",
    "50000-user-load",
    "24h-soak",
    "fault-and-rollback-injection"
  ],
  "evidence": [
    {
      "kind": "docker-production-smoke",
      "path": "docs/development/acceptance/v2.8.0/docker-smoke.json",
      "sha256": "<64-lowercase-hex>",
      "status": "pass"
    }
  ],
  "risks": []
}
```

## Docker 生产 smoke

在真实原生 amd64/x86_64 Linux 主机上运行 `C` 中的
`deploy/compose.single-file.yaml`。只能修改完整 Panel Secret、节点端口和镜像
引用。镜像必须固定为
`ghcr.io/luxiaba/remnanode-lite@${CANDIDATE_DIGEST}`。不得放宽模板中的资源、
capability、文件系统、init、healthcheck 或日志设置。

最终检查必须证明同一个容器已经运行至少 600 秒。evidence 的 `startedAt` 必须与
Docker `.State.StartedAt` 完全相同，且 `finishedAt - startedAt` 至少为 600 秒。
此前 `health=none`、使用可移动镜像 tag、非规范 Compose 文件或不同候选的运行都不
满足本 profile。

`docker-smoke.json` 记录：

- 通用 evidence 字段：schema、kind、candidate、pass 状态、容器启动时间、最终检查
  时间，以及实际执行且不含敏感信息的最终检查 command。
- 候选 manifest digest 和 digest-pinned image reference。
- `deploy/compose.single-file.yaml`，以及从 candidate Git object 读取该文件所得
  的 SHA-256，而不是当前 checkout 的摘要。
- 精确包含 `linux/amd64`、`linux/arm64` 的 manifest platform 集合。
- 原生 `amd64` / `x86_64`、kernel、Docker Engine 和 Docker Compose 身份。
- 真实宿主：480..512 MiB memory、1 CPU、1792..2048 MiB total disk、zero swap。
- 精确 Node version output 和 amd64 candidate binary SHA-256。
- 64 字符 Docker container ID、固定到 digest 的 `.Config.Image` 和精确
  `.State.StartedAt`；名为 `remnanode` 且正在运行的容器，health 为 healthy、
  healthcheck exit code 为 0、至少一次连续成功、OOM kill 为 0、restart 为 0。
- 实际 Docker 配置闭包：host network、`unless-stopped`、只读 rootfs、
  no-new-privileges、启用 init 且 PID 1 为 `docker-init` 或 `tini`、精确的
  drop/add capability、三个 tmpfs 的 size/mode/options、精确 healthcheck、精确
  `json-file` options、nofile soft/hard limit 和 35 秒 stop grace period。
- 实际限制：448 MiB memory、448 MiB memory plus swap、1 CPU、256 PIDs。
- 正数 memory current/peak 与 PID current/peak；current 不大于 peak，peak 不超过
  配置上限。
- low-memory mode、ASN database、internal socket、listener readiness 全部通过。
- Panel `2.8.1` connected，且真实代理流量通过。
- 保留且已脱敏的原始采集包 SHA-256。
- Release Owner 签注：operator `luxiaba`、role `release-owner`、decision
  `accept`。

Evidence 的规范结构如下：

```json
{
  "schemaVersion": 2,
  "kind": "docker-production-smoke",
  "candidateCommit": "<same C as manifest>",
  "status": "pass",
  "startedAt": "<exact container .State.StartedAt RFC3339 value>",
  "finishedAt": "<RFC3339 at least 600 seconds after startedAt>",
  "command": ["docker", "inspect", "remnanode"],
  "candidateImageDigest": "sha256:<same digest as manifest>",
  "imageReference": "ghcr.io/luxiaba/remnanode-lite@sha256:<same digest>",
  "source": {
    "path": "deploy/compose.single-file.yaml",
    "sha256": "<SHA-256 of C:path>"
  },
  "manifestPlatforms": ["linux/amd64", "linux/arm64"],
  "environment": {
    "arch": "amd64",
    "unameMachine": "x86_64",
    "kernel": "<non-empty>",
    "dockerEngineVersion": "<non-empty>",
    "dockerComposeVersion": "<non-empty>"
  },
  "host": {
    "memoryTotalBytes": 536870912,
    "cpuCount": 1,
    "diskTotalBytes": 2147483648,
    "swapTotalBytes": 0
  },
  "node": {
    "versionOutput": "remnanode-lite 2.8.0 (contract 2.8.0)",
    "binarySha256": "<manifest candidateNodeSha256.amd64>"
  },
  "container": {
    "id": "<64-lowercase-hex Docker container ID>",
    "name": "remnanode",
    "imageReference": "ghcr.io/luxiaba/remnanode-lite@sha256:<same digest>",
    "startedAt": "<exactly equal to top-level startedAt>",
    "status": "running",
    "healthStatus": "healthy",
    "healthCheckExitCode": 0,
    "consecutiveHealthSuccesses": 1,
    "oomKilled": false,
    "restartCount": 0,
    "networkMode": "host",
    "restartPolicy": "unless-stopped",
    "readOnlyRootfs": true,
    "noNewPrivileges": true,
    "initEnabled": true,
    "initProcess": "docker-init",
    "capDrop": ["ALL"],
    "capAdd": ["NET_ADMIN", "NET_BIND_SERVICE"],
    "tmpfs": [
      {
        "target": "/run/remnanode",
        "sizeBytes": 4194304,
        "mode": "0700",
        "writable": true,
        "noexec": true,
        "nosuid": true,
        "nodev": true
      },
      {
        "target": "/tmp",
        "sizeBytes": 16777216,
        "mode": "1777",
        "writable": true,
        "noexec": true,
        "nosuid": true,
        "nodev": true
      },
      {
        "target": "/var/log/remnanode",
        "sizeBytes": 29360128,
        "mode": "0750",
        "writable": true,
        "noexec": true,
        "nosuid": true,
        "nodev": true
      }
    ],
    "healthcheck": {
      "test": ["CMD", "/usr/local/bin/remnanode-lite", "healthcheck"],
      "intervalSeconds": 30,
      "timeoutSeconds": 5,
      "startPeriodSeconds": 10,
      "retries": 3
    },
    "logging": {
      "driver": "json-file",
      "options": {"max-size": "2m", "max-file": "2"}
    },
    "nofile": {"soft": 1048576, "hard": 1048576},
    "stopGracePeriodSeconds": 35,
    "memoryLimitBytes": 469762048,
    "memorySwapLimitBytes": 469762048,
    "nanoCPUs": 1000000000,
    "pidsLimit": 256
  },
  "resources": {
    "memoryCurrentBytes": 1,
    "memoryPeakBytes": 1,
    "pidsCurrent": 1,
    "pidsPeak": 1
  },
  "checks": {
    "lowMemoryEnabled": true,
    "asnDatabaseLoaded": true,
    "internalSocketReady": true,
    "listenerReady": true
  },
  "panel": {
    "version": "2.8.1",
    "connected": true,
    "realTrafficPassed": true
  },
  "rawBundleSha256": "<64-lowercase-hex>",
  "signoff": {
    "operator": "luxiaba",
    "role": "release-owner",
    "decision": "accept"
  }
}
```

容器字段必须读取最终 Docker 状态，不能从 Compose 抄入预期值。把 Docker 的
healthcheck 纳秒 duration 转成秒，将 `.HostConfig.Tmpfs` 解析为上面的精确 mount，
并在脱敏的原始采集包中只保留白名单字段。不得保存完整 `docker inspect` 文档或
`.Config.Env`，因为内联部署会在其中暴露 `SECRET_KEY`。

`initProcess` 从容器内 PID 1 读取。container ID、固定到 digest 的 `.Config.Image`
与 `.State.StartedAt` 共同标识完整 600 秒窗口内接受观测的容器。

## Evidence 与数据边界

command 数组不得包含控制字符或空参数。每个参数的大小写不敏感文本还必须避开验证器
禁止的全部字面片段：`secret`、`token`、`jwt`、`authorization`、
`password`、`api-key`、`apikey`、`panel-url`、`panel_url` 和
`://`。即使这些内容只是 flag 名或脱敏占位符也会被拒绝。Panel Secret 只能通过
受限文件描述符或本机 `0600` 文件注入。

提交的 JSON 和保留的原始采集包不得包含 Secret Key、JWT、CA、客户端证书、私钥、
IP、hostname、Panel URL、原始请求/响应或可逆用户数据。bundle digest 会把脱敏采集
材料绑定到签注，但不能证明操作员的观测内容。采集包必须按字段白名单构造，不能先保存
完整 inspect dump 再脱敏。

## 验证

```bash
go run ./cmd/release-evidence-check \
  -manifest docs/development/acceptance/v2.8.0/manifest.json \
  -tag v2.8.0
```

`scripts/release-check.sh` 会调用同一个验证器、重新构建候选 release binaries、运行
完整仓库门禁，并校验最终 tag 位置和 Release note。新候选完成启用规范 healthcheck
的真实 smoke 且 genuine evidence 提交前，release gate 失败属于预期。
