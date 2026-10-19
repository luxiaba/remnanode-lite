<!-- translation: locale=zh-CN; source=docs/development/contract-3.4.2.md; source-sha256=50de99c09e66f8ec9aa54f9fa00b1538e7d5046ad06570bd24763c3660a2862d -->

# Remnawave Node 3.4.2 行为契约基线

[英文原文](../../../development/contract-3.4.2.md) · [开发指南](README.md) · [上一版基线](contract-3.4.1.md)

本次审查范围是官方 `3.4.1..3.4.2`。公开 API 保持 25 条，历史契约文档保持不变。

## 固定证据

- 官方 Node `3.4.2`：`ea7a835485af81ec464ebbf7acaaa7d6dd486a05`。
- 上一版 `3.4.1`：`44912631321664dbd5822e9bf8d96766ccff7c93`。
- 官方源码内 `libs/contract/package.json` 仍为 `@remnawave/node-contract@3.4.1`；
  cipher 枚举已经变化，因此以固定 Git 对象为证据，不能只看 npm 包版本。
- 插件 `@remnawave/node-plugins@0.8.3`，XTLS SDK `0.17.0`。
- rw-core `v26.7.28` 和 GeoCheck `v0.3.0` 保持不变。

插件归档 SHA-1 为 `ffe6e9bc719bce8bdbaf4042723762503f149953`，解压后后端 schema
SHA-256 为 `4b6f8cff65d4ca1cfa192d6e512396ae207d849045759b64538c1734091b2836`。
归档完整性值和 SDK 证据见英文原文；已使用的 Handler/Stats RPC 方法与字段编号保持兼容。

## 启动后 Webhook

插件新增可选配置：

```json
{"postStart":{"enabled":true,"webhook":{"enabled":true,"url":"https://example.com/core-events"}}}
```

`postStart.enabled` 默认 false；`webhook` 可省略，但存在时必须提供布尔 `enabled`
和 HTTP(S) `url`，即使该项关闭也要校验。该功能不依赖 nftables。清理或禁用插件会
清理通知设置，不会单独重启正在运行的 core。

实际启动的新 core 通过 gRPC readiness 并提交 running 后，Node 在后台发送 POST，
包含 `scope=service`、`event=service.core_started`、UTC 毫秒时间戳，以及经过校验的
`internals.metadata` 五个字段：`name`、`uuid`、`id`、`tags`、`countryCode`。
缺省元数据为 `{}`，未知字段被丢弃。未变化配置的成功同步和失败启动不发送通知；
强制重启或配置变化只在成功后发送。

HTTP 超时为 5 秒，发送失败不改变启动结果。通知与 Torrent 共用 64 项队列和 4 个
出站 HTTP 槽位；URL 和正文在入队前固定，关闭时取消并等待发送结束。Lite 保留资源
限制：URL 最多 512 字节；元数据编码前预算为 8 KiB（含 tag 分隔开销）。元数据超限
或队列/发送容量不足时记录诊断并跳过通知，不停止 core；日志不包含 URL 凭据、查询
token 或元数据。

## Shadowsocks Cipher

`POST /node/handler/add-user` 不再接受 `cipherType=9`（NONE）。允许的值是
`-1`、`0`、`5`、`6`、`7`、`8`。DTO 与独立契约都在产生副作用前拒绝旧值。

## 交付与验证

Go 工具链与 Docker builder 升至 `1.26.9`，修复阻断依赖 PR 的标准库漏洞。独立打包的
rw-core/GeoCheck 不会因此被重新构建，也不宣称其旧工具链漏洞已消除。

验证覆盖固定源码 oracle、请求/响应、启动事件、队列和取消、race、Linux namespace
及真实 core 资源测试。正式发布仍按[发布流程](../release.md)验收不可变候选、兼容 Panel
连接与真实代理流量。
