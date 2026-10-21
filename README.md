# QPix

轻量的个人图床。Go 后端与 Vue 前端分离开发，统一构建为一个镜像、一个服务，支持 SQLite、PostgreSQL、MySQL，默认使用本地磁盘存储。

## 功能

- 管理员创建普通账号、关闭注册、账号停用与密码管理。
- 相册默认私有，按账号授权；访客通过收到的图片或分享链接访问，普通用户管理本人图片，管理员管理全部图片。
- 每用户容量和单张大小限制，上传并发预占容量。
- 相册或单图分享，支持密码、到期时间与撤销。
- 拖拽、选择、粘贴和多图上传，最多两个并发，展示进度并支持失败重试。
- 默认保留原文件，上传时可选原格式压缩或转换 WebP，通过 1–100 画质滑块调整；PNG 压缩保留透明度。独立生成最长边 480px 的 PNG 缩略图。GIF 保留动画，缩略图使用首帧。
- 多个本地目录和 S3 兼容存储，可测试连接、设为默认、停用；每次上传可以选择存储。
- 管理 API、上传接口、图片外链分别配置 IP / CIDR 白名单和 Referer 域名规则。
- 图库搜索、分页、网格和列表视图、重命名、相册归类、批量删除。
- 复制直链、Markdown、HTML，支持批量复制。
- 有有效期、可撤销的上传 Token，仅允许调用上传接口。
- 图片访问域名、图片存储用量和服务端限制展示，侧栏分为图片空间与系统管理。
- 删除失败记录持久化，启动时及每 10 分钟重试清理。

管理端必须登录，图片受相册权限、分享授权及 IP / Referer 规则约束。相册默认私有，未分类图片仅本人和管理员可见。支持 JPEG、PNG、GIF、静态 WebP，不接受 SVG。默认单张上限 20 MB、2500 万像素；压缩处理最多 400 万像素，超过时保留原文件。不包含旧兰空数据迁移。

原格式压缩对 JPEG / 静态 WebP 按画质重新编码，对普通 8 位 PNG 量化 RGB 色阶并保留逐像素 alpha；转换 WebP 使用有损编码并保留透明度。初始画质 82，仅采用更小的结果。成功后不额外保存原文件；已有图片、GIF、APNG、16 位 PNG、特殊色彩配置均不重编码。选择算法与限制见 [压缩评估](docs/compression.md)。

## 目录

```text
backend/
  cmd/qpix/           服务入口
  internal/app/       API、会话、图片处理及维护任务
  internal/config/    环境配置
  internal/database/  GORM 模型、三种数据库驱动与版本化迁移
  internal/storage/   本地与 S3 驱动
  internal/processing/ 压缩与方向处理
  internal/secret/    存储凭据加密
frontend/src/         Vue、TypeScript 页面与组件
Dockerfile           前后端统一多阶段构建
deploy/              单服务 Docker Compose
.github/             CI/CD 工作流和镜像验证脚本
docs/                接口、数据备份与存储扩展说明
```

## 本地运行

需要 Go 1.26+、Node.js 24。依赖版本记录于 go.mod/go.sum 和 package-lock.json。SQLite 使用纯 Go 驱动，不需要 CGO 或额外数据库进程。Vite 仅用于开发与构建，生产前端不需要 Node.js 服务。

在第一个 PowerShell 终端运行：

```powershell
cd D:\project\QPix\backend
$env:QPIX_ADMIN_PASSWORD = 'replace-with-a-strong-password'
go run -tags=nodynamic ./cmd/qpix
```

首次启动前请替换上面的示例密码，不能为空且最多 72 字节，不强制最短长度或复杂度。默认用户名为 `admin`。密码只用于初始化；已有账号不会因为修改环境变量而重置密码。

在第二个终端运行：

```powershell
cd D:\project\QPix\frontend
npm ci
npm run dev
```

访问 http://127.0.0.1:5173。开发代理把 `/api/` 和 `/i/` 转发给 `127.0.0.1:8080`，不需要开放跨域。直接运行 Go 不会自动读取 `.env`。

## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| QPIX_ADDR | 127.0.0.1:8080 | 后端监听地址 |
| QPIX_ADMIN_USERNAME | admin | 首次初始化管理员名称 |
| QPIX_ADMIN_PASSWORD | 无 | 首次启动必填，非空且最多 72 字节 |
| QPIX_DB_DRIVER | sqlite | sqlite、postgres、mysql |
| QPIX_DB_DSN | data/qpix.db | SQLite 为普通本地文件路径；其他类型必须填写连接串 |
| QPIX_STORAGE_ROOT | data/uploads | 原图和缩略图目录 |
| QPIX_MASTER_KEY_FILE | data/master.key | 独立加密密钥，首次自动创建；与数据库一起备份 |
| QPIX_TRUSTED_PROXIES | 空 | 可信代理 IP/CIDR，逗号分隔；默认忽略 X-Forwarded-For |
| QPIX_DISABLE_ACCESS_RULES | false | 临时跳过访问规则以恢复管理权限，恢复后关闭 |
| QPIX_MAX_UPLOAD_MB | 20 | 上传上限启动默认值（MiB），1–100；面板保存后以数据库值为准 |
| QPIX_SECURE_COOKIE | false | 通过 HTTPS 访问时设置 true |
| QPIX_APP_URL | 空 | 管理端完整源地址，如 https://qpix.example.com；生产建议明确设置 |
| QPIX_PUBLIC_URL | 空 | 图片域名；空值使用相对地址 |
| QPIX_WEB_ROOT | 空 | 前端构建目录；容器内固定为 /app/web，留空仅提供 API |

