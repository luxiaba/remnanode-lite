<!-- translation: locale=zh-CN; source=docs/development/README.md; source-sha256=307ccd46540d259324d216bd86dede137bc062acb88f9f78ffdaeae15ec8fb98 -->
# 开发指南

> 这是中文译文；涉及开发规则时，请以[英文原文](../../../development/README.md)为准。

本文面向第一次接触 Remnanode Lite 的维护者，介绍环境准备、代码定位、修改和本地验证。完成这些工作不需要启动真实节点，也不需要准备 Panel 或 `SECRET_KEY`。

项目的生产运行目标是 Linux，但日常 Go 开发可以在 Linux 或 macOS 上进行。
涉及 nftables、socket destroy、进程组和 cgroup 的结论必须回到 Linux 环境验证。

## 15 分钟上手路径

第一次接手不需要先读完所有长文档：

1. 阅读[项目说明](../project.md)的目标、边界和当前状态。
2. 按本页准备工具链，并跑通“第一次验证”。
3. 用下方代码地图找到目标包，先读同目录实现和测试。
4. 从[架构说明](../architecture.md)只选择目标组件的数据流、所有权和并发章节。
5. 按[测试指南](testing.md)选择与改动风险匹配的最小验证集合；准备提交时再阅读[贡献指南](../contributing.md)。

再根据改动类型选择深入阅读的资料：修改 Panel 可见行为时读[当前契约基线](contract-2.8.0.md)，修改版本或镜像时读[版本策略](../versioning.md)，准备正式版本时读[发布手册](../release.md)。[文档总览](../README.md)提供完整的角色导航。

## 开发环境

### 必需工具

- Git。
- Bash 4 或更高版本；CI 与安装脚本建议 Bash 5。macOS 自带 Bash 3.2 不支持脚本使用的 `${var,,}` 等语法。
- `go.mod` 中 `toolchain` 指定的完整 Go 版本，当前为 Go `1.26.5`。
- `gofmt`，随 Go 工具链安装。
- C 编译器与可用的 CGO；`check-go.sh` 无条件运行 race detector。macOS 安装 Xcode Command Line Tools，Linux 安装对应 build toolchain。
- GNU `timeout`；供应链和安装器检查会直接调用该命令。macOS 可安装 Homebrew `coreutils` 并把 `$(brew --prefix coreutils)/libexec/gnubin` 加入 `PATH`。

推荐使用与 CI 完全相同的 Go patch 版本。普通 `go test` 可能允许 Go 自动下载
工具链，但发行构建会设置 `GOTOOLCHAIN=local` 并拒绝版本不一致的本地工具链。

以下工具只在相应检查中需要：

- ShellCheck `0.11.0`：Shell 与 OpenRC 静态检查。
- actionlint `1.7.12`：GitHub Actions 静态检查。
- govulncheck `1.1.4`：可达 Go 漏洞扫描。
- Docker Engine 与 Docker Compose v2：Compose 校验、镜像构建和资源测试。
- Linux 的 `iproute2`、`nftables`、`unshare` 与 root 权限：网络管理集成测试。

仓库当前没有 `Makefile`。`scripts/*.sh` 是本地与 CI 共用的真实入口，文档中的
命令也直接调用这些脚本，避免维护另一套含义相近但行为不同的包装目标。

`scripts/install-ci-checks.sh` 是 GitHub Runner 专用脚本，它要求
`GITHUB_PATH`、`RUNNER_TEMP` 和 Linux 工具链。本地开发不要直接执行它。
actionlint 与 govulncheck 可以按 CI 固定版本安装：

```bash
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
export PATH="$(go env GOPATH)/bin:$PATH"
```

使用自定义 `GOBIN` 时，应把该目录加入 `PATH`，并用 `command -v actionlint govulncheck timeout` 确认脚本能够找到工具。

ShellCheck 必须由系统包管理器或官方发行包安装为 `0.11.0`。安装后先检查：

```bash
go version
shellcheck --version
actionlint -version
govulncheck -version
```

### 获取代码

维护者从稳定开发分支开始工作：

```bash
git clone git@github.com:luxiaba/remnanode-lite.git
cd remnanode-lite
git switch dev
go mod download
```

为具体改动创建短生命周期分支，不要直接在 `main` 上开发：

```bash
git switch -c fix/short-description
```

