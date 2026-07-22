<!-- translation: locale=zh-CN; source=docs/development/testing.md; source-sha256=8357e8274ce227852d372a3cce81effe7651def54100bc33effe666173637937 -->
# 测试指南

> 这是中文译文；测试规则和命令以[英文原文](../../../development/testing.md)为准。

[返回开发文档](README.md) · [贡献指南](../contributing.md)

本文介绍 Remnanode Lite 的测试层级、平台边界和对应命令。验证范围应与改动风险匹配，同时要清楚区分“本机通过”和“Linux/Panel 生产行为已经验证”。

## 基本原则

- 开发过程中优先运行目标包，逻辑批次结束后再扩大范围。
- 状态、锁、goroutine、取消或生命周期变化必须运行带 `-race` 的测试。
- 官方可见行为变化必须运行固定源码契约测试。
- Linux capability、netlink、nftables、进程组和 cgroup 结论只能由 Linux 测试支持。
- 正式发布前，维护者应使用当前 `main` 的不可变 `sha-<40 位提交>` 候选，在生产限制下连接真实 Panel 并验证真实代理流量。这是人工发布判断；随后从 `main` 发起 release workflow，运行观测不提交到仓库。
- 精确整机 512 MiB 目标、原生安装路径、`arm64` 运行、大用户量、长时间运行和故障注入仍是有价值的后续检查。单元测试不能替代它们没有覆盖的真实环境。
- 测试数据不得包含真实 Secret、JWT、证书、私钥、节点 IP、hostname 或原始响应；宿主清单、容器标识、时间戳、日志和 smoke JSON 也不写入仓库。

## 快速选择

| 场景 | 命令 | 预期成本 |
| --- | --- | --- |
| 修改一个 Go 包 | `go test -count=1 ./internal/<package>` | 低 |
| 修改并发或共享状态 | `go test -race -count=1 ./internal/<package>` | 中 |
| 普通 Go 回归 | `go test -count=1 ./...` | 中 |
| Go 提交前检查 | `bash scripts/check-go.sh` | 中至高 |
| Shell、Docker、workflow 或供应链 | `bash scripts/check-repository.sh` | 中至高 |
| Native bootstrap 或 bundle 格式 | `sh release/native/install_test.sh`、`go test ./cmd/release-tool` | 中至高 |
| Native 生命周期或 service adapter | `go test ./internal/rnlctl ./cmd/rnlctl` | 高 |
| 完整仓库门禁 | `REQUIRE_GOVULNCHECK=1 bash scripts/check.sh` | 高 |
| Linux 网络管理 | 两条 network namespace 集成测试 | Linux/root |
| 低内存预算 | `scripts/test-low-memory.sh --rw-core ...` | Docker/真实 core |
| 官方与候选行为比较 | `go run ./cmd/contract-probe ...` | 隔离验收环境 |
| 正式发布 | `bash scripts/release-check.sh` | 仅用于当前准备发布的 `main` 提交 |

## Go 测试

### 目标包循环

在编辑期间优先运行最接近的包：

```bash
go test -count=1 ./internal/httpserver
go test -run '^TestName$' -count=1 ./internal/httpserver
go test -race -count=1 ./internal/httpserver
```

`-count=1` 会禁用 Go 测试结果缓存，确保命令检查的是当前实现。并发相关改动应使用 `-race`；不要靠插入 `sleep` 掩盖同步或取消传播缺失。Go 竞争检测器（race detector）需要 CGO 和可用的 C 编译器；缺少编译工具链时应先修复开发环境，不能把跳过竞争检测记为通过。

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

脚本不会自动准备官方源码。未设置 `REMNANODE_OFFICIAL_SOURCE` 时，固定 Git object
重建会跳过，但已提交 source manifest 与本地 Go 路由契约的离线对照始终执行；因此
需要对齐官方行为的改动仍应先按下一节准备官方 Git repository。

## 固定官方源码契约测试

从 `internal/version/contract.version` 读取当前契约版本，避免在命令中复制版本号：

```bash
contract_version="$(tr -d '[:space:]' < internal/version/contract.version)"
official_dir="../remnawave-node-official-${contract_version}"

git clone --depth 1 --branch "$contract_version" \
  https://github.com/remnawave/node.git "$official_dir"

export REMNANODE_OFFICIAL_SOURCE="$(cd "$official_dir" && pwd)"
go run ./cmd/contract-source-check
go test -count=1 ./internal/contract
```

