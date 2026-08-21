<!-- translation: locale=zh-CN; source=docs/development/contract-3.3.2.md; source-sha256=70d41c6d53708e5100de3feae4af9e755d37500df271d19435056e2302265435 -->

# Remnawave Node 3.3.2 行为契约基线

[English source](../../../development/contract-3.3.2.md) · [返回开发指南](README.md) · [上一版 3.2.2 基线](contract-3.2.2.md)

本文记录官方 Remnawave Node `3.3.2` 的兼容性基线。更早的版本文档保持为不可变的
历史记录。本文说明已审查的 `3.2.2..3.3.2` 差异、Remnanode Lite 中的等价行为，
以及 Go 运行时继续保留的有界实现差异。

## 固定证据

- 官方仓库：`https://github.com/remnawave/node.git`
- 版本：`3.3.2`
- Commit：`afdfa2d837118efd95c317700e60e9429a169b48`
- 上一基线：`3.2.2`，commit
  `2c532c4e33bf5864e9867a7bdc36245cc1057eb1`
- 官方契约包：`@remnawave/node-contract@3.2.3`
- 外部插件 schema：`@remnawave/node-plugins@0.7.3`
- 官方 GeoCheck 资产：`remnawave/geocheck@v0.3.0`，commit
  `50e084bb3ed34b55fc6839fe0dc4bafd9fe275fc`