分支和 Pull Request 规则见[贡献指南](../contributing.md)。官方
`remnawave/node` 是行为证据来源，不是本仓库的 Git 上游；不要把它合并、rebase
或设置为本仓库的代码同步目标。

### 第一次验证

普通测试和构建不需要 `.env`、Panel、`SECRET_KEY` 或本地 rw-core：

```bash
go test -count=1 ./...
mkdir -p bin
go build -trimpath -o bin/remnanode-lite ./cmd/remnanode-lite
./bin/remnanode-lite version
```

上述二进制适合 CLI smoke test。完整守护进程还需要有效的 Panel Secret、mTLS
材料、rw-core、Linux capability 和受支持的主机网络环境，不应把在 macOS 上直接
运行守护进程当作集成验收。

## 固定官方契约源码

大多数 Go 测试可以独立运行。涉及 Panel API、请求/响应 schema、错误语义或
官方行为证据的修改，还应准备当前契约版本对应的官方源码 checkout：

```bash
contract_version="$(tr -d '[:space:]' < internal/version/contract.version)"
official_dir="../remnawave-node-official-${contract_version}"

git clone --depth 1 --branch "$contract_version" \
  https://github.com/remnawave/node.git "$official_dir"

export REMNANODE_OFFICIAL_SOURCE="$(cd "$official_dir" && pwd)"
go run ./cmd/contract-source-check
go test -count=1 ./internal/contract
```

`cmd/contract-source-check` 不读取 checkout 中的文件，而是直接从固定提交的 Git 对象重建 `official-source-manifest.json`。它会校验包名、版本和所有证据 blob 的 SHA-256，再从 `REST_API` 与 NestJS controller decorator 独立提取 method/path。它还会核对 Git tree 中的 controller/module 清单，并确认从 `AppModule` 可以到达每个已注册的 controller。

工作区是否有未提交修改、暂存区内容、replace refs 和当前 `HEAD` 都不会影响结果。固定对象缺失、摘要变化或本地路由漂移都会使检查失败。该工具不尝试自动翻译完整 Zod，也不会下载外部插件 schema。
该目录应放在仓库之外；`.official-source/` 即使意外放进仓库也会被忽略，不应提交。

## 代码地图

### 可执行入口

| 路径 | 职责 |
| --- | --- |
| `cmd/remnanode-lite` | CLI、配置装配、HTTPS 与内部 socket 启动、信号和有界关闭 |
| `cmd/contract-probe` | 对官方节点和候选节点执行 mTLS 黑盒契约比较 |
| `cmd/contract-source-check` | 从固定官方 Git object 重建并核验源码证据 manifest |
| `cmd/asn-builder` | 将固定 ASN 数据源构建为紧凑的只读前缀数据库 |
| `cmd/docs-check` | 校验 Markdown 结构、相对链接、锚点和入口可达性 |

### 运行时包

| 路径 | 职责与所有权 |
| --- | --- |
| `internal/config` | 有界解析配置文件与环境覆盖，不执行服务编排 |
| `internal/secret`、`internal/auth` | Secret 解码、mTLS 材料和 JWT 验证 |
| `internal/bodylimit` | 压缩请求解码、正文与解压资源上限 |
| `internal/httpserver` | TLS、认证、路由、传输错误、容量限制和跨组件 lifecycle gate |
| `internal/nodeapi` | 请求 DTO、联合类型、JSON 解码与官方错误模型 |
| `internal/stats` | 统计用例，不拥有 rw-core 进程 |
| `internal/nodehandler` | 用户增删、查询和连接清理用例 |
| `internal/plugin` | 插件快照、计划、torrent report、nftables 与 operation gate |
| `internal/xray` | rw-core 进程、配置、hash、日志和生命周期的唯一所有者 |
| `internal/xrayrpc` | 与 rw-core 通信的最小 protobuf/gRPC 客户端 |
| `internal/xrayrpc/wire` | `internal/xrayrpc` 使用的最小生成 protobuf wire 类型 |
| `internal/unixconfig` | rw-core 读取配置及发送 webhook 的内部 Unix socket 服务 |
| `internal/connections`、`internal/netadmin` | 用户/IP 连接解析与 Linux socket destroy |
| `internal/system`、`internal/asn` | 系统指标、网络监控和紧凑 ASN 查询 |
| `internal/contract` | 固定官方版本的可执行行为契约与差分语义 |
| `internal/version` | 项目版本与官方契约版本；二者含义见版本策略 |

