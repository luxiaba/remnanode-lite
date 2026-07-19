# Docker Compose 部署

[返回文档首页](README.md)

Docker Compose 是小内存节点的首选部署方式。服务器只需要一个权限受限的 YAML 文件和 Docker Engine，不需要源码、Go 工具链或持久日志卷。

本页以大量独立小节点常用的“单文件 Compose”作为主流程；仓库根目录的 `compose.yaml + .env` 仍作为集中管理或本地构建的可选方式。

## 部署模型

容器内只有一个主进程：`remnanode-lite` 直接启动和回收 rw-core，不使用 s6 或第二个常驻 supervisor。两者共享同一个容器 cgroup：

- `448 MiB` memory hard limit，禁用额外 swap；
- `1 CPU`、`256 PIDs`；
- 只读 rootfs；
- `/run/remnanode`、`/tmp`、`/var/log/remnanode` 使用总上限 `48 MiB` 的 tmpfs；
- Docker Node 日志采用 `2 MiB x 2` 的 `json-file` 轮转；
- 不挂载持久数据卷，重建容器会清空配置副本和日志，由 Panel 重新下发 Xray 配置。

这些限制为整机 `512 MiB RAM / 1 vCPU / 2 GB disk` 目标预留宿主机空间，但不是对任意流量和插件组合的 SLA。资源证据与边界见[资源预算](development/resource-budget.md)。

## 选择镜像

镜像位于公开 GHCR，可匿名拉取：

```text
ghcr.io/luxiaba/remnanode-lite
```

| 标签 | 行为 | 使用建议 |
| --- | --- | --- |
| `X.Y.Z-rnl.N` | 本项目经过发布验证的独立迭代 | 推荐生产使用，便于准确回滚 |
| `X.Y.Z` | 已完成对应官方版本对齐的正式构建 | 推荐生产使用 |
| `latest` | 最新一个经过发布验证的稳定构建 | 适合主动跟随稳定版，不适合作为回滚标识 |
| `sha-<40位commit>` | `main` 提交对应的候选构建 | 真实服务器验收 |
| `edge` | 当前 `main` 的浮动候选 | 仅临时观察 |

