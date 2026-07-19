# 测试指南

[返回开发文档](README.md) · [贡献指南](../../CONTRIBUTING.md)

本文说明 Remnanode Lite 的测试层级、平台边界和可执行命令。目标不是让每次修改都
重复最昂贵的门禁，而是让验证范围与改动风险匹配，并清楚区分“本机通过”和
“Linux/Panel 生产语义已经验证”。

## 基本原则

- 开发过程中优先运行目标包，逻辑批次结束后再扩大范围。
- 状态、锁、goroutine、取消或生命周期变化必须运行 race test。
- 官方可见行为变化必须运行固定源码契约测试。
- Linux capability、netlink、nftables、进程组和 cgroup 结论只能由 Linux 测试支持。
- 真实 Panel、真实 rw-core、资源门禁和 soak 是候选验收，不是普通单元测试的替代品。
- 测试数据不得包含真实 Secret、JWT、证书、私钥、节点 IP、hostname 或原始响应。

## 快速选择

| 场景 | 命令 | 预期成本 |
| --- | --- | --- |
| 修改一个 Go 包 | `go test -count=1 ./internal/<package>` | 低 |
| 修改并发或共享状态 | `go test -race -count=1 ./internal/<package>` | 中 |
| 普通 Go 回归 | `go test -count=1 ./...` | 中 |
| Go 提交前检查 | `bash scripts/check-go.sh` | 中至高 |
| Shell、Docker、workflow 或供应链 | `bash scripts/check-repository.sh` | 中至高 |
| Installer 事务 | `bash scripts/test-install-ops.sh` | 高 |
| 完整仓库门禁 | `REQUIRE_GOVULNCHECK=1 bash scripts/check.sh` | 高 |
| Linux 网络管理 | 两条 network namespace 集成测试 | Linux/root |
| 低内存预算 | `scripts/test-low-memory.sh --rw-core ...` | Docker/真实 core |
| 官方与候选行为比较 | `go run ./cmd/contract-probe ...` | 隔离验收环境 |
| 正式发布 | `bash scripts/release-check.sh` | 冻结候选专用 |

## Go 测试

### 目标包循环

在编辑期间优先运行最接近的包：

```bash
go test -count=1 ./internal/httpserver
go test -run '^TestName$' -count=1 ./internal/httpserver
go test -race -count=1 ./internal/httpserver
```

`-count=1` 禁用 Go 测试结果缓存，避免把旧成功结果误认为当前实现已经通过。
定位并发问题时保留 `-race`；不要通过增加 sleep 掩盖缺少同步或取消传播的问题。
Go race detector 需要 CGO 和可用的 C 编译器；缺少编译工具链时应先修复开发环境，不能把跳过 race test 记为通过。

### 普通全量回归

```bash
go test -count=1 ./...
```

该命令会运行当前平台能编译的所有普通测试。仓库中的真实集成测试还受环境变量保护，
未显式启用时会 `Skip`。

在 macOS 上，带 `//go:build linux` 的测试和实现不会参与编译，包括 Linux 进程、
nftables 与 netlink socket destroy。因此 macOS 的 `go test ./...` 适合快速回归，
但不等于 Linux 全量通过。Linux 上的普通 `go test ./...` 会编译 Linux 单元测试，
network namespace 与真实 rw-core 测试仍需显式开启。

### 标准 Go 门禁

```bash
bash scripts/check-go.sh
```

该脚本依次执行：

1. 工作树与暂存区 whitespace 检查。
2. 所有已跟踪和未忽略 Go 文件的 `gofmt` 检查。
3. 项目版本格式、契约版本格式、跨文件同步和正式对齐版本约束检查。
4. `go mod verify` 与 `go mod tidy -diff`。
5. 普通全量测试。
6. 全量 race test。
7. `go vet ./...`。

脚本不会自动准备官方源码。未设置 `REMNANODE_OFFICIAL_SOURCE` 时，固定官方源码
证据测试会跳过；因此需要对齐官方行为的改动应先按下一节准备 checkout。

## 固定官方源码契约测试

从 `internal/version/contract.version` 读取当前契约版本，避免在命令中复制版本号：

```bash
contract_version="$(tr -d '[:space:]' < internal/version/contract.version)"
official_dir="../remnawave-node-official-${contract_version}"

git clone --depth 1 --branch "$contract_version" \
  https://github.com/remnawave/node.git "$official_dir"

export REMNANODE_OFFICIAL_SOURCE="$(cd "$official_dir" && pwd)"
go test -count=1 ./internal/contract
```

测试会校验官方 checkout 的精确 commit，而不是只比较版本字符串。随后运行 Go 门禁时
保留该环境变量：

