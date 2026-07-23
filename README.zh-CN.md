<!-- translation: locale=zh-CN; source=README.md; source-sha256=2a51b5a4198c0792967943da93346ef116b2d1c53f804b92fbe42237938d35e9 -->
<div align="center">

# Remnanode Lite

**面向小型 Linux 服务器的 Remnawave Node Go 实现**

[English](README.md) | **简体中文** | [Русский](README.ru.md)

**英文 [README.md](README.md) 是权威版本，本页是中文说明。**

[![CI](https://github.com/luxiaba/remnanode-lite/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/luxiaba/remnanode-lite/actions/workflows/ci.yml)
[![Candidate](https://github.com/luxiaba/remnanode-lite/actions/workflows/container.yml/badge.svg?branch=main)](https://github.com/luxiaba/remnanode-lite/actions/workflows/container.yml)
[![Security](https://github.com/luxiaba/remnanode-lite/actions/workflows/security.yml/badge.svg)](https://github.com/luxiaba/remnanode-lite/actions/workflows/security.yml)
[![Go](https://img.shields.io/badge/Go-1.26.5-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)

[Docker 快速部署](#docker-快速部署) · [原生 Linux](#原生-linux) · [配置](docs/i18n/zh-CN/configuration.md) · [运维](docs/i18n/zh-CN/operations.md) · [完整文档](docs/i18n/zh-CN/README.md)

</div>

Remnanode Lite 是一个运行在 Linux 上的 Remnawave Node 实现。它接收 Remnawave Panel 下发的配置，管理 rw-core 进程、用户和插件规则，并上报系统与流量统计。Docker 镜像和已发布的 Native bundle 都包含该 Release 选定的 rw-core 及运行数据。

项目维护的部署配置面向**整机 512 MiB 内存、1 vCPU、2 GB 磁盘**的小型服务器，并提供 `linux/amd64` 和 `linux/arm64` 镜像。

> [!NOTE]
> Remnanode Lite 是独立维护的社区项目，与 Remnawave 官方没有隶属或背书关系。项目按照官方 Node 的公开行为保持兼容，代码则由我们独立开发和维护。

## 主要特点

- 实现 Remnawave Node `2.8.0` API 契约。
- 使用一个 Go 进程直接管理 rw-core，不依赖 Node.js 或 s6。
- 提供面向 512 MiB 服务器维护的低内存 Compose 配置。
- 支持用户热更新、统计、连接管理和官方插件规则格式。
- 提供 amd64/arm64 GHCR 镜像，并附带 SBOM、构建来源和证明。
- Native Linux 的安装、升级、回滚和修复由 `rnlctl` 统一处理。
- 只用一个 Compose 文件即可部署，不需要源码或持久化数据卷，`.env` 仍为可选项。

## 选择部署方式

| | Docker Compose | 原生 Linux |
| --- | --- | --- |
| 适用场景 | 已经具备 Docker Engine 与 Compose v2；这是默认方案。 | 无法安装 Docker，或机器不适合承担 Docker daemon 与容器运行时的常驻开销。 |
| 安装入口 | 下载 Release 附带的 Compose 文件，在 `.env` 或明确的内联 mapping 中填写 Panel Secret。 | 下载并校验一个精确 Release 的 `install.sh`，以 root 运行安装器。 |
| 更新与回滚 | 选择精确镜像 tag 或 digest，pull 后重建；切回原镜像引用即可回滚。 | 使用 `rnlctl upgrade --to VERSION` 和 `rnlctl rollback`；系统保留一个经过校验的 previous generation。 |
| 宿主服务 | 需要 Docker Engine daemon 及其容器运行时。 | 不需要 Docker Engine daemon 或容器运行时，但 `remnanode-lite` 仍会作为 systemd 或 OpenRC 后台服务运行。 |
| 版本选择 | 推荐精确 tag 或 manifest digest；`latest` 与 `preview` 是主动选择的移动通道。 | 只接受精确 `X.Y.Z` 或 `X.Y.Z-rnl.N` Release，不会解析移动镜像通道。 |

两种方式都使用 host networking 并需要 `NET_ADMIN`。不要与使用相同 Panel 或代理端口的其他 Node 同时运行。

## Docker 快速部署

开始前需要准备 Docker Engine 和 Compose v2，并在 Remnawave Panel 中创建好节点，拿到该节点的完整 Secret Key。节点端口必须能被 Panel 访问。下面的命令默认在 root shell 中执行，其他情况请按需使用 `sudo`。

先在 GitHub Releases 页面选择一个已经发布的版本，再从该精确 Release 下载 Compose 文件和环境变量模板。源码中的版本号和候选镜像都不是可下载的 Release：

```bash
mkdir -p /opt/remnanode-lite
cd /opt/remnanode-lite

VERSION="<published-version>" # 例如：X.Y.Z 或 X.Y.Z-rnl.N
BASE="https://github.com/luxiaba/remnanode-lite/releases/download/${VERSION}"

curl -fL \
  "${BASE}/docker-compose.single-file.yaml" \
  -o docker-compose.yaml
curl -fL \
  "${BASE}/remnanode-lite.env.example" \
  -o .env

chmod 600 docker-compose.yaml .env
```

Compose CLI 会自动读取同目录中的 `.env`。下载的两个文件都选择了该 Release 的精确镜像版本。在 `.env` 中填写 Panel 的节点端口和完整 Secret：

```env
NODE_PORT=38329
SECRET_KEY=PASTE_THE_COMPLETE_PANEL_SECRET_KEY
```

Compose 为 `NODE_PORT` 提供的回退值是 `2222`；`38329` 只是示例。无论选择哪个端口，都必须与 Panel 中该节点的端口一致。

已有部署可以继续使用原来的自定义目录，升级时不要求迁移目录。

如果希望保持真正的单文件部署而不创建 `.env`，可以在 `docker-compose.yaml` 中直接用完整值替换 `SECRET_KEY` 插值。下面的示例同时把端口回退值改为 `38329`：

```yaml
environment:
  NODE_PORT: "${NODE_PORT:-38329}"
  SECRET_KEY: "PASTE_THE_COMPLETE_PANEL_SECRET_KEY"
```

启动节点：

```bash
cd /opt/remnanode-lite
docker compose config --quiet
docker compose pull
docker compose up -d --no-build
docker compose ps
docker compose logs --tail=100 remnanode-lite
```

容器应先进入 healthy，随后节点应在 Panel 中恢复在线。最后再用真实代理流量确认部署结果；容器 healthy 本身并不能证明 Panel 连接和 rw-core 流量都正常。

从官方容器迁移时，原来的 `NODE_PORT` 和 `SECRET_KEY` 可以直接沿用；启动新容器前，请先停止旧容器。迁移、指定版本、digest 固定和回滚方法见 [Docker 部署指南](docs/i18n/zh-CN/deployment-docker.md)。

## 原生 Linux

当机器无法安装 Docker Engine，或不适合承担 Docker daemon 与容器运行时的开销时，使用 Native bundle。Native 并不表示没有后台服务：`remnanode-lite` 会直接由 systemd 或 OpenRC 运行。以 systemd 的 Rocky Linux 9 为主目标；Rocky Linux 8 和 Debian 12 兼容。OpenRC 为实验性路径，需要可用的 cgroup v2。

Native 安装永远不跟随移动通道。先在 GitHub Releases 页面选择一个已经发布的版本，再从同一个精确 Release 下载 `install.sh` 与 `SHA256SUMS`，校验安装器，并明确指定版本：

```bash
VERSION="<published-version>" # 例如：X.Y.Z 或 X.Y.Z-rnl.N
BASE="https://github.com/luxiaba/remnanode-lite/releases/download/${VERSION}"

curl -fLO "${BASE}/install.sh"
curl -fLO "${BASE}/SHA256SUMS"
grep '  install.sh$' SHA256SUMS | sha256sum --check --strict -

sudo sh ./install.sh --version "$VERSION" --port 38329
```

没有已安装的 Secret 时，安装器会安全地在终端中读取 Panel Secret。它会校验并安装一个完整 generation：Node、`rnlctl`、rw-core、GeoIP、GeoSite、ASN 数据和服务定义。启动后执行：

```bash
sudo rnlctl status --json
sudo rnlctl doctor
sudo rnlctl logs node --lines 100
```

Native bundle 与其对应 Release 使用相同的契约。批量上线前请阅读 [Native Linux 部署指南](docs/i18n/zh-CN/deployment-native.md)，其中包含前置依赖、无人值守与离线安装、精确版本升级、回滚、修复和卸载。

## Docker Compose 环境变量

绝大多数节点只需要设置 `NODE_PORT` 和 `SECRET_KEY`。受维护的 Compose 文件只插值以下 8 个变量：

| 变量 | `.env` 中必需 | Compose 回退值 | 用途 |
| --- | --- | --- | --- |
| `REMNANODE_IMAGE` | 否 | Release Compose 文件选择的精确镜像 | 镜像 tag 或 `name@sha256:...`；仅由 Compose 使用，不传入 Node。 |
| `NODE_PORT` | 否 | `2222` | Panel 连接节点的 HTTPS 端口，必须与 Panel 中的配置一致。 |
| `NODE_BIND_ADDR` | 否 | 空 | 只监听指定的本地地址；空值表示监听所有本地地址。 |
| `SECRET_KEY` | 是，除非直接写在 YAML 中 | 无；空值会使插值失败 | Panel 提供的完整 base64 或 base64url Secret。 |
| `LOW_MEMORY` | 否 | `1` | 启用小机器使用的低内存运行参数。 |
| `DISABLE_HASHED_SET_CHECK` | 否 | `false` | 仅用于调试，使每次 start 请求都重启 rw-core。 |
| `BODY_LIMIT_MB` | 否 | 空（自动） | 覆盖 Node 对外 API 的请求体上限；低内存模式会自动使用 16 MiB。 |
| `GOMEMLIMIT` | 否 | 空（自动） | 覆盖 Go 运行时内存软限制；低内存模式会自动使用 180 MiB。 |

插值优先级为 shell 环境变量 > `.env` > Compose 文件中的回退值。使用 `:-` 时，最终值未设置或为空会采用回退值。Compose 只把 `environment` 下明确列出的 7 个运行变量传入容器；`REMNANODE_IMAGE` 只供 Compose 使用，`.env` 中的未知键不会注入容器。

请使用上面展示的 YAML 映射写法。不要写成 `- SECRET_KEY="..."`：在这种列表写法中，引号会成为值的一部分，导致 Secret 无法解码。Compose 文件中含有 Secret，Docker 本地元数据也能看到环境变量，因此文件权限应保持为 `0600`。

所有运行参数、取值范围和优先级见 [配置参考](docs/i18n/zh-CN/configuration.md)。

## 常用操作

查看 Node 日志：

```bash
docker compose logs --tail=100 -f remnanode-lite
```

查看 rw-core 输出和错误日志：

```bash
docker exec -it remnanode-lite sh -c \
  'tail -n 50 -F "$LOG_DIR/xray.out.log" "$LOG_DIR/xray.err.log"'
```

查看当前运行版本：

```bash
docker exec remnanode-lite remnanode-lite version
```

如果要切换精确版本，先修改 `.env` 中的 `REMNANODE_IMAGE`。只有明确不使用
`.env` 时才直接修改 Compose 中的 `image:`。随后拉取镜像并重建容器：

```bash
docker compose pull
docker compose up -d --no-build --force-recreate
```

`latest` 只会在主动 pull 时检查新镜像，不会自行替换正在运行的容器。rw-core 日志位于 tmpfs，重建容器后会清空；Node 日志由 Docker 按 Compose 中的限制轮转。健康检查、故障排查、分批更新和回滚见 [运维指南](docs/i18n/zh-CN/operations.md)。

## 版本与镜像标签

| 标签 | 用途 |
| --- | --- |
| `X.Y.Z` | 与对应官方 Node 契约对齐的稳定版本，推荐用于生产和回滚。 |
| `X.Y.Z-rnl.N` | Remnanode Lite 自己的迭代版本，可用于提前开发或继续完善已有对齐版本。 |
| `latest` | 最近一次完整发布的稳定版本。它会移动，不能作为回滚依据。 |
| `preview` | 最近一次被提升的 `rnl.N` 预发布版本；不会推动 `latest`。 |
| `sha-<commit>` | 从某个 `main` 提交构建的不可变镜像，用于正式发布前验证候选。 |
| `edge` | 当前 `main` 的滚动镜像，只适合短期测试。 |

批量部署应使用同一个精确版本或 manifest digest，并保留上一个值用于回滚。完整规则见 [版本与镜像标签](docs/i18n/zh-CN/versioning.md)。

## 兼容性

| 项目 | 当前基线 |
| --- | --- |
| Native Linux bundle | 已发布的精确 Release |
| Node 契约 | `2.8.0` |
| rw-core | `v26.6.27` |
| 平台 | `linux/amd64`、`linux/arm64` |
| 整机目标 | `512 MiB RAM / 1 vCPU / 2 GB disk` |
| Compose 服务限制 | `448 MiB RAM`，不额外使用 swap |

该整机规格是设计目标。仓库维护的 Compose 配置会把容器严格限制为 `448 MiB / 1 CPU`，且不为容器提供额外 swap，为宿主机留出余量。

资源目标对应仓库维护的标准 Compose 配置，不表示任何流量和插件组合都一定适合相同规格。具体测量和边界见 [资源预算](docs/i18n/zh-CN/development/resource-budget.md)。

## 工作原理

```mermaid
flowchart LR
    Panel["Remnawave Panel"] -->|"mTLS + JWT"| Node["Remnanode Lite"]
    Node -->|"配置、用户、统计"| Core["rw-core"]
    Node --> Rules["nftables 与连接管理"]
    Core --> Traffic["代理流量"]
```

Node 负责管理 rw-core 进程及其运行状态，Xray 配置始终以 Panel 下发的内容为准。因此，重建容器不需要配置数据卷，Panel 会重新下发配置。包结构、生命周期和数据流见 [架构与运行设计](docs/i18n/zh-CN/architecture.md)。

## 文档

| 目标 | 从这里开始 |
| --- | --- |
| 部署或迁移节点 | [Docker Compose](docs/i18n/zh-CN/deployment-docker.md) · [原生 Linux](docs/i18n/zh-CN/deployment-native.md) |
| 配置与日常运维 | [配置](docs/i18n/zh-CN/configuration.md) · [运维](docs/i18n/zh-CN/operations.md) |
| 了解项目实现 | [项目范围](docs/i18n/zh-CN/project.md) · [架构](docs/i18n/zh-CN/architecture.md) |
| 参与开发 | [开发指南](docs/i18n/zh-CN/development/README.md) · [测试](docs/i18n/zh-CN/development/testing.md) · [贡献说明](docs/i18n/zh-CN/contributing.md) |
| 了解版本和发布 | [版本策略](docs/i18n/zh-CN/versioning.md) · [发布流程](docs/i18n/zh-CN/release.md) |
| 报告或检查安全问题 | [安全策略](docs/i18n/zh-CN/security.md) |

[中文文档索引](docs/i18n/zh-CN/README.md)包含更完整的使用和开发资料。

## 开发

普通单元测试不需要 Panel、Secret 或正在运行的 rw-core：

```bash
git switch dev
go mod download
go test -count=1 ./...
mkdir -p bin
go build -trimpath -o bin/remnanode-lite ./cmd/remnanode-lite
go build -trimpath -o bin/rnlctl ./cmd/rnlctl
./bin/remnanode-lite version
./bin/rnlctl version
```

Linux 网络集成、真实 rw-core 和 Panel 兼容属于不同的测试层。修改这些部分前，请先阅读 [开发指南](docs/i18n/zh-CN/development/README.md)。

## 安全

容器使用 host network 并持有 `NET_ADMIN`，因此能够修改宿主机的网络状态。只运行可信镜像，生产环境优先使用精确版本或 manifest digest。Compose 文件权限应保持为 `0600`，并限制对 Docker socket 和宿主机管理员账号的访问。

不要在公开 Issue 中发布 Secret、证书、真实节点信息或漏洞利用细节。私下报告方式见 [安全策略](docs/i18n/zh-CN/security.md)。

## 许可证

Remnanode Lite 使用 [AGPL-3.0-only](LICENSE) 许可证。