精确版本和 `sha-*` 按项目政策不主动移动，但 registry tag 在技术上不是不可变对象。需要最强固定时使用 `name@sha256:...` manifest digest。首个正式 Release 发布前，`latest` 和版本标签尚不存在，应从 [GHCR Package](https://github.com/luxiaba/remnanode-lite/pkgs/container/remnanode-lite) 选择候选 `sha-*`。

版本命名与晋升规则见[版本模型](versioning.md)。

## 前置条件

- Linux `amd64` 或 `arm64`；
- Docker Engine 与 Compose v2，即 `docker compose`；
- 已在 Panel 创建节点并取得完整 `SECRET_KEY`；
- Panel 中的 Node 端口与 `NODE_PORT` 一致；
- 宿主机防火墙允许 Panel 访问 Node API 端口，并按实际代理配置开放入站端口。

Compose 使用 `network_mode: host`，不要添加 `ports:`。容器持有 `NET_ADMIN`，可以修改宿主网络命名空间中的项目私有 nftables 表并关闭连接；只运行受信任的镜像。

## 单文件部署

先按当前发布阶段选择入口。首个正式 Release 尚未发布，或正在做候选验收时，必须把
部署文件和 `sha-*` 镜像绑定到同一个完整 commit；正式版本发布后则优先使用 Release
附带且经 `SHA256SUMS` 覆盖的 Compose 资产。

### 首发前或候选验收

```bash
(
  set -euo pipefail
  candidate_commit=REPLACE_WITH_40_CHAR_COMMIT
  printf '%s\n' "$candidate_commit" | grep -Eq '^[0-9a-f]{40}$'

  mkdir -p /opt/remnanode
  cd /opt/remnanode
  curl -fL \
    "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${candidate_commit}/docs/examples/compose.single-file.yaml" \
    -o docker-compose.yaml
  sed -i \
    "s|ghcr.io/luxiaba/remnanode-lite:latest|ghcr.io/luxiaba/remnanode-lite:sha-${candidate_commit}|" \
    docker-compose.yaml
  chmod 600 docker-compose.yaml
)
```

从 [GHCR Package](https://github.com/luxiaba/remnanode-lite/pkgs/container/remnanode-lite) 选择真实存在的完整 40 位 `sha-*` 标签，把其中 commit 填入变量。占位符未替换时校验会在下载前失败。这样 Compose 内容和镜像始终来自同一个提交；验收完成前不要自行重标记为正式版本。

### 正式版本

从同一个 GitHub Release 下载单文件资产和摘要：

```bash
VERSION=X.Y.Z-rnl.N # 或 X.Y.Z
BASE_URL="https://github.com/luxiaba/remnanode-lite/releases/download/v${VERSION}"

mkdir -p /opt/remnanode
cd /opt/remnanode
curl -fL "${BASE_URL}/docker-compose.single-file.yaml" -o docker-compose.yaml
curl -fLO "${BASE_URL}/SHA256SUMS"
grep -F ' docker-compose.single-file.yaml' SHA256SUMS \
  | sed 's|docker-compose.single-file.yaml|docker-compose.yaml|' \
  | sha256sum --check --strict
chmod 600 docker-compose.yaml
```

macOS 的 `shasum` 命令不是生产 Linux 部署路径；服务器示例以 GNU `sha256sum` 为准。

Release workflow 会把该资产中的 `image:` 固定为对应的精确版本，而不是 `latest`。下载后只需要填写节点端口和 Secret；希望主动跟随稳定通道时再显式改成 `latest`。

编辑以下字段：

```yaml
image: ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N

environment:
  NODE_PORT: "38329"
  SECRET_KEY: "粘贴 Panel 提供的完整 base64 内容"
  LOW_MEMORY: "1"
```

示例版本只用于展示格式，请替换为 GHCR 中真实存在的精确版本、`sha-*` 或 digest。

### Secret 写法

环境变量必须使用 YAML mapping：

```yaml
environment:
  SECRET_KEY: "eyJ..."
```

不要写成下面的列表形式：

```yaml
environment:
  - SECRET_KEY="eyJ..."
```

列表形式中的引号会成为变量值的一部分，通常导致：

```text
decode SECRET_KEY: illegal base64 data at input byte 0
```

单文件部署会让 Secret 出现在 Compose 文件和本机 `docker inspect` 元数据中，因此必须保持文件权限为 `0600`，限制 Docker socket、备份和主机管理员权限。Node 启动 rw-core 时会剥离 Panel Secret，不把它继续传给子进程。

## 启动与首次核验

```bash
cd /opt/remnanode
docker compose config --quiet
docker compose pull
docker compose up -d --no-build
docker compose ps
docker compose logs --tail=100 remnanode
ss -H -lnt "sport = :38329"
```

不要在自动化日志中运行不带 `--quiet` 的 `docker compose config`，它会展开内联 Secret。

容器变为 `healthy` 只证明 PID 1 存活且内部配置 Unix socket 已创建；它不证明：

- Panel 能通过网络访问节点；
- mTLS、JWT 或 Secret 正确；
- rw-core 已经在线；
- Panel 下发的代理入站端口可以访问。

Node 重启后 rw-core 初始离线是正常行为。Node 不从磁盘恢复旧 Panel 配置，Panel 后续健康轮询会重新调用 `/node/xray/start`。最终应在 Panel 确认节点在线，并检查实际代理功能。

## 从官方容器迁移

官方 `remnawave/node` 使用的 `NODE_PORT` 和完整 `SECRET_KEY` 可以继续使用；它们属于 Panel 与 Node 的外部契约，不依赖官方容器的 Node.js/s6 内部结构。迁移时不要让两个容器同时运行，因为 host network 下会争用 Node API 和代理入站端口。

1. 备份原 Compose，并记录原镜像精确版本，作为回滚目标。
2. 用本页的完整单文件样本替换服务定义；至少保留 host network、两个 capability、资源限制、只读 rootfs、tmpfs 和日志限制。
3. 沿用原 `NODE_PORT` 和 Secret，但把 `environment` 改为 YAML mapping，镜像固定为真实项目版本、`sha-*` 或 digest。
4. 拉取并强制重建同名容器；Compose 会停止旧容器后再创建新容器。

```bash
cd /opt/remnanode
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode
```

5. 在 Panel 确认节点重新在线、rw-core 已启动并抽查真实代理流量。新实现的 rw-core 日志路径是 `/var/log/remnanode/xray.out.log` 和 `xray.err.log`，不是官方容器的 `/var/log/xray/current`。

不需要迁移容器内运行状态或 Xray 配置卷：Panel 会重新下发配置。回滚时恢复备份 Compose 和原官方精确镜像，再执行同样的 pull/recreate；不要删除备份，直到新容器完成观察期。

## 候选镜像的自动化语义

合入 `main` 且命中容器构建输入时，`container` workflow 先构建多架构 manifest 并生成 build attestation，成功后才发布按策略不移动的 `sha-<commit>`；只有该提交仍是当前 `main` HEAD 时才移动 `edge`。候选镜像没有 GitHub Release 资产，也不代表正式发布。部署命令见前文“首发前或候选验收”。

## 固定 digest 与验证证明

拉取镜像后记录 registry digest：

```bash
VERSION=X.Y.Z-rnl.N # 或 X.Y.Z
IMAGE="ghcr.io/luxiaba/remnanode-lite:${VERSION}"

DIGEST_REF="$(docker image inspect \
  --format '{{range .RepoDigests}}{{println .}}{{end}}' \
  "$IMAGE" | head -n 1)"
printf '%s\n' "$DIGEST_REF" \
  | grep -Eq '^ghcr\.io/luxiaba/remnanode-lite@sha256:[0-9a-f]{64}$'
```

将 Compose 中的镜像改成输出的完整引用：

```yaml
image: ghcr.io/luxiaba/remnanode-lite@sha256:...
```

安装 GitHub CLI 后可以验证本仓库生成的证明：

```bash
gh attestation verify \
  "oci://${DIGEST_REF}" \
  --repo luxiaba/remnanode-lite \
  --signer-workflow luxiaba/remnanode-lite/.github/workflows/container.yml
```

tag 说明“希望引用哪个版本”，digest 说明“实际运行哪一份字节”。受控批量部署应保存后者。

## 更新与回滚

先备份当前 YAML，修改 `image:` 后主动拉取并重建：

```bash
cp -p docker-compose.yaml docker-compose.yaml.previous
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode
```

回滚时恢复上一个经过验证的 YAML，或把 `image:` 改回上一个精确版本/digest，再执行同样的 `pull` 与 `up`。不要通过覆盖旧版本标签实现回滚。

`latest` 不会自动替换正在运行的容器。使用它仍然需要定期、主动执行上述更新命令，并在更新前记录旧 digest。

## `.env` 可选模式

希望把非敏感 Compose 结构和节点参数分离时，可以使用仓库根目录的 `compose.yaml`：

```bash
curl -fLO https://raw.githubusercontent.com/luxiaba/remnanode-lite/main/compose.yaml
curl -fL https://raw.githubusercontent.com/luxiaba/remnanode-lite/main/.env.example -o .env
chmod 600 .env
```

至少设置：

```env
REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N
NODE_PORT=38329
SECRET_KEY=粘贴完整内容
LOW_MEMORY=1
```

同样应将示例版本替换为真实 tag 或 digest。完整变量说明见[配置参考](configuration.md)。

## 本地源码构建

源码构建只适合开发、审计或 registry 暂不可用的应急场景：

```bash
git clone https://github.com/luxiaba/remnanode-lite.git
cd remnanode-lite
cp .env.example .env
chmod 600 .env
# 编辑 .env

docker compose -f compose.yaml -f compose.build.yaml build --pull
docker compose -f compose.yaml -f compose.build.yaml up -d --no-build
```

不要在磁盘仅 2 GB 的生产节点上构建。Go 工具链、基础层和 BuildKit cache 可能显著超过运行时磁盘预算。

## 日志与磁盘

Node 主进程日志：

```bash
docker compose logs -f remnanode
```

rw-core 实时日志：

```bash
docker exec -it remnanode tail -n 50 -F /var/log/remnanode/xray.out.log
docker exec -it remnanode tail -n 50 -F /var/log/remnanode/xray.err.log
```

两条 rw-core 日志各以 `4 MiB` 为轮转阈值并保留一个 `.1` 文件，存放在 `28 MiB` tmpfs；容器重建后清空。Node 的 Docker 日志由 `json-file` 限制为约 `2 MiB x 2`。本项目不要求持久日志，长期监控应由宿主机在自身磁盘预算内完成。

检查和清理无用镜像：

```bash
docker system df
docker image ls ghcr.io/luxiaba/remnanode-lite
docker image prune
```

确认回滚窗口已经结束后再清理旧镜像。更多日常命令和故障定位见[运维与排障](operations.md)。

## 镜像内容与可追溯性

当前构建包含：

- 静态链接的 `remnanode-lite`；
- 固定版本和资产摘要的 rw-core `v26.6.27`；
- 对应的 `geoip.dat`、`geosite.dat`；
- 从固定 `ipverse/as-ip-blocks` commit 构建的 compact ASN 数据库；
- Debian bookworm slim 运行环境、CA 证书和 nftables 依赖。

基础镜像、rw-core 和 ASN 来源固定了 digest 或摘要，但 Debian `apt` 软件包当前未固定到 snapshot 和具体包版本，因此不能宣称字节级完全可复现。每个正式产物应通过 manifest digest、SBOM、provenance 和 attestation 共同识别。
