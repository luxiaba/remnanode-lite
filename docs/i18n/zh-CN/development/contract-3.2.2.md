<!-- translation: locale=zh-CN; source=docs/development/contract-3.2.2.md; source-sha256=f8d2a23ffa1944278c2f0fc613ce26fed3491c91da8c744500b6e5ed12fc5ee8 -->
# Remnawave Node 3.2.2 行为契约基线

> 这是中文译文；涉及契约细节时，请以[英文原文](../../../development/contract-3.2.2.md)为准。

[返回开发指南](README.md) | [上一版 3.0.0 基线](contract-3.0.0.md)

本文记录官方 Remnawave Node `3.2.2` 的兼容基线。此前的版本文档继续作为
不可变历史记录保留。本文说明经过审查的 `3.0.0..3.2.2` 差异、Remnanode Lite
实现的等价行为，以及刻意不同于官方 Node.js 和 s6 布局的实现边界。

## 固定证据

- 官方仓库：`https://github.com/remnawave/node.git`
- 版本：`3.2.2`
- commit：`2c532c4e33bf5864e9867a7bdc36245cc1057eb1`
- 上一基线：`3.0.0`，commit
  `46fc5d2d736ff60f6c6a9a56e2661acb95d3f559`
- 官方契约包：`@remnawave/node-contract@3.2.0`
- 外部插件 schema：`@remnawave/node-plugins@0.6.3`

