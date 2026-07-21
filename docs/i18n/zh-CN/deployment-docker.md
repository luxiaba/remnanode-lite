<!-- translation: locale=zh-CN; source=docs/deployment-docker.md; source-sha256=15764c8cf1504cbf0e0a0165ebf7fe101c538554c2593c76a5fbb11449e6cbe0 -->

# Docker Compose 部署

> 这是中文译文；涉及部署规则时，请以[英文原文](../../deployment-docker.md)为准。

[返回文档首页](README.md)

Docker Compose 是小内存节点的首选部署方式。服务器只需要一个权限受限的 YAML 文件和 Docker Engine，不需要源码、Go 工具链或持久日志卷。

本页以大量独立小节点常用的“单文件 Compose”作为主流程。两种受支持模板都可以从同目录 `.env` 读取部署值；仓库根目录的 `compose.yaml` 仍作为集中管理或本地构建的可选方式。

## 部署模型

容器内只有一个主进程：`remnanode-lite` 直接启动和回收 rw-core，不使用 s6 或第二个常驻 supervisor。两者共享同一个容器 cgroup：

- `448 MiB` memory hard limit，禁用额外 swap；
- `1 CPU`、`256 PIDs`；
- 只读 rootfs；
- `/run/remnanode`、`/tmp`、`/var/log/remnanode` 使用总上限 `48 MiB` 的 tmpfs；
- Docker Node 日志采用 `2 MiB x 2` 的 `json-file` 轮转；
- 不挂载持久数据卷，重建容器会清空配置副本和日志，由 Panel 重新下发 Xray 配置。

这些是严格的容器 cgroup 限制，即使 Docker 宿主机更大也应保持。`448 MiB` 内存上限与 `448 MiB` 内存加 swap 合计上限相等，因此容器没有额外 swap 配额。整机 `512 MiB RAM / 1 vCPU / 2 GB disk` 是设计目标，不代表任意流量和插件组合都适合相同规格。实测数据和边界见[资源预算](development/resource-budget.md)。

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
| `sha-<40位commit>` | 从一个 `main` 提交构建的不可变候选 | 验证发布候选，必要时解析并固定 digest |
| `edge` | 当前 `main` 的浮动候选 | 仅临时观察 |

