<!-- translation: locale=zh-CN; source=docs/development/contract-3.4.3.md; source-sha256=3b58dc8389e8152ae1048f2dc93c9cee365df8beebe2076f80d8e3f7bfe6021c -->

# Remnawave Node 3.4.3 行为契约基线

[英文原文](../../../development/contract-3.4.3.md) · [开发指南](README.md) · [上一版基线](contract-3.4.2.md)

## 固定证据

- 官方 Node `3.4.3`：`2ad154f8f1016c94c1872f337268af878af61b18`。
- 上一版 `3.4.2`：`ea7a835485af81ec464ebbf7acaaa7d6dd486a05`。
- [官方差异](https://github.com/remnawave/node/compare/3.4.2...3.4.3)只有三个文件：
  `docker/Dockerfile`、`package.json`、`package-lock.json`。

## 官方变化

两个 Docker 阶段的 Node.js 镜像从 `24.20-trixie-slim` 更新到 `24.22-trixie-slim`，
对应官方报告的 Brotli 安全修复 CVE-2026-111497。包及 lockfile 根版本更新为 `3.4.3`。

公开 API 保持 25 条，controller、命令 schema、认证、core 生命周期及插件行为均未变化。
契约 npm 包仍声明 `3.4.1`；插件 `0.8.3`、XTLS SDK `0.17.0`、rw-core `v26.7.28`
和 GeoCheck `v0.3.0` 保持不变。相关证据和继承的启动后 webhook 行为见
[3.4.2 基线](contract-3.4.2.md)。

## Lite 对齐

项目版本和报告给 Panel 的契约版本更新为 `3.4.3`，来源校验器与工作流固定到新官方提交，
并更新部署默认版本。Lite 使用 Go，不包含官方 Node.js 运行时。

另外，定时安全扫描发现 Go `crypto/x509` 证书验证器的 GO-2026-6809
（CVE-2026-111943）。Go 工具链和固定 SHA 的 Docker 构建镜像由 `1.26.9` 更新为
已包含修复的 `1.26.10`。单独打包的上游运行时二进制保持原有版本及构建工具链。

验证重点是官方实际差异、重新生成的来源证据、版本一致性、文档、安全扫描，以及必需的
CI 和不可变发布候选检查。运行时操作沿用 3.4.2 的行为及已有集成覆盖。
