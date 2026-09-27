<div align="center">

![new-api](/web/public/logo.png)

# New API ZTX

**连接模型、应用与 Agent 的自托管 AI 网关**

<p align="center">
  <a href="https://github.com/ZTXBOSS666/new-api-ztx">项目主页</a> |
  <a href="https://github.com/ZTXBOSS666/new-api-ztx/releases">发布版本</a> |
  <a href="https://github.com/ZTXBOSS666/new-api-ztx/issues">问题反馈</a>
</p>

</div>

---

## 项目简介

这是基于 [QuantumNous/new-api](https://github.com/QuantumNous/new-api) 二次开发的中文版本，仓库地址为：

<https://github.com/ZTXBOSS666/new-api-ztx>

项目面向合法授权的 AI API 网关、组织内部鉴权、多模型管理、用量统计、成本核算和私有化部署场景。使用者需要自行取得上游 API Key、模型服务和接口权限，并遵守上游服务条款及适用法律法规。

## 二改功能

### 每日抽奖

- 侧边栏提供独立的「每日抽奖」入口。
- 用户页面：`/lottery`。
- 管理页面：`/lottery-admin`。
- 管理员可以配置每日参与人数上限、每日中奖人数、每位中奖奖金和每次报名费用。
- 所有金额均使用原生额度点数；报名费用在创建报名记录的同一数据库事务中扣除。
- 报名失败会整体回滚，不会产生扣费；费用填写 `0` 表示免费报名。
- 每天北京时间 00:00 自动结算前一天，使用安全随机源抽取中奖用户并自动入账。
- 每个用户每天最多报名一次；历史中奖用户永久不能再次报名。
- 普通页面只显示掩码用户名，授权管理页面才显示完整用户名。

### 版本展示

- 移除前端 GitHub 更新检测，不再自动请求上游 GitHub Release 接口。
- 页头仅显示编译时注入的版本号。
- 版本号来自根目录 `VERSION` 文件，并通过以下参数注入：

```bash
-X github.com/QuantumNous/new-api/common.Version
```

## 功能概览

| 功能 | 说明 |
| --- | --- |
| 模型接入 | 支持 OpenAI、Anthropic、Gemini、Azure OpenAI、AWS Bedrock、DeepSeek、通义千问及其他兼容服务 |
| 渠道调度 | 模型映射、渠道优先级、权重、失败重试和多密钥管理 |
| 用量与成本 | 额度、订阅、用量日志、缓存计费和阶梯定价 |
| 访问控制 | 用户、分组、细粒度权限、API Key、OAuth/OIDC、通行密钥和两步验证 |
| 异步任务 | 通过 JavaScript 插件扩展图片、视频等任务接口 |
| Web 控制台 | 渠道管理、模型配置、用量统计、审计日志和 Playground |

## 快速开始

### Docker 单机启动

本项目的 Docker 镜像由 GitHub Actions 构建并发布到你的 GitHub Container Registry：

```bash
docker pull ghcr.io/ztxboss666/new-api-ztx:v1.0.1-ztx
docker run --name new-api-ztx -d --restart unless-stopped \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v "$(pwd)/data:/data" \
  ghcr.io/ztxboss666/new-api-ztx:v1.0.1-ztx
```

打开 <http://localhost:3000>，按照初始化向导创建管理员账号。SQLite 数据会保存在当前目录的 `data` 文件夹中。

如果 GHCR 镜像为私有镜像，先登录：

```bash
echo "$GITHUB_TOKEN" | docker login ghcr.io -u ZTXBOSS666 --password-stdin
```

### Docker Compose

仓库内的 [docker-compose.yml](./docker-compose.yml) 默认使用 PostgreSQL、Redis 和 New API。正式部署前必须修改数据库密码、Redis 密码、`SESSION_SECRET` 和其他敏感配置。

```bash
git clone https://github.com/ZTXBOSS666/new-api-ztx.git
cd new-api-ztx
# 编辑 docker-compose.yml，修改所有默认密码和密钥
docker compose up -d
docker compose logs -f new-api
```

### 本地编译运行

后端编译需要 Go 1.25.1 或更高版本，前端构建需要 Node.js 与项目锁定的前端工具链。进入 `web` 后先生成 `web/dist`，再编译 Go 服务：

```bash
cd web
npm install
npm run build
cd ..

# Linux amd64
go build -trimpath -ldflags "-s -w -X github.com/QuantumNous/new-api/common.Version=v1.0.1-ztx" -o build/new-api-ztx-linux-amd64 .

# Windows amd64（PowerShell）
$env:GOOS='windows'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'; $env:GOWORK='off'
go build -trimpath -ldflags "-s -w -X github.com/QuantumNous/new-api/common.Version=v1.0.1-ztx" -o build/new-api-ztx-windows-amd64.exe .
```

运行前设置 `PORT=3000`、`SQLITE_PATH=./data/one-api.db`，或通过启动参数配置端口和日志目录。

## 发布成品

每次推送版本标签后，GitHub Actions 会自动构建并发布：

- Linux amd64 与 arm64 可执行文件。
- Windows amd64 可执行文件。
- GHCR 多架构 Docker 镜像。
- 对应 SHA-256 校验文件。

发布页：

<https://github.com/ZTXBOSS666/new-api-ztx/releases>

Docker 镜像：

<https://ghcr.io/ztxboss666/new-api-ztx>

## 开发检查

修改 Go 后执行：

```bash
GOPROXY=https://goproxy.cn,direct go test ./controller -run Lottery -count=1
GOPROXY=https://goproxy.cn,direct go build ./...
```

修改前端后，在 `web` 目录执行：

```bash
npm run typecheck
npm run lint
npm run test
npm run build
```

前端格式化使用项目自带的 `oxfmt`，不要使用未配置项目风格的默认 Prettier。详细约定请阅读 [AGENTS.md](./AGENTS.md) 和 [web/AGENTS.md](./web/AGENTS.md)。

## 目录说明

| 目录 | 作用 |
| --- | --- |
| `router/`、`middleware/`、`controller/` | 路由、权限和 API 处理 |
| `model/` | 数据模型、迁移和数据库访问 |
| `service/` | 业务服务、定时任务和权限逻辑 |
| `relay/` | 上游模型适配与请求调度 |
| `web/` | React + TypeScript 管理控制台 |
| `plugins/tasks/` | JavaScript 任务插件 |
| `electron/` | Electron 桌面端 |

## 许可证与上游署名

本项目继续遵循原仓库的 [GNU Affero 通用公共许可证 v3.0（AGPLv3）](./LICENSE) 和 [NOTICE](./NOTICE)。修改版本必须保留原仓库要求的界面署名：

> Frontend design and development by New API contributors.

根据原仓库 NOTICE 的要求，修改版本还必须保留原项目的可见链接：

<https://github.com/QuantumNous/new-api>

本仓库为 ZTXBOSS666 的二次开发版本，源码与本版本修改内容以以下仓库为准：

<https://github.com/ZTXBOSS666/new-api-ztx>

第三方依赖声明见 [THIRD-PARTY-LICENSES.md](./THIRD-PARTY-LICENSES.md)。

## 问题反馈

请在你的仓库提交 Issue，并附上：

- 版本号和提交号。
- 部署方式（源码、二进制或 Docker）。
- 操作系统和数据库类型。
- 可复现步骤。
- 脱敏后的日志和错误信息。

问题反馈入口：

<https://github.com/ZTXBOSS666/new-api-ztx/issues>

---

<div align="center">

感谢使用 New API ZTX。

<a href="https://github.com/ZTXBOSS666/new-api-ztx">项目主页</a> ·
<a href="https://github.com/ZTXBOSS666/new-api-ztx/releases">发布版本</a> ·
<a href="https://github.com/ZTXBOSS666/new-api-ztx/issues">问题反馈</a>

</div>