通过设置页保存的图片域名优先于环境变量；改为空即可使用当前站点。切换数据库配置只会切换连接，不会自动搬迁数据。数据库与存储目录不要放进前端静态目录。首次本地存储配置在数据库中保存绝对路径；之后调整存储请通过“存储管理”，不要只改环境变量。设置代理和外链缓存的说明见 [访问控制](docs/access.md)。

PostgreSQL 连接示例：

```text
QPIX_DB_DRIVER=postgres
QPIX_DB_DSN=host=127.0.0.1 port=5432 user=qpix password=YOUR_PASSWORD dbname=qpix sslmode=require
```

MySQL 连接示例，数据库须使用 utf8mb4，生产连接按环境配置 TLS：

```text
QPIX_DB_DRIVER=mysql
QPIX_DB_DSN=qpix:YOUR_PASSWORD@tcp(127.0.0.1:3306)/qpix?charset=utf8mb4&parseTime=True&loc=UTC
```

## 容器运行

项目根目录复制 `.env.example` 为 `.env`，填写初始密码及镜像版本。发布镜像为 `ghcr.io/qwerto107/qpix`，包含前后端，支持 amd64 / arm64。首次正式版本发布前可使用 `:dev`；生产推荐固定版本标签。

使用已发布镜像：

```powershell
docker compose --env-file .env -f deploy/compose.yaml pull
docker compose --env-file .env -f deploy/compose.yaml up -d --no-build
```

也可以在本地一次构建前后端：

```powershell
docker compose --env-file .env -f deploy/compose.yaml up -d --build --pull never
```

默认访问 http://127.0.0.1:8090。只有一个 `qpix` 服务，由 Go 同时提供页面和 API，默认只绑定宿主机回环地址，方便接入已有 HTTPS 反向代理。数据库和图片存入 `qpix_data` 命名卷。生产环境设置 `QPIX_APP_URL` 和 `QPIX_SECURE_COOKIE=true`，代理整个站点到同一端口。图片独立域名只需代理 `/i/`（分享图片另使用 `/si/`）。

管理员在“偏好设置 → 上传限制”设置服务器单张上传上限（1–100 MiB），保存后对新上传生效并持久化，重启后继续使用。用户单张上限为 0 时跟随服务器，独立上限与服务器上限取较小值。应用按面板配置限制请求体；外层反向代理须额外放行 multipart 开销，例如 Nginx 使用 `client_max_body_size 101m`。SQLite 采用 WAL 和单连接短事务，仅用于单实例、本机磁盘。PG/MySQL 当前同样按单应用实例使用；多实例迁移调度与分布式限流不在本版范围。

## CI/CD

参考 CloudPaste，仅保留一个构建发布任务：PR 只构建统一镜像，不推送；推送 `main` 发布开发镜像 `:dev` 和 `:sha-<12位提交号>`；推送 `vX.Y.Z` 发布 `:X.Y.Z`、`:latest` 和 SHA 标签，预发布标签不更新 latest。Actions 支持手动填写自定义镜像标签。构建过程包含前端类型检查与 Go 编译，不重复启动测试数据库和验证容器。

仅发布一个 `ghcr.io/qwerto107/qpix` 多架构镜像，使用 GitHub 内置 `GITHUB_TOKEN`，无需 Docker Hub 凭据。仓库配置、GHCR 可见性、版本发布及升级方法见 [CI/CD 说明](docs/cicd.md)。

## 验证

```powershell
cd backend
go test -tags=nodynamic ./... -v
go vet -tags=nodynamic ./...
cd ../frontend
npm ci
npm run build
```

默认执行 SQLite 的完整接口测试和存储路径安全测试。若提供 `QPIX_TEST_POSTGRES_DSN`、`QPIX_TEST_MYSQL_DSN`，同一套接口流程也会在对应数据库运行，未配置时明确跳过。测试使用随机表前缀并在结束时删除这些测试表；请使用专用测试数据库。数据库测试按需执行，不纳入镜像发布流程。

账号、配额与分享见 [docs/permissions.md](docs/permissions.md)，接口见 [docs/api.md](docs/api.md)，备份恢复见 [docs/operations.md](docs/operations.md)，存储配置见 [docs/storage.md](docs/storage.md)，实际验证范围见 [docs/verification.md](docs/verification.md)。
