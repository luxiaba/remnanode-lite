<!-- translation: locale=zh-CN; source=docs/development/contract-3.0.0.md; source-sha256=e3a337663de69ae9833ddaf6d516b7f74a1c569a7b6ad889409da1dc59fca191 -->
# Remnawave Node 3.0.0 行为契约基线

> 这是中文译文；涉及契约细节时，请以[英文原文](../../../development/contract-3.0.0.md)为准。

[返回开发指南](README.md)

本文记录 Remnanode Lite 对官方 Remnawave Node `3.0.0` 的兼容基线。
此前的 [2.8.0 基线](contract-2.8.0.md)继续作为不可变历史记录保留；本文只说明
经过审查的版本差异，以及本项目对应实现的行为。

## 固定证据

- 官方仓库：`https://github.com/remnawave/node.git`
- 版本：`3.0.0`
- commit：`46fc5d2d736ff60f6c6a9a56e2661acb95d3f559`
- 上一基线：`2.8.0`，commit
  `596f015a5c8f876dc9a9d61b6cb78d35bd8e379b`
- 外部插件 schema：`@remnawave/node-plugins@0.6.0`

插件 schema 独立发布，不在 Node 仓库内。本次审查使用
`node-plugins-0.6.0.tgz`，SHA-1 为
`278e0d0e9180f22144580e1ad1589d82588bb285`，integrity 为
`sha512-q82oHyZxqw0OdbTyC6fDs6s+Wbky9HzVL36T/nDyRA4BKdboOuOH58tET0YiO12M1kQFXqiMsEci0ZEB1ONKmQ==`。
复核步骤见[外部插件 schema 证据](testing.md#外部插件-schema-证据)。

`internal/contract/official-source-manifest.json` 固定了可执行契约使用的官方
Git blob。CI 直接检查固定 commit，不信任 checkout 工作区内容。

## 官方差异审查

官方 `3.0.0` 在 `2.8.0` 之后有 8 个 commit。大部分改动是把官方 TypeScript
构建从 Webpack 迁移到 Rspack、调整 import 和更新内部依赖，不影响 Go 实现的兼容面。

真正与兼容有关的变化是：

- Zod 从 `3.25.76` 升级到 `4.4.3`；
- `@remnawave/node-plugins` 从 `0.4.5` 升级到 `0.6.0`；
- 新增插件阶段 `preStart.cleanupSockets`；
- 官方镜像选择的 Xray Core 从 `v26.6.27` 升级到 `v26.7.28`。

Node 没有新增环境变量。26 条公开 `/node` 路由和 2 条内部路由的 method、path、
请求字段、响应字段和嵌套层级均与 `2.8.0` 相同。

## 启动前清理 Unix Socket

Panel 可以下发下面的插件配置：

```json
{
  "preStart": {
    "enabled": true,
    "cleanupSockets": {
      "enabled": true,
      "files": ["/dev/shm/*.sock"]
    }
  }
}
```

管理端从 `3.1.0` 开始提供会下发该阶段的配置字段。`3.0.0` 管理端可以使用 Node
`3.0.0`，但不会下发 `preStart.cleanupSockets`。

`preStart.enabled` 默认是 `false`。`cleanupSockets` 可选；存在时必须提供
`enabled` 和 `files`。最多接受 64 个绝对路径或 glob pattern。每个路径先 trim，
结果不能为空，也不能包含 NUL。支持官方定义的 `*`、`?` 和方括号 pattern。

Xray 生命周期顺序为：

```text
确认旧 core 已停止
  -> 展开配置的 pattern
  -> 删除匹配的 Unix socket
  -> 启动替代 core
```

每个 pattern 最多处理 256 个匹配。实现使用 `lstat`，因此普通文件、目录和符号链接
不会被跟随或删除；不存在的路径直接忽略。pattern 展开、检查或删除失败只记录日志，
不会阻止 rw-core 启动。只有真正准备 spawn 新进程时才清理；复用当前 core 的未变化
start 请求不会执行清理。

Panel 及其插件配置属于可信管理输入。过宽 pattern 可能删除同一文件系统和权限范围内
其他进程的 Unix socket。运维者应使用 Node 自有运行目录中的精确路径。本项目不会额外
增加与官方行为不同的 allowlist 或 socket 活跃探测。

## Zod 4 接受边界

Panel 正常生成的值不受影响。经过审查的边界变化很小：

- UUID 接受 RFC version 1 到 8 且 variant 合法的值，以及 nil UUID 和小写全 `f`
  最大 UUID；其他 version/variant 形式和大写全 `F` 会被拒绝。
- `fe80::1%eth0` 之类带 scope 的 IPv6 文本会被 Zod 4 IP schema 拒绝。
- `z.int()` 必须是 JavaScript safe integer。插件整数在字段自身 min/max 之前，
  先限制到 `[-9007199254740991, 9007199254740991]`。

Remnanode Lite 保留已有 HTTP 400 验证 envelope，不复制 Zod 4 内部完整的 issue
措辞和嵌套错误元数据，因为 Panel 依赖的是响应契约，而不是官方实现所用校验库的细节。

## 运行时选择

发行 runtime lock 为两个支持架构选择 Xray Core `v26.7.28`，commit
`5ca6f4b7d4dc20a881d4330e498892697627ec0c`。GeoIP 和 GeoSite 随之更新并固定
源码归档与 payload digest；ASN 数据库继续使用原有固定源。Docker 与 Native bundle
读取同一份 lock，并在打包前验证所有资产。

运行时升级属于发行打包，不是新的 Panel API。精确版本、大小、hash、许可证和源码链接
以 `release/runtime-assets.lock.json` 和 bundle notices 为准。

rw-core `v26.7.28` 还改变了 REALITY 的默认兼容边界。入站未设置 `minClientVer` 时，
core 会使用 `26.3.27`；配置虽然可以正常启动，但较旧的 Xray 系客户端和第三方客户端
可能被拒绝。首选做法是升级客户端。显式设置 `minClientVer` 为 `0.0.0` 可以恢复旧兼容
边界，但 rw-core 会警告这可能增加被阻断的风险。Remnanode Lite 保持上游行为，不改写
Panel 下发的配置。

## 已接受的实现差异

除非本文另有说明，`2.8.0` 已记录的实现差异继续成立。Remnanode Lite 仍是单个 Go
进程，不复制官方 Node.js、s6、NestJS、CQRS、Rspack 或 Zod 内部结构，并继续为
512 MiB 目标只保留紧凑的启动后 Xray 状态。

socket glob 使用 Go 标准库，并把结果截断到官方的 256 项处理上限。标准库会先构造完整
匹配切片，而官方异步 iterator 可以流式停止。只有可信管理员在超大目录配置异常宽泛的
pattern 时才可能有明显影响。项目接受这个小风险，以保持零依赖和易审计。

项目还刻意保留两项范围很小的插件校验差异。负数
`torrentBlocker.blockDuration` 没有明确的运维含义，因此本实现会拒绝它，尽管外部包中
未附加约束的 `z.int()` 会接受。Go CIDR parser 会接受 `/024` 这类零填充前缀长度，
而 Zod 4 拒绝这种写法。Panel 不会生成这两种形式，为此修改生产 parser 只会增加复杂度，
不会改善正常运行。

## 验证

源码级验证：

```bash
export REMNANODE_OFFICIAL_SOURCE=/absolute/path/to/remnawave-node-3.0.0
go run ./cmd/contract-source-check -source "$REMNANODE_OFFICIAL_SOURCE"
go test -count=1 ./internal/contract ./internal/nodeapi ./internal/plugin ./internal/xray
```

发布验收还必须确认不可变的 `sha-<commit>` 候选能连接兼容的真实 Panel、启动内置
rw-core 并承载真实代理流量。使用支持 pre-start 的版本（`3.1.0` 或更新版本）验收时，
还必须完成一次“删除陈旧 socket、保留普通文件”的清理验证。主机细节、日志、流量记录
和服务器数据不写入仓库。
