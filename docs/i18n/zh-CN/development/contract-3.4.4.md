<!-- translation: locale=zh-CN; source=docs/development/contract-3.4.4.md; source-sha256=e7c8f6c624e24d887a4997f9943c2670edfbaa25a4d4ff061cf2aec8e214c843 -->

# Remnawave Node 3.4.4 行为契约基线

[英文原文](../../../development/contract-3.4.4.md) · [开发指南](README.md) · [上一版基线](contract-3.4.3.md)

## 固定证据

- 官方 Node `3.4.4`：`c1e40e6d27c68e8a2325419a6c193488d8952984`。
- 上一版 `3.4.3`：`2ad154f8f1016c94c1872f337268af878af61b18`。
- [官方差异](https://github.com/remnawave/node/compare/3.4.3...3.4.4)更新了 Xray-core
  到 `v26.10.31`，并更新官方 Node 版本及 npm 依赖元数据。

## Lite 对齐

公开 API 保持 25 条，官方 Node 的 controller、schema、认证、插件行为和生命周期契约未变。
Lite 的项目版本和契约版本更新为 `3.4.4`，并固定新的官方来源提交。

Lite 不使用官方 Node.js/npm 运行时；本次相关变化是 amd64 和 arm64 内置 Xray-core
从 `v26.7.28` 更新到 `v26.10.31`。精确归档和二进制摘要固定在
[`release/runtime-assets.lock.json`](../../../../release/runtime-assets.lock.json)，来源和第三方说明同步更新。

3.4.3 继承的行为（包括启动后 webhook）保持不变。验证重点是官方差异、来源证据、
Xray 资产锁、版本一致性以及必需的 CI 和不可变发布候选检查。