精确版本与 `sha-*` 按项目政策不主动移动，但 registry tag 在技术上不是不可变对象。需要最强固定时使用 `name@sha256:...` manifest digest。首个正式 Release 发布前，`latest` 和版本标签尚不存在，应从 [GHCR Package](https://github.com/luxiaba/remnanode-lite/pkgs/container/remnanode-lite) 选择真实候选并解析其 manifest digest。

版本命名与晋升规则见[版本模型](versioning.md)。

## 前置条件

- Linux `amd64` 或 `arm64`；
- Docker Engine 与 Compose v2，即 `docker compose`；
- 已在 Panel 创建节点并取得完整 `SECRET_KEY`；
- Panel 中的 Node 端口与 `NODE_PORT` 一致；
- 宿主机防火墙允许 Panel 访问 Node API 端口，并按实际代理配置开放入站端口。

Compose 使用 `network_mode: host`，不要添加 `ports:`。容器持有 `NET_ADMIN`，可以修改宿主网络命名空间中的项目私有 nftables 表并关闭连接；只运行受信任的镜像。

## 单文件部署

生产部署应使用与镜像来自同一个 Release 的 Compose 文件。这个文件包含在该 Release 的 `SHA256SUMS` 中，并且已经指向精确版本。

从同一个 GitHub Release 下载单文件资产和摘要：

```bash
VERSION=X.Y.Z-rnl.N # 或 X.Y.Z
BASE_URL="https://github.com/luxiaba/remnanode-lite/releases/download/v${VERSION}"

mkdir -p /opt/remnanode-lite
cd /opt/remnanode-lite
curl -fL "${BASE_URL}/docker-compose.single-file.yaml" -o docker-compose.yaml
curl -fLO "${BASE_URL}/SHA256SUMS"
grep -F ' docker-compose.single-file.yaml' SHA256SUMS \
  | sed 's|docker-compose.single-file.yaml|docker-compose.yaml|' \
  | sha256sum --check --strict
chmod 600 docker-compose.yaml
```

示例使用受支持 Linux 主机自带的 GNU `sha256sum`。校验完成后，在 `docker-compose.yaml` 同目录创建 `.env`，设置镜像、Node 端口和 Secret：

```env
REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N
NODE_PORT=38329
SECRET_KEY=粘贴_Panel_提供的完整_base64_内容
LOW_MEMORY=1
```

```bash
chmod 600 .env
```

从该目录运行命令时，Compose 会自动读取 `.env` 做变量插值；同名的已导出 shell 变量优先级更高。只有模板 `environment` mapping 中显式声明的值才会注入容器，`.env` 中的无关键不会进入容器。具有安全默认值的变量可以省略。示例版本只用于展示格式，请替换为 GHCR 中真实存在的精确版本、`sha-*` 或 digest。只有确定要跟随稳定通道时，才使用 `latest`。

### 测试候选版本

首个正式版本发布前，或需要测试新候选时，从镜像对应的同一 commit 下载 Compose 模板：

```bash
(
  set -euo pipefail
  candidate_tag=sha-REPLACE_WITH_FULL_MAIN_COMMIT
  case "$candidate_tag" in
    sha-*) candidate_commit="${candidate_tag#sha-}" ;;
    *) echo "candidate tag must be sha-<commit>" >&2; exit 1 ;;
  esac
  printf '%s\n' "$candidate_commit" | grep -Eq '^[0-9a-f]{40}$'

  mkdir -p /opt/remnanode-lite
  cd /opt/remnanode-lite
  curl -fL \
    "https://raw.githubusercontent.com/luxiaba/remnanode-lite/${candidate_commit}/deploy/compose.single-file.yaml" \
    -o docker-compose.yaml
  cat >.env <<EOF
REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite:${candidate_tag}
NODE_PORT=38329
SECRET_KEY=粘贴_Panel_提供的完整_base64_内容
LOW_MEMORY=1
EOF
  chmod 600 docker-compose.yaml .env
)
```

从 [GHCR Package](https://github.com/luxiaba/remnanode-lite/pkgs/container/remnanode-lite) 选择完整的 `sha-<40位main commit>` 标签，启动前替换端口和 Secret 占位值。需要内容寻址固定时，把 tag 解析为 manifest digest，并在 `.env` 中设置 `REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite@sha256:<manifest-digest>`。候选镜像只是测试构建，不能当作正式版本发布。

### 环境变量与 Secret 写法

单文件和仓库根目录模板都在 YAML mapping 中进行变量插值：

```yaml
environment:
  NODE_PORT: "${NODE_PORT:-38329}"
  SECRET_KEY: "${SECRET_KEY:?set SECRET_KEY in .env or the shell}"
```

`.env` 是插值来源，并不表示把整个文件复制进容器。只有 mapping 中显式列出的键会传入容器。shell 优先于 `.env`；如果修改后没有生效，应检查并清除遗留的同名导出变量。

不要写成下面的列表形式：

```yaml
environment:
  - SECRET_KEY="eyJ..."
```

列表形式中的引号会成为变量值的一部分，通常导致：

```text
decode SECRET_KEY: illegal base64 data at input byte 0
```

插值完成后，无论 Secret 来自 `.env`、shell 还是内联 mapping，有效值都会出现在本机 `docker inspect` 元数据中。所有包含 Secret 的文件都必须保持 `0600` 权限，并限制 Docker socket、备份和主机管理员权限。Node 启动 rw-core 时会剥离 Panel Secret，不把它继续传给子进程。

## 启动与首次核验

```bash
cd /opt/remnanode-lite
docker compose config --quiet
docker compose pull
docker compose up -d --no-build
docker compose ps
docker compose logs --tail=100 remnanode-lite
ss -H -lnt "sport = :38329"
```

不要在自动化日志中运行不带 `--quiet` 的 `docker compose config`，它会展开并打印实际生效的 Secret。

容器显示 `healthy`，表示 Node 已经接受内部 Unix socket 连接。仍需检查 Panel 和真实流量，因为该健康检查不覆盖：

- Panel 能通过网络访问节点；
- mTLS、JWT 或 Secret 正确；
- rw-core 已经在线；
- Panel 下发的代理入站端口可以访问。

Node 重启后 rw-core 初始离线是正常行为。Node 不从磁盘恢复旧 Panel 配置，Panel 后续健康轮询会重新调用 `/node/xray/start`。最终应在 Panel 确认节点在线，并检查实际代理功能。

## 从官方或旧版容器迁移

官方 `remnawave/node` 使用的 `NODE_PORT` 和完整 `SECRET_KEY` 可以继续使用；它们属于 Panel 与 Node 的外部契约，不依赖官方容器的 Node.js/s6 内部结构。旧版 Remnanode Lite 模板曾把 service 和容器命名为 `remnanode`，也使用相同步骤迁移。绝不能让 `remnanode` 与 `remnanode-lite` 同时运行，因为 host network 下会争用 Node API 和代理入站端口。

新安装示例使用 `/opt/remnanode-lite`，以便和官方部署目录区分。已有自定义目录不需要迁移；应始终在实际存放该部署 Compose 文件及可选 `.env` 的目录中执行命令。

1. 备份原 Compose，并记录原镜像精确版本，作为回滚目标。
2. 在旧 Compose 定义仍可用时先执行 `docker compose down`。然后用本页完整单文件模板替换服务定义，并保留 host network、两个 capability、资源限制、只读 rootfs、tmpfs 和日志限制。
3. 通过显式 `environment` mapping 沿用原 `NODE_PORT` 和 Secret，镜像固定为真实项目版本、`sha-*` 或 digest。
4. 对新 Compose project 执行 `down --remove-orphans`。如果名为 `remnanode` 的容器因为属于另一个 Compose project 而仍然存在，应先检查并确认它确实是旧 Node，再显式删除，之后才能启动替代容器。

```bash
cd /opt/remnanode-lite
docker compose down --remove-orphans
docker container inspect remnanode \
  --format 'name={{.Name}} image={{.Config.Image}}' 2>/dev/null || true
```

如果检查输出了容器，必须人工核对名称和镜像，确认它是旧 Node。只有核对完成后，才单独执行删除命令：

```bash
docker rm -f remnanode
```

确认 `remnanode` 已不存在后，再启动并核验替代容器：

```bash
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode-lite
```

5. 在 Panel 确认节点重新在线、rw-core 已启动并抽查真实代理流量。新实现的 rw-core 日志路径是 `/var/log/remnanode/xray.out.log` 和 `xray.err.log`，不是官方容器的 `/var/log/xray/current`。

不需要迁移容器内运行状态或 Xray 配置卷：Panel 会重新下发配置。回滚时先停止 `remnanode-lite`，再恢复备份 Compose 和原官方精确镜像；绝不能让两个容器名同时运行。新容器完成观察期前不要删除备份。

## 候选镜像

对于 `main` 的每个提交，`container` workflow 都会构建 `linux/amd64` 和 `linux/arm64` 镜像、发布多架构 manifest，并记录构建来源。全部成功后，它会发布 `sha-<commit>`；如果该提交仍是 `main` 最新提交，还会更新 `edge`。这些检查说明镜像如何构建，但不能证明它运行正常。

正式发布前，维护者应使用这个精确候选或其 manifest digest，在仓库维护的 Compose 限制下确认容器正常启动并保持健康、连接真实 Panel、启动 rw-core 且承载真实代理流量，没有意外 OOM、重启或生命周期异常。这是 tag 前的人工发布判断，宿主清单、容器标识、时间戳、日志和 smoke JSON 等运行数据不写入仓库。

候选镜像没有 GitHub Release 资产，不是正式版本。构建 attestation 用于验证构建来源，不能替代实际运行确认。

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
  --cert-identity https://github.com/luxiaba/remnanode-lite/.github/workflows/container.yml@refs/heads/main \
  --deny-self-hosted-runners
```

tag 说明“希望引用哪个版本”，digest 说明“实际运行哪一份字节”。受控批量部署应保存后者。

## 更新与回滚

先备份当前 Compose 输入。在使用 `.env` 时修改 `REMNANODE_IMAGE`；只有明确内联时才修改 `image:`，然后主动拉取并重建：

```bash
cp -p docker-compose.yaml docker-compose.yaml.previous
[ ! -f .env ] || cp -p .env .env.previous
docker compose config --quiet
docker compose pull
docker compose up -d --no-build --force-recreate
docker compose ps
docker compose logs --tail=100 remnanode-lite
```

回滚时恢复上一个经过验证的 Compose 文件和 `.env`，或把当前实际生效的镜像设置改回上一个精确版本/digest，再执行同样的 `pull` 与 `up`。不要通过覆盖旧版本标签实现回滚。

`latest` 不会自动替换正在运行的容器。使用它仍然需要定期、主动执行上述更新命令，并在更新前记录旧 digest。

## 批量上线

批量上线全程使用同一个已验证的 manifest digest，并保留上一 digest 以便回滚。精确版本 tag 更容易阅读，但部署记录仍应保存 `name@sha256:...`。不要把 `latest` 或 `edge` 直接推送到全部节点。

1. 按架构、发行版、地区和主要流量类型划分节点组，并为每台机器记录当前 digest、目标 digest 和回滚 Compose。
2. 先更新一小组能代表主要网络和架构的 canary。至少跨过一个业务高峰，检查 Panel 连接、rw-core 同步、真实代理流量、内存、重启、进程、磁盘和日志。
3. 依次扩大到约 `5%`、`25%`、`50%`，最后完成余量。每一阶段都必须结束观察后再继续；单个批次不要大到无法在同一维护窗口内恢复上一 digest。
4. 每个阶段抽查容器健康、Panel 状态、代理流量、restart/OOM 计数、内存、PID、磁盘和 Xray/nft 错误，并记录每个节点实际使用的 digest。
5. 如果出现无法解释的节点离线、代理失败、反复 Xray 启动失败、OOM、异常重启、进程卡住、资源越界或同类错误集中增长，立即停止扩批。先回滚该批次，再保留日志和 digest 对应关系用于排查。

回滚不依赖 Registry 移动 tag：恢复每台节点已记录的上一 Compose/digest，执行 `pull` 与 `up --force-recreate`，并重新确认 Panel 与真实流量。问题没有形成明确结论前，不要继续更新尚未触及的节点，也不要清理 canary 上的上一镜像。

发布前验证不能代替分阶段上线，生产扩批仍需逐阶段观察。

## 仓库根目录 Compose 与 `.env`

两种受支持模板都会从同目录 `.env` 自动插值其中显式声明的变量，因此上面的单文件流程无需修改模板即可使用 `.env`。如需使用仓库根目录模式，应从同一个正式 GitHub Release 下载 `compose.yaml`、环境模板和摘要，不能把未来 `main` 的 Compose 与旧镜像版本混用：

```bash
VERSION=X.Y.Z-rnl.N # 或 X.Y.Z
BASE_URL="https://github.com/luxiaba/remnanode-lite/releases/download/v${VERSION}"

curl -fLO "${BASE_URL}/compose.yaml"
curl -fLO "${BASE_URL}/remnanode.env.example"
curl -fLO "${BASE_URL}/SHA256SUMS"
grep -E ' (compose.yaml|remnanode.env.example)$' SHA256SUMS \
  | sha256sum --check --strict
mv remnanode.env.example .env
chmod 600 .env
```

至少设置：

```env
REMNANODE_IMAGE=ghcr.io/luxiaba/remnanode-lite:X.Y.Z-rnl.N
NODE_PORT=38329
SECRET_KEY=粘贴完整内容
LOW_MEMORY=1
```

`REMNANODE_IMAGE` 应保持为该 Release 的精确版本，或改为已经验证的 manifest digest。从当前目录调用 Compose 时会自动读取 `.env`，同名 shell 变量优先，只有 Compose `environment` mapping 中列出的值会进入容器。完整变量说明见[配置参考](configuration.md)。

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
docker compose logs -f remnanode-lite
```

rw-core 实时日志：

```bash
docker exec -it remnanode-lite tail -n 50 -F /var/log/remnanode/xray.out.log
docker exec -it remnanode-lite tail -n 50 -F /var/log/remnanode/xray.err.log
```

两条 rw-core 日志各以 `4 MiB` 为轮转阈值并保留一个 `.1` 文件，存放在 `28 MiB` tmpfs；容器重建后清空。Node 的 Docker 日志由 `json-file` 限制为约 `2 MiB x 2`。本项目不要求持久日志，长期监控应由宿主机在自身磁盘预算内完成。

检查和清理无用镜像：

```bash
docker system df
docker image ls ghcr.io/luxiaba/remnanode-lite
docker image prune
```

清理前先记录一个已验证的旧版本 tag 或 manifest digest，并确认对应镜像仍在本机；始终至少保留这一个明确的回滚镜像。`docker image prune` 默认只删除 dangling image，不要使用会把唯一回滚版本一并删除的批量清理参数。更多日常命令和故障定位见[运维与排障](operations.md)。

## 镜像内容与可追溯性

当前构建包含：

- 静态链接的 `remnanode-lite`；
- 固定版本和资产摘要的 rw-core `v26.6.27`；
- 对应的 `geoip.dat`、`geosite.dat`；
- 从固定 `ipverse/as-ip-blocks` commit 构建的 compact ASN 数据库；
- Debian bookworm slim 运行环境、CA 证书和 nftables 依赖。

基础镜像、rw-core 和 ASN 来源都固定了 digest 或摘要。Debian `apt` 软件包没有固定到 package snapshot，因此两次构建不保证字节级完全相同。识别正式产物时，应同时保留 manifest digest、SBOM、provenance 和 attestation。