```bash
REMNANODE_OFFICIAL_SOURCE="$REMNANODE_OFFICIAL_SOURCE" \
  bash scripts/check-go.sh
```

适用改动包括：

- `/node` method/path 或路由数量。
- 请求字段、联合类型、默认值或未知字段处理。
- 成功响应、应用错误、HTTP 状态或传输关闭语义。
- stats reset、用户 mutation、插件同步等副作用。
- 官方契约版本或固定 commit 更新。

只运行 `go test ./internal/contract` 不会启动官方 Node；它验证固定源码证据和本地
可执行 schema。真实服务行为差分使用后文的 `contract-probe`。

### 外部插件 schema 证据

官方 Node 的插件 `config` schema 来自独立 npm 包，不在固定源码 checkout 内。当前
`@remnawave/node-plugins@0.4.5` tarball 可以在隔离临时目录中复核：

```bash
plugin_tgz="$(mktemp)"
trap 'rm -f "$plugin_tgz"' EXIT

curl --fail --location --silent --show-error \
  --proto '=https' --tlsv1.2 \
  https://registry.npmjs.org/@remnawave/node-plugins/-/node-plugins-0.4.5.tgz \
  -o "$plugin_tgz"

test "$(openssl dgst -sha1 "$plugin_tgz" | awk '{print $NF}')" = \
  3bfc3988278790ec40a93d6e6169f893c31bf62d
test "sha512-$(openssl dgst -sha512 -binary "$plugin_tgz" | openssl base64 -A)" = \
  'sha512-r9Lce/l/kHQATNhWbcutApFSJ5hH/Yu6Kv0+/qjpUDIEa1+DFb54Q8IwuvqWzxxbGkG9oO0cAeN4busBzz0a5Q=='
tar -tzf "$plugin_tgz" \
  | grep -Fx 'package/build/backend/models/node-plugins.schema.js'
```

检查实际 schema 时使用 `tar -xOf` 从上述固定路径读取，不要安装包或执行其中代码。
当前 CI 不联网下载该 tarball；自动源码证据测试只覆盖官方 Git checkout 中登记的
路径。升级插件版本时必须同时核对官方 `package.json`/`package-lock.json`、更新摘要、
重新审计 schema，并调整 `internal/nodeapi`、`internal/plugin` 和相关契约测试。

## 仓库与静态检查

### 工具版本

`scripts/check-repository.sh` 要求：

- Go toolchain 与 `go.mod` 完全一致。
- ShellCheck 恰好为 `0.11.0`。
- actionlint 可执行；为复现 CI，使用 `1.7.7`。

安装 Go 工具：

```bash
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
```

不要在本地调用 `scripts/install-ci-checks.sh`。它是 GitHub Runner bootstrap，依赖
`GITHUB_PATH`、`RUNNER_TEMP`、Linux 归档和 `sha256sum`。

### 仓库门禁

```bash
bash scripts/check-repository.sh
```

该脚本执行：

- `git diff --check`。
- `go run ./cmd/docs-check`，检查 Markdown H1、围栏、本地链接、锚点和入口可达性。
- ShellCheck、所有 Bash 脚本的 `bash -n` 和 OpenRC 脚本的 `sh -n`。
- actionlint。
- Docker/Compose 打包策略检查。
- 下载源、固定摘要、Action SHA 和 installer bootstrap 等供应链检查。
- 使用精确 Go toolchain 交叉构建 Linux `amd64` 与 `arm64` 二进制。

如果 Docker Compose 可用，打包测试还会执行 Compose schema 校验；如果不可用，脚本
会明确输出跳过信息，但其他静态策略仍会运行。需要宣称 Compose 已验证时，不应忽略
这条 skip。

### 漏洞扫描与完整仓库检查

```bash
govulncheck ./...
```

日常完整仓库入口是：

```bash
REMNANODE_OFFICIAL_SOURCE="$REMNANODE_OFFICIAL_SOURCE" \
REQUIRE_GOVULNCHECK=1 \
  bash scripts/check.sh
```

`check.sh` 组合 Go 门禁、仓库门禁、离线 installer 测试和 govulncheck。若未设置
`REQUIRE_GOVULNCHECK=1` 且本机没有 govulncheck，它会跳过漏洞扫描；因此发布前和
需要报告完整结果时必须显式要求该工具。

即便该命令成功，它也不包含 Linux network namespace、真实 rw-core、Panel 黑盒、
资源门禁或长期 soak，不应描述为生产验收完成。

## Installer 测试