`contract-source-check` 直接读取固定提交对象，禁用 replace refs，也不信任 checkout、index 或 `HEAD`。它逐个校验证据 blob 摘要，并从官方 `REST_API`、全局 prefix、route constant 和 controller decorator 重建 method/path manifest。

同一检查还会从 Git tree 枚举 controller 和 module，核验真实 Nest bootstrap、静态 import、严格 metadata、decorator 归属、module 注册可达性，以及内部 controller 的 prefix exclusion。遇到条件表达式、spread、alias、复合 decorator 或未批准的 dynamic module 时会直接失败，不做猜测性提取。随后运行 Go 门禁时可保留环境变量，让 contract package 重复执行这项证据检查：

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

这些命令不会启动官方 Node。机器提取只证明固定源码内容与公开路由映射，没有声称把
完整 Zod 自动翻译为 Go；本地可执行 schema 仍由边界测试覆盖，真实服务行为差分使用
后文的 `contract-probe`。

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
- actionlint 可执行；为复现 CI，使用 `1.7.12`。

安装 Go 工具：

```bash
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
```

不要在本地调用 `scripts/install-ci-checks.sh`。它是 GitHub Runner 初始化脚本，依赖
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

如果 Docker Compose 可用，打包测试还会执行 Compose schema 校验；如果不可用，脚本会明确输出跳过信息，但其他静态策略仍会运行。如果要声称已经完成 Compose 验证，就不能忽略这条跳过提示。

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
`REQUIRE_GOVULNCHECK=1` 且本机没有 govulncheck，它会跳过漏洞扫描；仓库的必需 CI
job 会显式设置该开关，因此发布只能使用已经执行扫描器的候选。

`check.sh` 成功并不能证明生产行为。它不会让候选镜像连接真实 Panel、承载真实流量，也不会覆盖所有支持架构、init 系统、宿主规格、负载和故障路径。

## Native 发行与生命周期测试

修改 `release/native/install.sh`、bundle 格式、`rnlctl`、service 定义、账号所有权、升级、回滚、修复或卸载时运行：

```bash
sh release/native/install_test.sh
go test -count=1 ./cmd/release-tool ./internal/rnlctl ./cmd/rnlctl
go test -race -count=1 ./cmd/release-tool ./internal/rnlctl
```

bootstrap fixtures 覆盖精确版本下载、本地归档摘要、`--yes`、`--prepare-only`、Secret 文件和移动通道拒绝。`internal/rnlctl` 使用临时 root 与 service fake，覆盖严格 manifest、锁和 journal、generation 原子选择、服务状态恢复、回滚、repair、账号所有权及 purge 安全；不会写入真实 `/etc/remnanode-lite` 或启动宿主服务。

当归档结构、runtime assets 或 release 脚本发生变化时，还要构建并验证真实 bundle：

```bash
mkdir -p dist/native
bash scripts/build-native-bundle.sh dist/native amd64 arm64
version="$(sed -n 's/^var Version = "\([^"]*\)"$/\1/p' internal/version/version.go)"
bash scripts/test-native-release-bundle.sh "dist/native/remnanode-lite_${version}_linux_amd64.tar.gz" amd64
bash scripts/test-native-release-bundle.sh "dist/native/remnanode-lite_${version}_linux_arm64.tar.gz" arm64
```

构建需要精确 Go toolchain 和完整的固定 runtime asset cache。只有在 cache 完整时才使用 `RNL_OFFLINE_BUILD=1`。bundle smoke test 会用真实 `rnlctl` 生命周期代码打开生成的归档，在带限制 `umask` 的临时 test root 中安装，同时保留假的 service-manager 边界。若改动影响 service-manager 行为，它不能替代真实 systemd/OpenRC 检查。

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

这些测试只操作隔离 namespace。不要删掉环境变量保护，也不要把测试改为直接操作开发机的默认 network namespace。

## 低内存资源测试

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

带日期的 M6 50,000 用户结果只用于比较后续资源敏感改动，不能自动代表另一个构建的表现。

资源处理、请求解析、配置保留、队列、日志、并发上限或 rw-core 生命周期发生变化时，应运行该测试。结果应记录 cgroup 峰值；单独的 Go 进程 RSS 不是对应指标。详细基线见[资源预算](resource-budget.md)。

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

两份受维护的生产 Compose 都把 service、container 和 hostname 统一为
`remnanode-lite`。Compose 从 `.env` 读取插值，shell 环境优先，并且只有显式映射的
运行变量会进入容器。打包测试会确认两个模板解析为同一服务、覆盖所有受支持的
`.env` 变量，并要求 `SECRET_KEY` 未设置或为空时在 Compose 展开阶段失败。

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

