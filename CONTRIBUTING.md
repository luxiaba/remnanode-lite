# 贡献指南

感谢你改进 Remnanode Lite。本项目维护一个面向低资源 Linux 节点的 Go 实现，外部
行为以固定版本的官方 Remnawave Node 为契约，同时保持独立的代码、架构和发行节奏。

贡献的首要目标是：行为可证明、状态所有权清晰、资源有界、失败可恢复，并让下一位
维护者能够理解和验证这次变化。

## 开始之前

请先阅读：

- [开发指南](docs/development/README.md)：环境、代码地图和常见修改路径。
- [架构说明](docs/architecture.md)：运行链路、依赖方向、并发与生命周期边界。
- [测试指南](docs/development/testing.md)：测试层级和按改动选择测试。
- [版本策略](docs/versioning.md)：项目版本与官方契约版本的独立语义。
- [文档总览](docs/README.md)：部署、运维、契约和发布资料入口。

发现安全漏洞或 Secret 泄漏风险时，不要先创建公开 Issue；按
[SECURITY.md](SECURITY.md) 的私密渠道报告，不要公开利用细节。

## 分支模型

- `main` 是受保护的发布分支，只接收从 `dev` 提升且已通过代码门禁的候选；该提交合入后才冻结为 M8 发布候选，验收完成后允许发布资料专用分支按发布白名单进入。
- `dev` 是稳定开发与集成分支，普通功能和修复最终合入这里。
- 日常工作使用从最新 `dev` 创建的短生命周期主题分支。

开始一个变更：

```bash
git fetch origin
git switch dev
git pull --ff-only origin dev
git switch -c fix/short-description
```

推荐分支前缀：

- `feat/`：新增官方契约能力或项目能力。
- `fix/`：修复行为、资源、安全或运维问题。
- `refactor/`：不改变外部行为的结构调整。
- `test/`：测试与验收工具。
- `docs/`：文档。
- `chore/`：依赖、CI、发布和维护工作。