本次审查使用的插件归档为 `node-plugins-0.6.3.tgz`，SHA-1 为
`9562fe8a6d90ec646023211ee7487cbede91fcdc`，integrity 为
`sha512-WBuY6PeSe8Sm/3mPWHPACDjOPrLE/bHwzQZiUYwF8L+Ww3q8f+5gVdRHZY+V+c+pm5ozhxRxrzyphgKg3jb7hw==`。
插件 schema 不在 Node Git 仓库内，必须按照
[外部插件 schema 证据](testing.md#外部插件-schema-证据)单独审查。

`internal/contract/official-source-manifest.json` 固定了官方源码身份和每个已审查
blob。3.2.2 manifest 包含 75 个证据文件，以及与 3.0.0 相同的 26 条机器提取
公开路由。提取器只为官方 `IntegrationsModule` 的计算型 imports 表达式新增了一项
精确的 fail-closed 许可；源码路径、callee 或表达式中任意一项发生变化都会被拒绝。

## 对齐状态

| 范围 | 本基线状态 | 兼容含义 |
| --- | --- | --- |
| 公开路由和响应 | 已确认不变 | 26 条公开路由的 method、path、响应 envelope 和响应字段均保持对齐。 |
| Start 可选 internals | 已实现 | 按官方 optional/null 边界接收并校验 `metadata` 和 `integrations`。 |
| Integration 执行 | 官方 stock 版本没有实现 | 官方 3.2.2 未提供任何 `*.integration.ts` provider；Remnanode Lite 接收传输字段，但不增加独立扩展运行时。 |
| Torrent 报告 webhook | 已实现并完成独立复核 | 即使外部投递失败，本地阻断和报告收集仍是权威结果。 |
| Core 生命周期和版本上报 | 等价实现 | Go 进程所有者已提供有界 SIGINT/SIGKILL 升级和基于可执行文件的版本探测。 |
| Panel 选择的 Core 和 GeoData | 已实现并完成独立复核 | 下载使用派生的持久 cache，发行版固定的内置资产始终作为 fallback。 |

实现完成和代码复核本身不构成发布证据。本基线仍需通过下文所述的组合测试、不可变
候选、真实 Panel 连接和代表性代理流量验证。

## 官方差异审查

官方 3.2.2 在 3.0.0 之后包含 24 个 commit。与兼容有关的变化是：

- `POST /node/xray/start` 新增可选的 `internals.metadata` 和
  `internals.integrations` 字段；
- 新增 Integration module 框架，但 stock 3.2.2 源码树没有内置具体 Integration；
- 通过 `xrayConfig.geodata` 描述可选的自定义 Core 和 GeoData 下载；
- `@remnawave/node-plugins@0.6.3` 新增 `torrentBlocker.webhookUrl`；
- 改为从可执行文件探测 Core 版本，并支持预发布版本；
- 延长受监督的停止窗口，并加入 SIGKILL 升级。

其余变化属于构建工具、框架包和源码组织调整，不改变公开 HTTP 接口。公开 `/node`
路由仍为 26 条，另有两条面向 Core 的内部路由。响应 schema、HTTP method、路由 path、
鉴权规则和成功 envelope 均未变化。

## Start 请求 Internals

Panel 现在可以额外发送下面的数据：

```json
{
  "internals": {
    "metadata": {
      "name": "node-1",
      "uuid": "66baa45a-c6a2-44f8-80ac-2095dcfc4b6a",
      "id": 42,
      "tags": ["edge"],
      "countryCode": "NL"
    },
    "integrations": {
      "example": {
        "enabled": true
      }
    },
    "forceRestart": false,
    "hashes": {
      "emptyConfig": "empty-hash",
      "inbounds": []
    }
  },
  "xrayConfig": {}
}
```

两个新字段都可选。省略字段可以通过校验，显式 JSON `null` 会被拒绝。存在
`metadata` 时，示例中的五个字段全部必填：`name`、`uuid` 和 `countryCode` 是
string，`id` 是 number，`tags` 是 string 数组。官方 schema 不对这些值附加
UUID、国家代码、整数、正数或非空约束。未知 metadata 字段按照普通 Zod object
语义被剥离。

`integrations` 必须是使用 string key 的 object。值可以是任意 JSON，包括嵌套
object、array、scalar 和 `null`。请求对象和 `internals` 对象中的未知字段继续被
接受并剥离。已有的 `forceRestart`、hash 和不透明 `xrayConfig` 行为不变。

Remnanode Lite 在 HTTP 边界解析并校验两个字段，然后在映射到 Go Xray 生命周期命令
时丢弃它们。这与官方 stock 3.2.2 的可观测行为一致，因为其 Integration descriptor
列表为空：同步过程不读取任何一个字段就直接返回成功。这不是对未来官方 Integration
的前向承诺。任何包含具体 provider 的版本都需要重新审查行为并明确决定是否实现。

## Torrent 报告 Webhook

0.6.3 插件 schema 在 `torrentBlocker.webhookUrl` 增加了一个可选、语法合法的 URL。
Torrent Blocker 接受 Core 报告后，官方实现先执行正常的本地阻断和报告处理，再以
JSON 发起一次 best-effort HTTP `POST`。投递超时为 5 秒。HTTP status、响应内容和
投递错误都不改变本地结果，也没有重试或持久化出站队列。

Go 实现保留相同的所有权边界。已有的有界内部 Core-webhook 队列继续串行处理本地
nftables 和报告状态。本地操作提交后，若配置了外部地址，则使用
`Content-Type: application/json` 和 5 秒超时尝试投递。失败不影响本地结果。关闭时
会取消正在执行的投递，并停止接收新任务。专项测试、race 测试和完整 plugin 测试均已
通过；正式发布前仍必须通过完整 release gate。

## 生命周期和 Core 版本等价性

官方 3.2.2 将 s6 down 等待时间从 5 秒提高到 10 秒，并配置 supervisor 在 Core
停止卡住时升级到 SIGKILL。Remnanode Lite 不使用 s6：它直接拥有独立进程组中的
rw-core，先发送 SIGINT 并最多等待 5 秒，再发送 SIGKILL 并最多再等待 5 秒。释放
进程所有权前还会确认整个进程组已清理。虽然没有复制 supervisor 机制，但可观测要求
等价。

官方 Node 不再信任 `XRAY_CORE_VERSION` 作为运行时事实来源。它在启动时和 Core
成功启动后执行所选 `/usr/local/bin/rw-core version`，并保留包括 prerelease 在内的
合法 SemVer。Go manager 同样在 readiness 成功后，以有界输出和 timeout 探测所选
可执行文件，并在生命周期提交时原子缓存结果；探测失败后还会执行节流的后台恢复。
显式配置的版本继续作为已接受的运维 override，但不是默认生产事实来源。

官方内置 Core 仍为 `v26.7.28`。因此本基线不会仅仅为了跟随 Node 契约就修改
`release/runtime-assets.lock.json`。

## Panel 选择的 Core 和 GeoData

官方 3.2.2 会解释 `xrayConfig.geodata` 下两个相互独立的可选 section：

```json
{
  "xrayConfig": {
    "geodata": {
      "core": {
        "url": "https://downloads.example/rw-core",
        "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
      },
      "assets": [
        {
          "url": "https://downloads.example/geoip-custom.dat",
          "file": "geoip-custom.dat"
        }
      ]
    }
  }
}
```

Remnanode Lite 通过 `/var/lib/remnanode-lite/panel-runtime/{assets,cores}` 下的派生
cache 实现这些指令。Core 和 asset section 独立校验。下载要求完整 redirect 链始终
使用 HTTPS，采用 15 秒 total timeout、5 秒 idle timeout 和 128 MiB 上限，并以
原子方式发布下载完成的文件。

Core 文件按 SHA-256 content address 存储。只有 configured digest 和可执行文件
`version` 的 SemVer 都通过校验后才会激活 candidate。candidate 无效或下载失败时，
继续保留当前可用 Core，发行版固定的内置 Core 是最终 fallback。GeoData 会复用已有
非空普通文件。缺失 asset 下载失败时，创建与官方兼容的空 stub 并继续；默认 GeoIP
和 GeoSite path 会 overlay 内置文件，因此固定副本始终可用。

Docker 继续使用只读 root filesystem，并通过 named volume 持久化该 cache。Native
安装使用已有、由 service 账号所有的 `/var/lib/remnanode-lite` 层级。这个派生 cache
刻意不进入已安装 generation、`release/runtime-assets.lock.json` 或 `rnlctl` transaction
journal：它是 Panel 选择的运行时状态，不是发行资产，也不属于某次发行的可复现身份。

## 已接受的实现差异

除非本文覆盖，3.0.0 基线中的所有已接受差异继续成立。Remnanode Lite 仍是单个 Go
进程，不复制 NestJS/CQRS/s6 内部结构，并继续使用有界的请求、配置、队列和进程所有权
模型。

空的 Integration 框架通过接收公开请求字段来表示，而不是增加推测性的动态加载。
动态 Core 和 GeoData 使用有界派生 cache，而不是修改打包路径。这样既保留项目维护的
只读容器和 Native generation 模型，又保持 Panel 可见的选择和 fallback 行为。

## 验证

源码级验证：

```bash
export REMNANODE_OFFICIAL_SOURCE=/absolute/path/to/remnawave-node-3.2.2
go run ./cmd/contract-source-check -source "$REMNANODE_OFFICIAL_SOURCE"
go test -count=1 ./internal/contract ./internal/nodeapi ./internal/plugin ./internal/xray
go test -race ./internal/contract/... ./internal/nodeapi ./internal/plugin ./internal/xray
```

完整发布 gate 还必须覆盖 plugin webhook 投递及失败、Core digest/version 拒绝、成功
选择自定义 Core、GeoData cache 复用和 stub fallback、只读容器 profile 下 cache
持久化，以及无网络时回退到内置资产。

发布验收仍必须确认不可变的 `sha-<commit>` candidate 可以连接兼容的真实 Panel、启动
可用 Core 并承载真实代理流量。主机细节、日志、流量记录、下载的运行时 payload 和
服务器数据不写入仓库。