### 工程与交付目录

| 路径 | 职责 |
| --- | --- |
| `.github/workflows/ci.yml` | 必需的 Go、仓库、installer 与 Linux 网络管理 CI 门禁 |
| `.github/workflows/container.yml` | candidate workflow：候选多架构镜像构建、attestation、不可变候选标签和记录已接受 digest 的 `release-index.json` 资产 |
| `.github/workflows/release.yml` | 以 draft-first 方式发布：校验已接受 digest 绑定，在公开 Release 前晋升精确镜像，验证 immutable 状态后再次确认精确标签，再推进移动通道 |
| `.github/workflows/reconcile.yml` | 从已公开 Release 中已 attestation 的 index 幂等恢复精确镜像标签及其有资格拥有的 `latest` 或 `preview` 通道 |
| `.github/workflows/contract-sync.yml`、`.github/workflows/security.yml` | 官方版本监测与定时安全检查 |
| `scripts/check*.sh` | Go、仓库、供应链和完整门禁的稳定入口 |
| `cmd/rnlctl`、`release/native/install.sh`、`internal/rnlctl` | Native bundle 安装、generation 事务、升级、回滚、修复和卸载 |
| `deploy/` | systemd/OpenRC service、原生 `node.env` 与生产单文件 Compose 模板 |
| `compose.yaml`、`compose.build.yaml` | GHCR 运行配置与本地源码构建覆盖层 |
| `Dockerfile` | 双架构 Node、固定 rw-core/geo/ASN 资产和最小 runtime 镜像 |

请求的主链路是：

```text
Panel
  -> HTTPS/mTLS/JWT
  -> httpserver（路由、限流和 lifecycle gate）
  -> nodeapi（解析与校验）
  -> stats / nodehandler / plugin
  -> xray.Manager
  -> xrayrpc gRPC
  -> rw-core
```

更完整的状态所有权、内部 webhook 和关闭顺序见[架构说明](../architecture.md)。

## 日常开发循环

1. 先确认改动属于哪个组件，以及是否改变官方可见行为。
2. 阅读同目录实现与测试，先运行最接近的包测试。
3. 修改代码并执行 `gofmt`，不要顺手格式化无关文件。
4. 重复运行目标包测试；并发或状态修改至少追加目标包 race test。
5. 按[测试指南](testing.md)的改动矩阵补充契约、Shell、Docker 或 Linux 测试。
6. 检查 diff、文档与 CHANGELOG，再提交一个逻辑完整的阶段性变更。

典型快速循环：

```bash
go test -count=1 ./internal/xray
gofmt -w internal/xray/changed_file.go internal/xray/changed_file_test.go
go test -race -count=1 ./internal/xray
git diff --check
git diff
```

测试范围应与改动风险相匹配。完整仓库检查适合在一个逻辑批次完成后或提交 Pull Request 前运行，不必每次小改动都执行。

正式发布前，维护者应使用不可变的 `sha-<main commit>` 候选镜像确认它能在仓库维护的 Compose 限制下正常启动、连接真实 Panel 并承载真实代理流量。随后从当前 `main` 以精确源码版本发起 release workflow；它会验证候选镜像、Native 资产、attestation 和 `release-index.json` 记录的已接受 digest，创建并校验 draft Release，在公开前晋升精确镜像，确认最终 Release 已变为 immutable 后再次确认精确标签，再推进对应移动通道。稳定版推进 `latest`，预发布版只推进 `preview`。公开后若 registry 晋升失败，使用 `reconcile-release` 从 immutable Release 资产恢复精确标签和符合条件的通道，不会重新构建。运行观测只用于人工发布判断，不作为文件提交到仓库。

## 常见修改路径