安装、升级、卸载、service unit、OpenRC 或 `install-env-helpers.sh` 变化至少运行：

```bash
bash scripts/test-install-ops.sh
bash scripts/check-repository.sh
```

`test-install-ops.sh` 使用临时目录和命令 mock，离线验证锁、权限、路径安全、Secret
迁移、原子替换、失败回滚、systemd/OpenRC 状态转换和卸载隔离。它不会改动真实
`/etc/remnanode` 或启动本机服务。

测试中的部分 `flock` 分支只有系统存在 `flock` 时才运行。macOS 结果不能替代
Ubuntu/OpenRC 的真实安装验收；installer 行为变化仍应等待 CI 的 Ubuntu job，并在
候选阶段按发布验收协议验证真实主机。

## Linux 网络管理集成测试

在具备 user/network namespace、nftables 和 root 权限的 Linux 主机上：

```bash
sudo env "PATH=$PATH" REMNANODE_NFT_INTEGRATION=1 \
  go test ./internal/plugin \
  -run '^TestNFTManagerInNetworkNamespace$' -count=1 -v

sudo env "PATH=$PATH" REMNANODE_SOCKET_KILL_INTEGRATION=1 \
  go test ./internal/netadmin \
  -run '^TestKillSocketsInNetworkNamespace$' -count=1 -v
```

推荐使用 Ubuntu 24.04，与 CI 一致：

```bash
sudo apt-get update
sudo apt-get install --yes iproute2 nftables
```

这些测试只操作隔离 namespace。不要删掉环境变量保护，也不要把测试改为直接操作
开发机默认 network namespace。

## 低内存资源门禁

资源测试将测试进程与真实 rw-core 放在同一个 Docker cgroup 中，默认使用
`448 MiB / 1 CPU / no swap / 256 PIDs / 50,000 users`：

```bash
scripts/test-low-memory.sh \
  --rw-core /path/to/linux/rw-core \
  --users 50000 \
  --memory 448
```

前置条件：

- Docker daemon 正常运行。
- `--rw-core` 指向与 Docker 架构相同的可执行 Linux rw-core。
- 宿主机支持 Docker memory、CPU、swap 和 PID 限制。

资源、请求解析、配置保留、队列、日志、并发上限或 rw-core 生命周期变化时需要运行。
结果应记录 cgroup peak；单独的 Go 进程 RSS 不是门禁判定值。详细基线见
[资源预算](resource-budget.md)。

## Docker 与镜像测试

只验证策略和 Compose schema：

```bash
bash scripts/test-docker-packaging.sh
```

本地源码镜像构建会下载固定的基础镜像、rw-core、geo 和 ASN 资产，成本明显高于
Go build，仅在 Dockerfile、构建参数或运行资产发生变化时执行：

```bash
SECRET_KEY=packaging-check \
  docker compose -f compose.yaml -f compose.build.yaml build
```

`packaging-check` 只用于 Compose 解析，不能启动节点。真实启动必须使用 Panel 生成的
完整 Secret，并遵循 [Docker 部署文档](../deployment-docker.md)的安全要求。

## 黑盒契约比较

先查看路由及其是否允许默认探测：

```bash
go run ./cmd/contract-probe -list
```

准备由同一 CA 签发的 Panel 客户端证书和单独保存的 JWT：

```bash
export REMNANODE_CONTRACT_CA=/secure/ca.pem
export REMNANODE_CONTRACT_CERT=/secure/panel-client-cert.pem
export REMNANODE_CONTRACT_KEY=/secure/panel-client-key.pem

go run ./cmd/contract-probe \
  -token-file /secure/panel.jwt \
  -target official=https://127.0.0.1:2222 \
  -target candidate=https://127.0.0.1:3222
```

第一个 target 是比较基线。默认只运行无破坏性的安全路由；启动、停止、用户增删、
连接清理、统计 reset、report drain 和 nftables 操作必须同时显式指定 `-routes` 与
`-allow-mutating`，并且只能在隔离验收环境执行。

探针不会输出 JWT 或原始响应 body。证书只包含 DNS 名称但 target 使用 IP 时，传入
`-server-name`；工具不提供跳过 TLS 验证的选项。

## 发布门禁

```bash
RELEASE_TAG=<tag> \
REMNANODE_OFFICIAL_SOURCE="$REMNANODE_OFFICIAL_SOURCE" \
REQUIRE_GOVULNCHECK=1 \
  bash scripts/release-check.sh
```

该脚本只适用于已经冻结且具备真实 acceptance evidence 的发布候选。它要求工作树
干净、release note 和 CHANGELOG 已最终化、证据 manifest 可校验、候选 ancestry
合法，并运行完整仓库检查。普通开发分支缺少这些资料时失败是正常行为，不要通过
伪造 evidence、放宽检查或提前修改发布状态让它变绿。

