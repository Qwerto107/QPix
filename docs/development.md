# 开发指南

[文档中心](README.md) · [API 参考](api.md) · [发布流程](releasing.md)

## 环境与结构

使用 Go 1.26 系列（具体工具链见 `backend/go.mod`）、Node.js 24 和 npm。SQLite 为纯 Go 驱动，不需要 CGO；项目依赖由 `go.sum` 与 `package-lock.json` 固定。

```text
backend/
  cmd/qpix/            程序启动与健康检查命令
  internal/app/        认证、资源、管理 API 与页面网关
  internal/config/     启动参数与配置校验
  internal/database/   GORM 模型、驱动与版本化迁移
  internal/storage/    本地与 S3 驱动
  internal/processing/ 压缩、格式及方向处理
  internal/secret/     存储凭据与分享令牌加密
  internal/webui/      按构建标签内嵌前端
frontend/src/          Vue、TypeScript 页面与组件
scripts/               前端构建产物准备
.github/               检查、打包、镜像及 Release 工作流
docker-compose.yml     统一镜像的容器配置
docs/                  使用、部署、接口与维护文档
```

## 分离开发

第一个 PowerShell 终端：

```powershell
cd backend
$env:QPIX_ADMIN_PASSWORD = 'replace-this-password'
go run -tags=nodynamic ./cmd/qpix
```

第二个终端从仓库根目录执行：

```powershell
cd frontend
npm ci
npm run dev
```

访问 <http://127.0.0.1:5173>。Vite 将 `/api/`、`/i/`、`/si/` 代理至 `127.0.0.1:8080`。直接运行 Go 不会读取 `.env`；首次密码仅用于初始化，不能通过更改环境变量重置账号。

不带 `with_frontend` 标签且未设置 `QPIX_WEB_ROOT` 时，仅提供 API。数据默认相对后端工作目录保存在 `data/`，不要将本地数据库、密钥、图片或 `.env` 提交到仓库。

## 完整程序与镜像

先构建前端，再准备固定的内嵌目录：

```sh
npm --prefix frontend ci
npm --prefix frontend run build
node scripts/prepare-webui.mjs
```

在 `backend` 目录使用 `CGO_ENABLED=0` 构建，标签为 `nodynamic,with_frontend`，具体命令见 [二进制部署](binary.md#源码打包)。生成的程序不需要外置前端。准备脚本拒绝隐藏配置、源码映射、依赖目录及符号链接，只替换固定生成目录；不要手工把上传文件放入内嵌目录。

源码镜像在仓库根目录构建，数据目录和 `.env` 按 [部署指南](deployment.md) 准备：

```sh
docker build -t qpix:local .
```

将 `docker-compose.yml` 中的 `image` 改为 `qpix:local`，再执行 `docker compose up -d --pull never`。Dockerfile 默认构建完整程序；CI 的 `release` 目标复用已完成的二进制附件，不重复编译。

## 按需验证

在后端目录运行现有测试及静态检查：

```sh
go test -tags=nodynamic ./...
go vet -tags=nodynamic ./...
```

在前端目录执行 `npm run build`，包含 TypeScript 检查和生产构建。按修改影响选择必要检查，不为简单文案或布局变更新增业务用例。

默认执行 SQLite 测试；提供 `QPIX_TEST_POSTGRES_DSN`／`QPIX_TEST_MYSQL_DSN` 时，同套接口用例在对应数据库运行，未提供则明确跳过。测试创建随机表前缀并清理，只连接专用测试数据库，不使用真实部署库。

CI 使用现有 Go 用例及前端构建，`full_checks` 默认关闭，不引入浏览器测试框架。发版需要的压缩包与 SHA-256 核对属于产物完整性检查。未配置 PG／MySQL 集成连接、真实 S3 或目标硬件时，不将其标记为已实测。

## 维护约定

- 使用中文注释，关键逻辑说明权限、事务或边界原因，保持实现简单。
- 新功能遵循现有路由、组件与目录结构；代码与文档同步更新。
- 数据库结构版本与应用版本相互独立；正式发布后的结构变更新增迁移，不能改写已发布迁移定义。
- 密码、完整 Token、S3 凭据和主密钥不能写入日志、演示截图或仓库。
- 独立需求分别提交；提交与发布需获得项目维护者授权。版本标签应指向明确提交，不移动已有标签。