| 修改内容 | 通常涉及 | 需要特别保持的边界 |
| --- | --- | --- |
| 新增或调整 `/node` 路由 | `internal/contract`、`internal/httpserver/node_routes.go`、`internal/nodeapi`、对应 service | method/path、请求、响应、错误和副作用必须共同对齐 |
| Xray 启停或配置更新 | `internal/xray/lifecycle.go`、`manager.go`、`process_*`、`apiconfig.go` | Manager 是进程唯一所有者；保留取消、超时和关闭顺序 |
| 用户热更新 | `internal/nodehandler`、`internal/xray/handler.go`、`internal/xrayrpc` | gRPC 结果与 hash commit 必须保持事务语义 |
| 统计语义 | `internal/stats`、`internal/xrayrpc/stats.go`、HTTP route | 特别检查 reset、缺失值和官方响应 schema |
| 插件或 nftables | `internal/plugin`、`internal/connections`、`internal/netadmin` | lifecycle lease 先于 plugin operation gate；Linux 集成测试不可省略 |
| 配置、Secret 或认证 | `internal/config`、`internal/secret`、`internal/auth`、`internal/httpserver` | 有界输入、文件安全、Secret 不进入日志 |
| Linux 系统能力 | `*_linux.go` 与对应 `*_stub.go` | 非 Linux 仍须编译；Linux 行为必须在 Linux 验证 |
| Docker 镜像 | `Dockerfile`、`compose*.yaml`、`.dockerignore`、candidate workflow | 资产固定摘要、多架构、资源限制与非持久日志 |
| 安装、升级、卸载 | `scripts/`、`deploy/` | 锁、原子替换、回滚、权限和 systemd/OpenRC 对称性 |
| 项目版本 | `internal/version`、安装脚本、Compose、发布 workflow | 不要把项目版本与契约版本重新耦合 |
| 官方契约升级 | `internal/version/contract.version`、`internal/contract`、source manifest、CI 固定 ref、契约文档 | 固定源码 commit，先机器提取并评审 diff 再实现，不自动宣称完整 Zod 等价 |

每类修改的最低测试集合见[测试指南](testing.md#按改动选择测试)。

## 工程约束

### 兼容性优先

外部 API 不能按项目偏好自由设计。不要根据“更合理”的直觉改变状态码、JSON shape、缺失值、
错误文本类别或副作用；先从固定官方源码和黑盒行为取得证据，再更新可执行契约和实现。

### 资源必须有界

项目面向 `512 MiB RAM / 1 vCPU / 2 GB disk` 主机。新增请求体、缓冲区、日志、
队列、缓存、goroutine、并发槽或外部命令输出时，必须给出清晰上限和失败语义。
不要用长期保留完整 Xray 配置换取调试便利。

### 状态必须有唯一所有者

- `xray.Manager` 拥有 rw-core 进程和 Xray 生命周期。
- `plugin.Service`/`plugin.State` 拥有插件状态与防火墙计划。
- `httpserver` 协调跨 Xray 与 Plugin 的请求锁序。

不要在新 handler 中绕开这些入口直接改共享状态，也不要引入反向锁序。

### 取消与关闭是正常路径

可能阻塞的 I/O、外部命令、gRPC 和 gate 等待都应接收并传播 `context.Context`。
新增后台 worker 时必须说明谁启动、谁停止、队列满时如何失败，以及关闭时如何等待。

### 平台差异必须显式

Linux 专属实现使用 build tag，并提供可编译的非 Linux stub。macOS 适合快速开发，
但 stub 返回成功或不可用都不能证明 Linux 的 capability、netlink、nft 或进程组语义。

### 生成文件和固定资产

`internal/xrayrpc/wire/wire.pb.go` 是生成文件，不要手工编辑。仓库使用 `protoc 35.1` 和
`protoc-gen-go v1.36.11`，由固定入口重新生成：

```bash
scripts/generate-protobuf.sh
go test -count=1 ./internal/xrayrpc
scripts/generate-protobuf.sh --check
```

脚本要求本机 `protoc --version` 精确输出 `libprotoc 35.1`，并在隔离临时目录安装固定
版本的 Go plugin；`--check` 会重新生成后逐字节比较。修改 wire schema 还必须证明
golden wire、Handler/Stats 和真实 rw-core 集成语义没有意外漂移。

Docker 基础镜像、GitHub Actions、rw-core、ASN 源和下载资产均有固定版本或摘要。
升级时必须同时更新校验脚本和来源说明，不能只替换 URL。

## 下一步

完成第一次普通测试后，按修改目标继续阅读：

- API、行为对齐：[当前契约基线](contract-2.8.0.md)。
- 并发和状态所有权：[架构说明](../architecture.md)。
- 本地与 CI 验证：[测试指南](testing.md)。
- 提交与评审：[贡献指南](../contributing.md)。
- 版本或发布：[版本策略](../versioning.md)与[发布流程](../release.md)。