具体 tag、版本和 `latest` 语义见[版本策略](../versioning.md)，候选冻结与发布步骤见
[发布流程](../release.md)。

## 按改动选择测试

| 改动 | 最低验证 | 提升验证 |
| --- | --- | --- |
| 纯文档 | `go run ./cmd/docs-check`、`git diff --check`；人工复核命令事实 | 涉及发布/部署时运行对应脚本 |
| 普通 Go 逻辑 | 目标包普通测试 | `bash scripts/check-go.sh` |
| 锁、状态、worker、关闭 | 目标包 race test | 全量 race 与相关生命周期测试 |
| HTTP/API/schema | `nodeapi`、`httpserver`、`contract` | 固定源码契约与黑盒差分 |
| Xray 生命周期 | `xray`、`httpserver` race | 真实 rw-core、Panel、资源门禁 |
| 用户与 stats | `nodehandler`、`stats`、`xtls` | contract response 与 Panel 差分 |
| 插件纯逻辑 | `plugin` race | HTTP lifecycle 交错测试 |
| nftables/socket destroy | 对应 Linux unit test | 两条 namespace 集成测试 |
| 配置/Secret/JWT | `config`、`secret`、`auth`、server security | installer Secret 流程 |
| Shell/service | `bash scripts/check-repository.sh`、`bash scripts/test-install-ops.sh` | 真实 systemd/OpenRC |
| Docker/Compose | `bash scripts/test-docker-packaging.sh` | 多架构镜像构建与候选部署 |
| 依赖或下载资产 | `go mod tidy -diff`、供应链检查、govulncheck | 双架构构建、SBOM/attestation |
| 项目版本 | `bash scripts/check-version.sh` | release preflight |
| 官方契约升级 | 全契约与固定源码测试 | 全部注册路由黑盒、Panel 全流程 |
| protobuf wire | `scripts/generate-protobuf.sh --check`、`go test ./internal/xtls` | 真实 rw-core 与 golden wire 回归 |
| 资源上限 | 相关 unit/race | `test-low-memory.sh` 与 soak |

“最低验证”适合开发循环，不代表 PR 一定只需要这一列。改动跨越多个组件时取各行并集。

## CI 对应关系

`.github/workflows/test.yml` 的 required gate 汇总四组并行 job：

| CI job | 主要命令 |
| --- | --- |
| `go` | 固定官方源码 + `scripts/check-go.sh` |
| `repository` | 安装固定静态工具 + `scripts/check-repository.sh` |
| `installer` | `scripts/test-install-ops.sh` |
| `netadmin` | 两条 Linux namespace 集成测试 |
| `gate` | 要求上述所有 job 都为 success |

容器 workflow 按路径触发，不是所有 PR 都有 container check。`main` 上命中容器输入
时会先构建并证明 manifest，再发布不可移动的候选 tag；正式版本由 tag release workflow
晋升 acceptance manifest 绑定的同一 digest。不要把路径条件导致的
“未运行”误判为失败，也不要把可选 container job 配成永远要求出现的通用门禁。

## 编写测试

- 优先使用标准库 `testing`、局部 fake 和窄接口，不为断言语法引入依赖。
- 使用 `t.TempDir()`、`t.Setenv()` 和测试专属端口，禁止写真实系统路径。
- 并发测试使用 channel、context 或明确同步信号，不依赖脆弱 sleep 排序。
- 每个可能阻塞的测试都有 deadline；失败消息说明实际值、期望值和操作阶段。
- Linux 集成测试必须有 build tag 与显式环境变量保护。
- 契约测试同时覆盖合法输入、缺失字段、错类型、联合类型、额外字段和响应 schema。
- 资源测试关注有界峰值和失败语义，不只记录平均值或单进程 RSS。
- 修复 bug 时先添加可稳定复现的回归测试，再修改实现。

## 常见误区

- `go test ./...` 显示成功，但官方源码证据测试可能因未设置环境变量而跳过。
- macOS 成功不覆盖任何 `//go:build linux` 文件。
- `check.sh` 在未安装 govulncheck 时默认允许跳过，完整报告应设置
  `REQUIRE_GOVULNCHECK=1`。
- `check-repository.sh` 在没有 Docker Compose 时允许跳过 Compose schema 验证。
- `release-check.sh` 不是普通开发命令，未完成候选证据时预期失败。
- 成功构建 Go 二进制不代表 Docker 固定资产、多架构或 Linux capability 已验证。