已审查的插件归档为 `node-plugins-0.7.3.tgz`，SHA-1 是
`d58cc34d15838d6ac543c112ac65265d6189745e`，integrity 值为
`sha512-y1+dIrZVENchojkBJHC5KHocTTDl/xeCdIvbYgoTWYZ5pWuIkyoFYm/fGUA//W3DQ6eAlAKkg1HC24MeKAoIvA==`。
该 schema 仍位于 Node 仓库之外，必须按
[外部插件 Schema 证据](testing.md#外部插件-schema-证据)单独审查。

`internal/contract/official-source-manifest.json` 固定官方源码身份和每个已审查 blob。
3.3.2 manifest 包含 85 个证据文件和 27 条机器提取的公开路由。新增路由为
`POST /node/stats/get-geocheck`；此前 26 条路由的方法和路径保持不变。

## 对齐状态

| 范围 | 本基线状态 | 兼容性含义 |
| --- | --- | --- |
| 公开路由与响应 | 已实现并固定源码 | 旧路由继续对齐；GeoCheck 新增官方成功响应与 `A018` 失败 envelope。 |
| 派生 SNI 门禁 | 已实现 | 只有 ClientHello 携带从 Panel Secret 派生的 server name 时才出示节点证书；随后仍必须通过 mTLS 和 JWT。 |
| Secret 完整性 | 已实现 fail-closed 校验 | 无效或不一致的密钥材料，以及有效期异常的 CA，会在公开 listener 启动前被拒绝。 |
| Torrent `rulePlacement` | 已实现 | 可选插件值按官方默认值与 schema 边界选择 Torrent Blocker 规则位置。 |
| GeoCheck 运行时 | 官方固定二进制加有界执行 | Docker 与 Native Release 打包官方 GeoCheck `v0.3.0`；Node 串行执行并限制时间和输出。 |
| Core 生命周期 | 等价实现 | readiness 期间 rw-core 提前退出会立即报告；现有有界进程组清理保持不变。 |

实现与代码审查本身不是 Release 证据。发布前仍须完成发布手册要求的完整测试、
不可变候选、真实 Panel 连接和代表性代理流量验证。

## 已审查的上游差异

官方 3.2.2 之后与兼容性有关的变化是：

- TLS 证书选择边界新增派生 SNI 检查；
- 解码 Panel Secret 后在启动时检查完整性；
- 新增 `POST /node/stats/get-geocheck` 和错误码 `A018`；
- 官方容器加入 GeoCheck `v0.3.0`；
- 插件 schema `0.7.3` 加入 Torrent Blocker `rulePlacement`；
- rw-core 在 API ready 前退出时会被明确识别。

框架、构建、展示和源码组织变化不影响其他公开路由。Remnanode Lite 通过现有 Go
所有权边界实现可观察行为，不复制 NestJS 或 s6 内部结构。

## 派生 SNI 与 Secret 完整性

预期 TLS server name 由规范化的 JWT 公钥和 CA 证书 PEM body 确定性派生，算法为
HKDF-SHA256，info 为 `rw-v1`。ClientHello 缺少 SNI 或 SNI 不匹配时，不会出示节点
证书。它是额外的预认证门禁，不替代 TLS 1.3、客户端证书验证或 bearer JWT 校验。

完整 `SECRET_KEY` 解码后，会在创建 listener 前检查：

- CA 与节点证书能够解析，且 CA 当前处于有效期；
- CA 签名能够由自身公钥验证；
- 节点证书签名能够由该 CA 验证；
- 节点私钥能够解析并与节点证书匹配；
- JWT 公钥能够解析。

这些检查不会轮换、修复或记录 Secret 内容。失败会明确停止启动。与官方启动检查一样，
节点证书有效期交由正常 TLS 验证路径处理，不额外增加 Lite 独有的启动拒绝规则。

## GeoCheck 路由与运行边界

`POST /node/stats/get-geocheck` 接受包含可选字符串 `ip` 和 `interface` 的对象。
`ip` 经 trim 后优先使用，且必须能够解析为 IPv4 或 IPv6；否则传递非空 interface；
两者都没有时使用默认路由。通常的未知字段剥离、mTLS、SNI、JWT、body 大小和 handler
准入规则继续生效。

Node 使用 `--json --svg-base64 --quiet` 和可选 `--interface` 参数运行固定
`geocheck` 资产。同一时刻只允许一个任务，每次执行限时 45 秒，stdout 与 stderr
分别限制为 32 MiB。成功结果必须是唯一一个 JSON 文档，并包含非空 `image.data`；
该文档放入普通 `response` envelope 返回。无效输入、并发执行、超时、执行失败、输出
超限、无效 JSON 和缺少图片都使用官方 `A018` 应用错误族。

Release lock 记录 Linux `amd64` 与 `arm64` 归档和提取后二进制的精确摘要、上游
commit 与 MIT 许可证。该可执行文件是未经修改的官方 GeoCheck `v0.3.0` 二进制。
上游使用 Go `1.26.5` 构建这个固定二进制；本基线不宣称其中残余的工具链漏洞已经
消除。认证路由、单任务门禁、45 秒期限与 32 MiB 输出限制能够缩小暴露面，但不会
重写或修补该可执行文件。Lite 现有的最小权限容器和 Native 服务配置不会授予
`CAP_NET_RAW`，因此 GeoCheck 会使用上游 TCP fallback，而不是 raw ICMP 或逐跳探测。
报告仍然有效，但网络诊断细节可能少于官方容器。

## Torrent Blocker 规则位置

插件 schema `0.7.3` 新增可选的 `torrentBlocker.rulePlacement`，取值为 `0` 到
`1000` 的 number。省略或设为零时保持默认插入位置；正整数选择指定路由规则位置，
并限制在现有规则列表范围内；非整数继续使用默认位置。该位置进入插件 plan 与 active
snapshot，因此修改它时仍与 Torrent Blocker 其他配置一起执行 validate、apply、commit
事务。

现有 `includeRuleTags`、本地报告收集、连接阻断和 best-effort 报告 webhook 行为不变。

## 生命周期与已接受差异

如果 rw-core 在启动等待 gRPC API 时退出，Remnanode Lite 现有的进程观察器已经会立即
收敛到同样的提前退出失败，不再等待整个 readiness 期限。它仍拥有完整子进程组，并保留
现有的有界 SIGINT/SIGKILL 清理和状态提交规则。

除非本文覆盖，3.2.2 基线中的已接受差异继续有效。Remnanode Lite 仍是单个 Go 进程，
保留明确的请求与资源限制，也不复现官方 Node.js、s6 或控制台展示结构。

## 验证

源码与聚焦验证：

```bash
export REMNANODE_OFFICIAL_SOURCE=/absolute/path/to/remnawave-node-3.3.2
go run ./cmd/contract-source-check -source "$REMNANODE_OFFICIAL_SOURCE"
go test -count=1 ./internal/contract ./internal/httpserver ./internal/secret ./internal/geocheck ./internal/plugin ./internal/xray
go test -race ./internal/httpserver ./internal/geocheck ./internal/plugin ./internal/xray
```

完整 Release gate 还必须在两种打包架构上校验 GeoCheck 资产摘要，通过目标 Panel 验证
SNI 拒绝与接受、GeoCheck 成功和有界失败、Torrent 规则顺序、Core 提前退出、正常 Core
启动及真实代理流量。主机信息、日志、生成的 GeoCheck 报告、流量记录和服务器数据均不
进入仓库。
