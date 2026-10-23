# 开发指南

[文档中心](README.md) · [API 参考](api.md) · [发布流程](releasing.md)

[环境](#环境与结构) · [本地运行](#分离开发) · [构建](#完整程序与镜像) · [验证](#按需验证) · [数据库与并发](#数据库与并发约定) · [图片处理](#图片处理)

## 环境与结构

使用 Go 1.26.4 或更高版本（工具链见 `backend/go.mod`）、Node.js 24 和 npm。SQLite 和图片编解码使用 Go／内嵌 WASM，发行构建关闭 CGO，无需安装外部编解码程序。依赖由 `go.sum` 和 `package-lock.json` 固定。

```text
backend/
  cmd/qpix/            启动与健康检查命令
  internal/app/        认证、资源、管理 API 与图片网关
  internal/config/     启动配置
  internal/database/   模型、驱动、迁移及 SQLite 维护
  internal/storage/    本地与 S3 驱动
  internal/processing/ 压缩、方向与格式处理
  internal/secret/     凭据及分享令牌加密
  internal/webui/      按构建标签内嵌前端
frontend/src/          Vue 与 TypeScript 页面
scripts/               内嵌前端准备脚本
.github/               检查、打包与发布
docker-compose.yml     统一镜像运行配置
docs/                  使用与维护文档
```

## 分离开发

第一个 PowerShell 终端从仓库根目录启动后端：

```powershell
cd backend
$env:QPIX_ADMIN_PASSWORD = 'replace-this-password'
go run -tags=nodynamic ./cmd/qpix
```

第二个终端从仓库根目录启动前端：

```powershell
cd frontend
npm ci
npm run dev
```

访问 <http://127.0.0.1:5173>。Vite 将 `/api/`、`/i/` 和 `/si/` 代理到 `127.0.0.1:8080`。Go 不自动读取 `.env`；数据默认保存在后端工作目录的 `data/`，初始密码只用于首次创建管理员。

未启用 `with_frontend` 且未设置 `QPIX_WEB_ROOT` 时，后端仅提供 API。源码、数据、密钥、上传文件和本地环境配置应分别保存。

## 完整程序与镜像

### 构建完整程序

从仓库根目录构建前端并准备内嵌目录：

```sh
npm --prefix frontend ci
npm --prefix frontend run build
node scripts/prepare-webui.mjs
```

在 `backend` 目录执行：

```sh
CGO_ENABLED=0 go build -tags='nodynamic,with_frontend' -trimpath -ldflags='-s -w' -o qpix ./cmd/qpix
```

PowerShell 使用以下命令：

```powershell
$env:CGO_ENABLED = '0'
go build -tags='nodynamic,with_frontend' -trimpath -ldflags='-s -w' -o qpix.exe ./cmd/qpix
```

`nodynamic` 使用 Go／内嵌图片编码路径，`with_frontend` 内嵌页面。准备脚本只替换固定生成目录，并拒绝隐藏配置、源码映射、依赖目录和符号链接。生成的程序与 [发行程序](binary.md) 使用同一启动配置。

### 构建本地镜像

在仓库根目录执行：

```sh
docker build --build-arg QPIX_COMMIT_HASH="$(git rev-parse HEAD)" -t qpix:local .
```

浏览器控制台显示版本及 8 位提交哈希。构建时优先读取 `QPIX_COMMIT_HASH`、`GITHUB_SHA`，否则读取当前 Git 提交；缺少元数据时显示 `unknown`。Docker 源码构建通过上述参数传入哈希，CI 发布镜像复用已包含哈希的前端产物。

按 [部署指南](deployment.md) 准备可写的数据目录和 `.env`。把 Compose 的 image 改为 `qpix:local`，执行 `docker compose up -d --pull never`。

Dockerfile 默认 standalone 目标从源码构建。CI 的 release 目标复用打包程序，流程见 [发布文档](releasing.md)。

## 按需验证

后端目录的常用命令：

```sh
go test -tags=nodynamic ./...
go vet -tags=nodynamic ./...
```

前端目录执行 `npm run build`，包含 TypeScript 检查和生产构建。根据修改影响选择必要检查；纯文档调整检查链接和命令即可。

| 验证范围 | 配置 |
| --- | --- |
| SQLite | 默认执行，使用临时数据库 |
| PostgreSQL | 设置 `QPIX_TEST_POSTGRES_DSN` |
| MySQL | 设置 `QPIX_TEST_MYSQL_DSN` |

外部数据库测试创建随机表前缀并清理，请连接专用测试数据库。未配置连接时，对应测试明确跳过。真实 S3、目标硬件和大数据量性能需要单独验证。

CI 的常规检查和可选 `full_checks` 见 [发布流程](releasing.md#触发方式)。

## 数据库与并发约定

### 事务与权限

- 多语句原子操作显式使用事务，事务内始终使用 tx。单语句写入跳过 GORM 默认事务。
- 数据库结构版本独立于应用版本，新增结构变更使用新迁移；迁移需兼容失败重试及 MySQL 非事务 DDL。
- 图片用量通过 `used_bytes` 计数。上传前按原文件大小预占，完成后结算，失败释放。上传、删除记录和计数更新处于同一事务；物理删除失败时保留用量记录。
- PostgreSQL、MySQL 配额操作先锁定用户主键行，再读取预占。SQLite 使用单连接和进程锁。
- 策略锁只保护必要的状态校验与数据库写入。请求接收、bcrypt、存储 I/O、图片传输和响应写出放在锁外，写入前重新检查权限及目标状态。
- 自定义 Referer 只缓存编译结果。每次查询当前相册策略并比对内容，变更主动失效。

### 连接与维护

| 配置 | SQLite | PostgreSQL | MySQL |
| --- | --- | --- | --- |
| 最大连接／空闲连接 | 1／1 | 10／10 | 10／10 |
| 最长连接存活 | 不限 | 30 分钟 | 3 分钟 |
| 空闲回收时间 | 不限 | 5 分钟 | 5 分钟，存活上限先到则先回收 |
| 预编译缓存 | 不额外启用 | 使用驱动默认行为 | 最多 100 种 SQL，有效期 5 分钟 |

SQLite 的连接级 PRAGMA 由内部 DSN 设置，每次建连都会应用。迁移后执行 `optimize=0x10002`，维护任务执行 optimize 和 PASSIVE 检查点。数据库连接、手动维护和 pg_trgm 配置见 [部署指南](deployment.md#数据库配置)。

GORM 自动时间和 SQLite 绑定时间统一为 UTC，SQLite 保留原驱动文本格式。迁移版本 9 规范既有时间字段，避免游标及今日统计混用时区；测试需包含非 UTC 时间。

迁移版本 10 增加可空的 `preview_key`。旧图片及缩略图继续可读，升级不会批量生成预览。

### 列表分页

图库、分享和 Token 列表使用游标分页。网页保存已访问页的游标支持上一页，总数来自最近一次首页请求；筛选变化或刷新后重取首页。接口参数、排序和空总数约定见 [API 参考](api.md#列表分页)。

## 图片处理

### 上传与编码

图片接收最多 8 个在途请求、每账号 2 个，解码和编码阶段全局并发 2 个。保留原文件时流式写入存储，符合压缩条件时才读入原文件字节。用户可选方式、重编码限制和元数据处理见 [压缩细节](usage.md#压缩细节)，内存及临时空间预算见 [部署指南](deployment.md#图片处理与内存)。

PNG 使用色阶量化与 DEFLATE，画质 100 保留完整 8 位色阶；量化对 NRGBA、RGBA 使用 Pix 快路径，并保留透明度。JPEG、PNG 可并行，WebP 编码保留独立互斥。

BMP／TIFF 使用 `x/image`，ICO 使用有界目录校验及 PNG 帧编码。AVIF 使用 `gen2brain/avif` 内嵌 WASM，HEIC 使用 `gen2brain/h265/heic`，静态 JXL 使用 `gen2brain/jxl`。SVG 先校验 XML，再尝试用 `oksvg`／`rasterx` 生成静态缩略图；不支持的绘图特性由浏览器预览原文件。动画和多页文件避免重编码，ICO 转 WebP 显式取最大尺寸。

上传默认以随机 ID 命名，`random_name=false` 仅改变显示名称。原文件、独立预览和缩略图都用同一个 ID 的不同后缀保存，失败补偿及删除清理覆盖三类对象。

### 缩略图

缩略图最长边为 480 px。不透明图片使用 JPEG，含透明度的图片使用 PNG。双边不超过 480 px、体积不超过 128 KiB 的 JPEG、PNG 和 ICO 直接复用原图。GIF、动画 WebP 单独生成首帧缩略图。

浏览器兼容性有限的格式另生成最长边 1920 px 的详情预览，使用相同的透明度判断。旧 PNG 缩略图继续可读。客户端使用 API 返回的 `preview_url` 和 `thumbnail_url`，不自行推断格式和后缀。预览经过同一个网关检查身份、相册及分享权限。

## 协作约定

代码与关键逻辑注释使用中文，修改遵循现有目录和技术栈。功能变更同步更新对应使用说明或 API 约定；权限和持久化行为以代码及必要验证为依据。

密码、完整 Token、S3 凭据、主密钥和本地数据不写入日志、截图或仓库。提交、推送及发布遵循根目录的 [AGENTS.md](../AGENTS.md)。