`release-check.sh` 用于当前 `main` 的正式发布准备。它要求工作区干净、版本与带日期的 `CHANGELOG.md` 一致、固定官方源码可验证，并运行完整仓库检查。它在 release workflow 创建 draft Release、随后公开其 tag 前运行。运行时验证不由该脚本读取：维护者应在发起 release workflow 前人工确认同一 `sha-<commit>` 候选能够连接真实 Panel、承载真实代理流量且没有意外生命周期或资源故障，不把运行数据提交到仓库。

具体 tag、版本和 `latest` 语义见[版本策略](../versioning.md)，候选冻结与发布步骤见
[发布流程](../release.md)。

## 按改动选择测试

| 改动 | 最低验证 | 提升验证 |
| --- | --- | --- |
| 纯文档 | `go run ./cmd/docs-check`、`git diff --check`；人工复核命令事实 | 涉及发布/部署时运行对应脚本 |
| 普通 Go 逻辑 | 目标包普通测试 | `bash scripts/check-go.sh` |
| 锁、状态、worker、关闭 | 目标包 race test | 全量 race 与相关生命周期测试 |
| HTTP/API/schema | `nodeapi`、`httpserver`、`contract` | 固定源码契约与黑盒差分 |
| Xray 生命周期 | `xray`、`httpserver` race | 真实候选运行验证；风险需要时运行资源测试 |
| 用户与 stats | `nodehandler`、`stats`、`xrayrpc` | contract response 与 Panel 差分 |
| 插件纯逻辑 | `plugin` race | HTTP lifecycle 交错测试 |
| nftables/socket destroy | 对应 Linux unit test | 两条 namespace 集成测试 |
| 配置/Secret/JWT | `config`、`secret`、`auth`、server security | installer Secret 流程 |
| Native bootstrap | `sh release/native/install_test.sh` | 在目标主机安装精确 Release |
| Native lifecycle/service | `go test ./internal/rnlctl ./cmd/rnlctl`、`go test -race ./internal/rnlctl` | 改动 Native runtime 行为时实测 systemd/OpenRC |
| Docker/Compose | `bash scripts/test-docker-packaging.sh` | 多架构镜像构建，以及严格容器限制下的真实候选验证 |
| 依赖或下载资产 | `go mod tidy -diff`、供应链检查、govulncheck | 双架构构建、SBOM/attestation |
| 项目版本 | `bash scripts/check-version.sh` | release preflight |
| 官方契约升级 | 全契约与固定源码测试 | 全部注册路由黑盒、Panel 全流程 |
| protobuf wire | `scripts/generate-protobuf.sh --check`、`go test ./internal/xrayrpc` | 真实 rw-core 与 golden wire 回归 |
| 资源上限 | 相关 unit/race | 按风险运行 `test-low-memory.sh` 或更长时间的压力测试 |

“最低验证”适合开发循环，不代表 PR 一定只需要这一列。改动跨越多个组件时取各行并集。

## CI 对应关系

`.github/workflows/ci.yml` 的必需门禁由四组并行作业和一个汇总作业组成：

| CI job | 主要命令 |
| --- | --- |
| `go` | 固定官方源码 + `scripts/check-go.sh` |
| `repository` | 安装固定静态工具 + `scripts/check-repository.sh` |
| `native` | `sh release/native/install_test.sh`、runtime lock 校验 |
| `netadmin` | 两条 Linux namespace 集成测试 |
| `gate` | 要求上述所有 job 都为 success |

并非所有 PR 都会执行 candidate workflow；`main` 的每次 push 则会构建 manifest、生成证明，再发布不可移动的 `sha-<commit>` 候选。手工发起的 release workflow 会校验该候选的 OCI 结构与 attestation，并把同一镜像摘要晋升为正式标签。不要把只在部分 PR 出现的 candidate job 配成所有 PR 都必须出现的门禁。

## 编写测试

- 优先使用标准库 `testing`、局部测试替身和窄接口，不为断言语法引入依赖。
- 使用 `t.TempDir()`、`t.Setenv()` 和测试专属端口，禁止写真实系统路径。
- 并发测试使用 channel、context 或明确同步信号，不靠 `sleep` 碰运气控制执行顺序。
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
- `release-check.sh` 是最终源码预检，不是日常开发循环中的普通命令。
- 成功构建 Go 二进制不代表 Docker 固定资产、多架构或 Linux capability 已验证。