普通 Pull Request 的目标分支是 `dev`。维护者准备发布时先创建 `dev -> main` Pull Request；冻结候选验收完成后，还会按[发布流程](docs/release.md#7-受保护-main-下提交发布资料)创建只含 README、CHANGELOG、roadmap、evidence 和 Release note 的 `release/v*-docs -> main` 最终化 PR。除这两个受控入口外，不要直接在 `main` 开发，也不要在功能 PR 中创建或移动正式 tag。

官方 `remnawave/node` 只作为协议和行为参考。不要把它的分支 merge/rebase 到本项目，
也不要通过 Git 历史同步实现；固定源码应放在仓库外，由契约测试验证精确 commit。

## 确定变更范围

提交代码前先回答：

1. 这是内部实现变化，还是 Panel/rw-core 可观察行为变化？
2. 哪个组件应拥有新状态、进程、队列或后台任务？
3. 输入、内存、磁盘、并发和外部命令输出的上限是什么？
4. 请求取消、进程退出、部分失败和重复调用时发生什么？
5. macOS stub、Linux 实现、Docker、systemd 和 OpenRC 中哪些路径受影响？
6. 哪些测试能证明行为，而不只是执行到了代码？

范围较大的变更应先在 Issue 或设计说明中写清契约、所有权、迁移和验证计划。明确的
小修复可以直接提交 PR，但描述仍需说明原因和测试。

## 实现规范

### Go 风格

- 所有 Go 文件必须通过 `gofmt`；不要混入无关格式化或重命名。
- 优先使用标准库和现有内部包，不为少量语法便利引入依赖。
- 包名、类型名和函数名表达领域职责，避免 `util`、`common`、`manager2` 等模糊容器。
- 在消费方定义小接口；不要为了 mock 预先抽象没有第二个真实边界的代码。
- 构造函数建立完整不变量，必需依赖不应在运行中悄悄为 `nil`。
- 使用 `fmt.Errorf("operation: %w", err)` 添加操作上下文，并保留可检查的错误链。
- 注释解释不明显的约束、证据或锁序，不复述代码表面动作。
- 新代码必须配套测试；bug 修复优先加入能稳定复现问题的回归测试。

### 契约与 HTTP 边界

`/node` API 不是自由设计面。method/path、请求 shape、联合类型、默认值、成功响应、
错误类别、状态码、连接关闭和副作用都可能被 Panel 依赖。

涉及外部行为时：

1. 在固定官方源码中找到证据。
2. 更新 `internal/contract` 的路由、schema、语义或来源清单。
3. 更新 `internal/nodeapi` 与 `internal/httpserver`。
4. 通过现有 service 进入 Xray、Stats、Handler 或 Plugin，不在 route 中复制领域逻辑。
5. 添加请求、响应、错误和副作用测试。
6. 在候选阶段使用 `contract-probe` 与真实官方节点比较。

不要因为另一种 JSON 或错误格式“更符合 Go 习惯”就偏离已验证的官方行为。

### 状态、并发与生命周期

- `xray.Manager` 是 rw-core 进程、配置、hash 和生命周期的唯一所有者。
- `plugin.Service` 与 `plugin.State` 拥有插件快照、防火墙计划和 torrent report。
- `httpserver` 的 lifecycle gate 协调跨 Xray 与 Plugin 的操作。
- 固定锁序是外层 lifecycle lease、Plugin operation gate、Manager 内部状态。
- 不要从新内部入口绕过 gate，也不要在持有内层锁时反向获取外层锁。

所有可能阻塞的 I/O、外部命令、gRPC、队列和 gate 等待都应传播
`context.Context`。新增 goroutine 或 worker 时必须有明确所有者、停止信号、等待路径、
队列容量和过载响应；不允许无法关闭的后台任务。

并发变化至少运行目标包 race test，并增加确定性的交错测试。不要用长时间 sleep
代替同步信号。

### 资源约束

生产目标是整机 `512 MiB RAM / 1 vCPU / 2 GB disk`。任何新增资源都必须有界：

- HTTP 和压缩后的请求体。
- JSON、protobuf、命令 stdout/stderr 和文件读取。
- channel、report、缓存、map 与 goroutine 数量。
- handler、连接、Xray start 和批处理并发。
- 日志单文件、轮转数量、tmpfs 和持久磁盘写入。
- 启动、停止、重试和关闭总时间。

优先流式处理或保留 hash/摘要，不长期保存大配置的第二份副本。资源变化应说明最坏
情况，并按[测试指南](docs/development/testing.md)决定是否重跑 50k 用户资源门禁。

### 安全与 Secret

- 不记录或提交 `SECRET_KEY`、JWT、客户端证书、私钥和完整认证 header。
- 不把真实节点 IP、hostname 或原始 Panel 响应写入测试 fixture 和 acceptance evidence。
- 配置和 Secret 文件读取必须有大小、类型、symlink、owner/mode 与稳定读取保护。
- 外部命令使用参数数组和有界输入输出，不拼接未经验证的 shell 文本。
- HTTP 客户端和探针必须验证 TLS，不增加通用的 insecure 跳过开关。
- Docker 与原生服务继续遵守最小 capability、只读 rootfs/文件权限和
  `no-new-privileges` 约束。

如果在本地 diff、日志或测试输出中发现真实 Secret，先停止传播和提交，再完成轮换与
清理；不要仅在后续 commit 中删除后继续公开原有历史。

### Linux 与跨平台

生产运行目标是 Linux。Linux 专属能力使用 `//go:build linux`，并提供非 Linux stub
保证日常开发平台可编译。stub 只表达“不可用”或可移植退化行为，不能伪造 nftables、
netlink、capability 或进程组成功。

修改 Linux 专属路径时：

- 在 macOS/Linux 运行普通包测试。
- 在 Linux 运行对应 unit test。
- 涉及 nftables 或 socket destroy 时运行隔离 namespace 集成测试。
- 涉及 service manager 时同时考虑 systemd 与 OpenRC。

完整命令见[测试指南](docs/development/testing.md#linux-网络管理集成测试)。

### Shell、安装器和 service 文件

- Bash 脚本使用 `set -euo pipefail`，OpenRC 文件保持 POSIX `sh` 兼容。
- 所有 shell/service 文件保持 LF；`.gitattributes` 已固定行尾。
- 文件替换使用受限临时目录、校验后原子 rename，并保留清晰回滚点。
- installer 的共享锁、信任根、下载预算、路径验证和 Secret 迁移不可绕过。
- systemd 与 OpenRC 的用户、capability、资源上限、停止和卸载语义应保持对称。
- 修改安装器必须运行离线操作测试，不要用真实主机安装代替失败注入覆盖。

### 生成文件、依赖与供应链

`internal/xtls/xrpc/wire.pb.go` 是生成文件，不得手工编辑。wire schema 变化必须通过
`scripts/generate-protobuf.sh` 使用固定的 `protoc 35.1` 与 `protoc-gen-go v1.36.11`
生成，并运行 protobuf golden test；提交前用 `scripts/generate-protobuf.sh --check`
证明生成结果没有漂移。

新增或升级依赖时：

- 说明标准库或现有依赖为何不足。
- 运行 `go mod tidy -diff`、`go mod verify` 和 govulncheck。
- 评估二进制大小、初始化成本、常驻内存和 transitive dependency。
- GitHub Actions 使用完整 40 位 commit SHA。
- Docker 基础镜像、下载资产、rw-core 与 ASN 来源使用固定版本和 SHA-256。
- 同步更新供应链检查，不允许只改 URL 让静态门禁失去覆盖。

## 文档与变更日志

代码与文档是同一个变更的一部分。以下变化必须同步文档：

- 用户可见配置、默认值、资源限制或部署步骤。
- API、官方契约版本或已知差异。
- 架构边界、锁序、状态所有权或关闭语义。
- 分支、CI、版本、镜像标签或发布流程。
- 安装、升级、回滚和卸载行为。

面向使用者的变化更新 `docs/CHANGELOG.md`。不要把未完成验收写成已发布事实，也不要
在多份文档复制容易漂移的“当前状态”；优先链接版本策略、契约或 release note 的
单一事实源。

纯文档变更至少运行：

```bash
go run ./cmd/docs-check
git diff --check
```

文档包含命令时，应在适用环境实际执行或明确标注前置条件、示例占位符与破坏性范围。

## 测试要求

测试范围按风险决定，完整矩阵见[测试指南](docs/development/testing.md#按改动选择测试)。
最低要求：

- 开发循环运行目标包测试。
- 状态或并发变化运行目标包 race test。
- API 变化运行 `nodeapi`、`httpserver`、`contract` 和固定源码证据测试。
- Shell/部署变化运行仓库门禁；installer 变化追加离线操作测试。
- nftables/netlink 变化运行 Linux namespace 测试。
- 资源上限或大配置路径变化运行低内存门禁。

提交 PR 前，尽量运行与 CI 等价的仓库检查：

```bash
REMNANODE_OFFICIAL_SOURCE=/absolute/path/to/pinned-official-source \
REQUIRE_GOVULNCHECK=1 \
  bash scripts/check.sh
```

该命令不包含真实 Panel、Linux network namespace、真实 rw-core 或长期 soak。无法在
本地运行的平台测试应在 PR 中明确说明，不能写成已经通过。

## Commit

采用 Conventional Commits 风格：

```text
feat(contract): support a verified node route
fix(xray): preserve cancellation while waiting for startup
refactor(plugin): isolate firewall plan construction
test(installer): cover failed service rollback
docs: add the developer testing guide
chore(deps): update grpc with verified module state
```

要求：

- 标题使用祈使语气并说明结果，不写 `update files`、`fix bug` 等模糊描述。
- scope 使用稳定组件名，例如 `xray`、`plugin`、`contract`、`installer`、`container`。
- 一个 commit 包含一个可解释、可验证的逻辑变化，但不要机械拆分同一修复的每个小步骤。
- 不混入无关格式化、生成文件或本地配置。
- 提交前检查 `git diff --cached --check` 和 staged diff。

较大工作可以按“契约/核心实现/部署与文档”等逻辑 checkpoint 分成少量 commit，确保
每个 checkpoint 都能独立评审，不要求为每次微调创建 commit。

## Pull Request

普通 PR 目标为 `dev`。描述应包含：

- 问题和用户可观察影响。
- 方案及为何属于当前组件。
- 官方契约证据或“不改变外部行为”的说明。
- 并发、资源、安全和平台影响。
- 实际执行的命令与结果；明确列出未运行的环境测试。
- 配置、部署、迁移、回滚和文档变化。

提交前检查：

- [ ] 分支基于最新 `dev`，PR 目标是 `dev`。
- [ ] diff 不包含 Secret、本地 `.env`、真实节点资料或无关改动。
- [ ] Go 代码已 `gofmt`，module 状态未意外变化。
- [ ] 已添加或更新回归测试，并按风险运行对应检查。
- [ ] Linux-only 行为没有仅凭 macOS stub 宣称通过。
- [ ] 用户可见变化已更新文档和 CHANGELOG。
- [ ] 新依赖、Action 和下载资产已固定并完成供应链检查。
- [ ] 没有提前创建、覆盖或移动正式 tag。

Review 重点依次是正确性与契约、状态/并发、资源边界、安全、可维护性和风格。评审中
发现实现方向需要改变时，优先修正设计和测试，不通过增加注释为错误所有权辩护。

## 发布边界

普通贡献完成于合入 `dev`，不负责自行发布。`dev -> main` 提升、候选镜像、正式 tag、
`latest`、Release 资产和 acceptance evidence 由维护者按[版本策略](docs/versioning.md)
与[发布流程](docs/release.md)统一处理。

发布门禁要求冻结候选、干净工作树、固定官方源码和真实验收资料。不要在普通 PR 中
修改检查以绕过尚未完成的 evidence，也不要将 `edge` 或 commit SHA 镜像描述为正式版。

## 许可证

提交贡献即表示你有权提供相关代码和文档，并同意其按仓库的
[AGPL-3.0-only](LICENSE) 许可证发布。引用或改写外部实现时必须确认来源与许可证兼容，
并在需要时保留归属信息。
