<!-- translation: locale=zh-CN; source=docs/development/contract-3.4.1.md; source-sha256=9d959740b105714ef03663f18b4651d2f5eb05d2851f5cecd92dacbcf5133ce7 -->

# Remnawave Node 3.4.1 行为契约基线

[英文原文](../../../development/contract-3.4.1.md) · [返回开发指南](README.md) · [上一版 3.3.2 基线](contract-3.3.2.md)

本文记录官方 Remnawave Node `3.4.1` 的兼容性基线。更早版本的文档保持为不可变
历史记录。本文说明已审查的 `3.3.2..3.4.1` 差异、Remnanode Lite 中的等价行为，
以及 Go 运行时继续保留的有界实现差异。

## 固定证据

- 官方仓库：`https://github.com/remnawave/node.git`
- 版本：`3.4.1`
- Commit：`44912631321664dbd5822e9bf8d96766ccff7c93`
- 上一基线：`3.3.2`，commit
  `afdfa2d837118efd95c317700e60e9429a169b48`
- 官方契约包：`@remnawave/node-contract@3.4.1`
- 外部插件 schema 包：`@remnawave/node-plugins@0.8.2`
- 官方 rw-core：`v26.7.28`（未变）
- 官方 GeoCheck：`remnawave/geocheck@v0.3.0`（未变）

已审查的插件归档为 `node-plugins-0.8.2.tgz`，SHA-1 为
`9588288f9190b73b2ce868845d4248c98eadc25f`，integrity 为
`sha512-/klo/XH4imZ2cupLavj4++S+hHgVA8uzhVgpQdC0y9kzUtVE168d7brcYEdxB1UGw49LsER+UDYplcyTSvV5QQ==`。
解压得到的 `node-plugins.schema.js` SHA-256 为
`e096eba57a8ce1499a0e117bf5b9dfd7f324a9a6fc455066fcb31d5c86a91d21`，与已审查的
0.7.3 schema 逐字节相同。包版本与 Zod 依赖发生变化，但可接受的插件配置没有变化。

`internal/contract/official-source-manifest.json` 固定官方源码身份和所有已审查 blob。
3.4.1 manifest 包含 88 个证据文件和 25 条机器提取的公开路由。

## 对齐状态

| 范围 | 当前状态 | 兼容性含义 |
| --- | --- | --- |
| 公开路由与响应 | 已实现并固定源码 | 两条退役的 inbound-user 查询路由不再暴露；其余 25 条方法、路径、schema 和响应 envelope 保持对齐。 |
| SNI 校验 | 作为可选门禁实现 | 官方默认 `SNI_VERIFICATION=false`。开启后恢复精确的派生 SNI 证书选择门禁；两种模式都继续强制 TLS 1.3、mTLS 与 JWT。 |
| 用户替换清理 | 已实现 | `add-user` 带有 `prevVlessUuid` 时，Node 会在移除前读取用户 IP，并在加入新凭据前尝试断开旧连接。 |
| nftables 运行选项 | 已实现 | ingress drop 日志默认开启；reply-direction 接受默认关闭，可由管理员显式开启。 |
| 插件 schema | 已复核，无 schema 差异 | 0.8.2 与 0.7.3 接受相同的插件文档。 |
| 框架与容器重构 | 不照搬 | Result helper、Nest typed config、Node.js、S6、ASN 解压与 npm native 依赖布局不定义 Go 架构或 Panel wire contract。 |

实现与评审本身不是发布证据。正式发布仍需完成发布手册规定的全量测试、不可变候选、
真实 Panel 连接和代表性代理流量验证。

## 已审查的上游差异

官方 3.3.2 之后与兼容性有关的变化为：

- 删除 `POST /node/handler/get-inbound-users` 与
  `POST /node/handler/get-inbound-users-count`；
- 通过 `SNI_VERIFICATION` 提供可选派生 SNI 校验，默认关闭；
- 新增 `NFTABLES_LOGGING`，官方默认 `true`；
- 新增 `NFTABLES_ACCEPT_REPLY_TRAFFIC`，官方默认 `false`；
- `add-user` 用 `prevVlessUuid` 替换凭据时，尽力断开用户此前的连接。

官方错误/Result helper 重写改变的是 TypeScript 类型和内部控制流，不改变成功响应
envelope、应用错误码或 HTTP 状态行为。Remnanode Lite 继续使用现有明确的 Go Result
与所有权边界。

## 公开路由退役

