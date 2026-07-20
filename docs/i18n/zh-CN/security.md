<!-- translation: locale=zh-CN; source=SECURITY.md; source-sha256=d575599959883257606ee7120312cf031c39d4d15f466eb1cee191f2b54edc50 -->

# 安全策略

> 这是中文译文；安全规则以[英文原文](../../../SECURITY.md)为准。

遇到漏洞时如何报告、哪些版本仍接收安全修复，以及节点运行需要哪些权限，都可以在本文找到。部署加固与实现细节另见[架构说明](architecture.md)和[运维文档](operations.md)。

## 报告漏洞

请使用 GitHub 的[私密漏洞报告](https://github.com/luxiaba/remnanode-lite/security/advisories/new)提交安全问题。不要在公开 Issue、Discussion、日志或截图中披露以下内容：

- `SECRET_KEY`、JWT、CA、节点证书或私钥；
- Panel URL、真实 IP、hostname 或可识别节点的信息；
- 未经脱敏的请求、响应、配置或运行日志；
- 在修复发布前可直接复现攻击的完整利用细节。

报告应尽量包含受影响版本或 commit、部署方式、影响范围、最小复现步骤和建议缓解方式。请使用虚构地址和脱敏材料复现；维护者需要更多信息时会在私密 advisory 中继续沟通。

如果 Secret 可能已经泄漏，应立即轮换。后续 commit 删除该值并不能把它从 Git 历史、日志、缓存、registry 或其它副本中移除。

## 支持范围

只有已经发布的正式版本才提供安全支持。`edge`、`sha-*` 与 `candidate-sha-*` 都是候选构建，不承诺长期维护。当前策略如下：

| 版本 | 安全修复策略 |
| --- | --- |
| `latest` 指向的稳定版本 | 接收安全修复 |
| 同一版本线的上一个稳定版本 | 在可合理回滚或升级的范围内处理高影响问题 |
| `edge`、历史候选与更早版本 | 不保证修复，请升级到受支持版本 |

确切支持范围会在对应 GitHub Security Advisory 和 Release note 中说明。

## 运行信任边界

Remnanode Lite 是具有网络管理权限的节点软件，不是普通无特权 Web 服务：

- Panel 到 Node 的公开接口要求最低 TLS 1.3、双向认证和 RS256 Bearer JWT。
- Docker 使用宿主网络；`NET_ADMIN` 允许管理本项目 nftables 表并通过 `NETLINK_SOCK_DIAG` 关闭连接，`NET_BIND_SERVICE` 允许监听低端口。
- 当前容器以 root UID 启动，但会丢弃其它 capability，启用 `no-new-privileges` 和只读 rootfs。host network 与 `NET_ADMIN` 仍然构成明确的宿主机信任边界。
- 只应运行来自本仓库、已验证的镜像。生产优先固定精确版本或 manifest digest，并验证 build attestation。
- Node 不持久化 Panel 下发的完整 Xray 配置；重启后由 Panel 重新同步。运行日志同样可以是临时数据。

受支持的单文件生产模板是 [`deploy/compose.single-file.yaml`](../../../deploy/compose.single-file.yaml)。除非经过评审的部署变更明确替代，否则应保留其中的 capability、只读文件系统、tmpfs、进程、内存、CPU、healthcheck 和日志轮转约束。

## Secret 处理

原生 systemd/OpenRC 部署推荐将 Secret 存放在 `/etc/remnanode/secret.key`，权限为 `root:remnanode 0640`。配置与 Secret 由 Go 进程使用有界、拒绝符号链接的文件读取路径加载，不会把整份 `node.env` 导出到服务环境。

单文件 Compose 必须将 Secret 内联，因此它会出现在 `docker inspect` 可读取的容器元数据中。应执行：

```bash
chmod 600 docker-compose.yaml
```

同时限制 Docker socket、备份、终端历史和主机管理员权限。Node 启动 rw-core 前会从继承环境中剥离 `SECRET_KEY`、`SECRET_KEY_FILE`、`INTERNAL_REST_TOKEN` 与 `REMNANODE_ENV`，并覆盖资源路径和内部 webhook token；该 token 默认每次启动随机生成，显式配置时使用经过 Go 配置解析的值。其它非受管环境变量仍会继承，因此不要向 Node 容器注入无关 Secret。

绝不能提交 `.env`、包含真实 Secret 的展开后 Compose、`/etc/remnanode/node.env`、`secret.key`、证书、私钥或原始验收采集包。

## 供应链

仓库当前采用以下控制：

- GitHub Actions 固定到完整 commit SHA；
- Go module 会被校验，定时的[安全 workflow](../../../.github/workflows/security.yml)会对可达 Go 代码执行 `govulncheck`；
- 基础镜像固定 manifest digest；
- rw-core、geo 与 ASN 来源固定版本/commit，并校验下载摘要；
- Release 镜像包含 SBOM、BuildKit provenance 和 GitHub build attestation；
- Release 二进制、Compose 和数据资产由 `SHA256SUMS` 覆盖。

这些措施还不能保证构建结果逐字节可复现：Dockerfile 安装的 Debian 软件包尚未固定到 snapshot 和精确版本。核验正式镜像时，应同时检查 registry manifest digest、SBOM、provenance 和 attestation，不能只看 tag。

仓库 CI 以 [`.github/workflows/ci.yml`](../../../.github/workflows/ci.yml) 为准，面向使用者的发布变化记录在根目录英文 [`CHANGELOG.md`](../../../CHANGELOG.md)。`docs/archive/` 中的历史记录（例如[2026 审计整改记录](archive/2026-07-audit-remediation.md)）只保存历史语境，不代表当前安全状态或发布状态。

## 安全设计原则

- 所有外部输入必须在副作用前完成认证、解压边界、JSON 解码和契约校验。
- 进程、队列、请求体、并发、外部命令输出和关闭时间必须有界。
- rw-core、插件快照和 nftables 状态各有唯一所有者；失败不能提前提交本地成功状态。
- Node 只管理本项目的 rw-core 进程、内部 socket 和固定 nftables 表，不接管宿主机的整体防火墙策略。
- 连接销毁会扫描宿主 network namespace 中的 TCP 连接；只要连接的本地或远端地址等于目标 IP，就可能被关闭，而且不按 PID 区分，因此生产节点应专门承担这项工作。Panel 请求会过滤本地和特殊地址；管理员命令 `remnanode-lite kill-sockets` 直接调用内核适配器，不经过这层应用过滤。
- 发布验收材料不得包含可还原用户或生产环境的数据。

## 安全相关变更

安全敏感变更必须遵循[贡献指南](contributing.md)，增加回归覆盖并运行与边界匹配的检查。完整本地仓库门禁为：

```bash
REMNANODE_OFFICIAL_SOURCE=/absolute/path/to/pinned-official-source \
REQUIRE_GOVULNCHECK=1 \
  bash scripts/check.sh
```

该门禁通过只证明仓库级检查，不替代真实 Linux namespace、候选 attestation、Panel 集成，也不替代对应版本发布 profile 要求的运行时与扩展验收。