3.4.1 契约包删除两条 inbound-user 查询命令及路由常量。Remnanode Lite 因而在与
其他未知 Node 路由相同的 handler 前 known-route 边界拒绝这些 method/path。
底层 rw-core gRPC 方法仍可用于有界的运行时与资源测试，但不再属于 Panel-to-Node
HTTP 表面。

注册路由从 27 条减少为 25 条。请求预算、handler admission、源码证据、响应形状测试
和差异探针都使用同一份已审查路由集合。

## 可选派生 SNI

官方 3.3.2 要求每个 TLS ClientHello 都携带派生 server name；官方 3.4.1 将该额外门禁
改为可选并默认关闭。Remnanode Lite 采用同样行为：

- `SNI_VERIFICATION=false` 时正常提供配置的证书，但仍要求 Panel 客户端证书与有效
  bearer JWT；
- `SNI_VERIFICATION=true` 时，只有 ClientHello server name 与规范化 CA、JWT 公钥
  PEM 正确派生的值完全一致，才提供 Node 证书；
- 两种模式都只允许 TLS 1.3。

该开关只改变证书选择边界，不会关闭 Secret 完整性、mTLS、JWT、known-route 检查或
请求资源限制。

## 用户替换连接清理

`add-user` 包含 `prevVlessUuid` 时，官方 Node 会在删除旧 inbound 凭据前读取当前用户
IP 列表，删除后发布连接清理，再加入新凭据。连接清理是尽力操作，不改变成功 HTTP
响应。

Remnanode Lite 在现有 Xray process lease 与串行 mutation gate 中执行相同可观察顺序：

1. 非破坏性读取 IP 统计；
2. 从选定 inbound 移除旧用户/哈希状态；
3. 尝试一次有界、去重的 socket 清理批次；
4. 加入新凭据，只提交成功的 inbound mutation。

若旧用户移除失败，Lite 保留原有可重试语义，不会断开连接或加入新凭据。若请求仍
有效但可选 IP 查询或 socket 清理失败，则记录有界诊断并继续替换凭据，与官方的尽力
清理结果一致。

## nftables 运行选项

`NFTABLES_LOGGING=true` 在每条 ingress 和 Torrent Blocker drop 前加入内核日志表达式。
egress 地址与端口过滤不会记录日志。繁忙服务器若因拦截流量产生过多 kernel/journald
日志，可设置为 `false`。

`NFTABLES_ACCEPT_REPLY_TRAFFIC=true` 会在 IPv4/IPv6 input 与 forward chain 的 ingress
block set 前插入 `ct direction reply accept`。这样可保留宿主机主动连接的回复，同时不
改变 inbound original-direction 与 egress 过滤。开启后 conntrack 可用性会成为建表依赖。
官方默认仍为 `false`，除非管理员主动开启，否则保持原来的无状态 ingress 行为。

这些选项在进程启动时读取，并用于初始化或重建项目自有 nftables 表。它们不会修改
firewalld 或 Remnanode Lite IPv4/IPv6 表之外的任何表。

Lite 保留独立的 TCP/UDP egress 端口规则，不复制 npm native addon 的命名包计数器。
Panel 没有读取这些计数器的路由；在不引入官方内部表布局的情况下，允许、丢弃、日志、
reply-direction、IPv4 与 IPv6 行为仍保持对齐。

## 保留的运行资产与已接受差异

官方 3.4.1 仍打包 rw-core `v26.7.28` 与 GeoCheck `v0.3.0`，因此 Lite 的运行资产锁无需
改变。官方容器还更新了 Node.js、S6 Overlay、ASN 解压和 npm native 依赖；这些是
TypeScript 实现的交付细节，不会复制到 Go 二进制及其独立固定的 Native/Docker 资产。

除非本文明确覆盖，3.3.2 基线中的已接受差异继续有效。Remnanode Lite 仍是一个 Go
进程，保留明确的请求/资源限制及事务化 Xray/plugin 所有权。

## 验证

源码与聚焦验证：

```bash
export REMNANODE_OFFICIAL_SOURCE=/absolute/path/to/remnawave-node-3.4.1
go run ./cmd/contract-source-check -source "$REMNANODE_OFFICIAL_SOURCE"
go test -count=1 ./internal/contract ./internal/httpserver ./internal/config ./internal/nodehandler ./internal/plugin
go test -race ./internal/httpserver ./internal/nodehandler ./internal/plugin
```

完整发布门禁还必须在 Linux namespace 中验证两种 nftables 可选规则形状，使用目标 Panel
验证 SNI 开启/关闭、带真实流量的用户凭据替换、正常 rw-core 启动和不可变候选的代理
流量。宿主机详情、日志、流量记录和服务器数据不得进入仓库。
