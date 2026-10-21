# 验证记录

验证日期：2026-10-21。

## 控制面板上传上限

- 管理员在“偏好设置 → 上传限制”设置服务器单张上限，范围 1–100 MiB。复用 settings 表持久化，无需结构迁移；数据库值优先于启动默认，保存后新上传采用新值。
- `go test -tags=nodynamic ./internal/app -v` 通过。新增用例验证调大后实际上传原先超限的合成 PNG、调低后网页与永久 Token 上传均返回 413、用户独立上限取较小值、普通用户不能更新、无效值/空更新拒绝、部分字段更新不覆盖域名、重建应用恢复已保存值、降低上限后已有图片可读取。
- `go vet -tags=nodynamic ./internal/app`、Windows 后端构建、`npm run build` 通过。PostgreSQL / MySQL 用例仍因未配置连接而跳过。
- 浏览器确认新增输入、范围说明、独立保存按钮，保留当前 20 MiB 执行保存并收到成功反馈；未调整现有账号、图片和单张限制的实际值。截图为 `.cache/preview/panel-upload-limit.png`。
- 本地后端已更新，前端继续运行。仓库 Nginx 请求限制改为 101 MiB，覆盖面板最大值与 multipart 开销；未在本机运行 Nginx / Docker，外层自有代理仍需配置相应请求限制。

## 管理列表、密码与 Token 有效期

- 用户、分享、存储、访问控制统一改为表格列表，窄屏在表格内横向滚动。访问控制通过独立编辑弹窗配置，取消编辑不修改已保存规则。
- 按用户确认取消密码最短长度与复杂度要求，保留账号密码非空、确认一致和 bcrypt 72 字节上限；初始化、创建、重置、本人改密及可选分享密码保持一致。
- 上传 Token 支持常用期限、自定义日期时间、永久。API 使用 expires_at，null 表示永久；界面默认 90 天，完整 Token 仍仅在创建时显示一次。
- `go test -tags=nodynamic ./... -v` 通过：新增短密码创建/初始化/重置/改密/登录、空密码/超长 UTF-8 密码/确认不一致拒绝、短分享密码解锁、永久 Token 上传与撤销、过期拒绝、自定义时区转换及数据库到期列升级测试。固定期限与哈希唯一约束在升级后保持，重复升级可执行。
- `go vet -tags=nodynamic ./...`、CGO 关闭的 Windows 构建和 Linux 交叉编译通过；未运行 Linux 二进制。`npm run build` 类型检查与生产构建通过。
- 浏览器实际检查 1280 像素桌面与 720 像素窄屏：四个列表、长名称/链接省略、编辑弹窗、访问规则草稿取消、密码无 minlength、Token 自定义时间输入与永久状态。窄屏页面无整页横向溢出，宽表格可横向滚动。
- 本地接口创建自定义与永久 Token，并用永久 Token 上传合成图片成功。验收后只撤销本轮两个 Token、删除本轮图片；原账号状态、其他 Token、图片和访问规则保持原状。
- 升级本地开发服务前备份 `.cache/preview` 至 `.cache/backups/preview-before-v04-20261021-165749`，新版继续运行，健康检查通过。PostgreSQL / MySQL 集成仍因没有可用连接而跳过，真实 S3 未实测。

截图：`.cache/preview/list-users.png`、`list-shares.png`、`list-storages.png`、`list-access.png`、`token-lifetimes.png`、`token-custom-form.png`。

## v0.3 统一登录修正

- 移除登录页账号类型选择；管理员与普通用户统一使用 `/auth/login`、`/auth/me`、`/auth/logout`、`/auth/password`。后端识别数据库角色，网络规则通过后才创建会话；旧 `/member/auth/*` 接口移除。
- `go test -tags=nodynamic ./internal/app -v` 通过：管理员与普通账号识别、普通用户会话恢复/退出/改密、CSRF、停用账号、错误密码、旧会话撤销、管理员 IP 与普通用户 Referer 规则独立生效，受网络限制的登录不会建立会话，普通用户不能操作管理接口。PostgreSQL / MySQL 用例未配置连接而跳过。
- `go vet -tags=nodynamic ./internal/app`、Windows 后端构建与 `npm run build` 通过。应急规则测试改为使用实际受管理规则保护的用户管理路径，并单独重跑通过。
- 浏览器实际验证：管理员与普通用户均从只有用户名、密码的同一表单登录；分别确认角色菜单、页面刷新恢复会话和退出。验收后原合成账号恢复停用，配额保持原值。
- 已更新本地开发后端，健康检查通过。最终登录页截图为 `.cache/preview/unified-login.png`。

## v0.3 访客入口修正

- 按用户要求移除登录页访客浏览入口、前端公开相册目录，以及 `GET /api/v1/browse/albums`、`GET /api/v1/browse/images` 接口。旧 `/browse` 路径不再展示公开内容，使用普通登录/工作空间页面。
- `go test -tags=nodynamic ./internal/app -v` 通过：旧公开接口对匿名与管理员均返回 404；图库/相册列表要求登录；开放访客权限后凭图片链接可读取原图与缩略图，私有图片和密码分享继续校验权限。PostgreSQL / MySQL 用例因未配置连接而跳过。
- `npm run build` 通过，重新构建 Windows 后端并更新本地服务。
- 浏览器实际验证：登录页与旧 `/browse` 无访客入口；退出登录后打开上传图片的直链，正常解码为 640×420；密码分享只展示指定图片，保留解锁流程。最终运行服务中再次确认旧公开接口 404、匿名图片直链 200。
- 本轮合成图片与相册已清理、测试分享已撤销，原有数据保留。截图为 `.cache/preview/links-login.png`。

下方为各阶段历史验收记录，公开浏览入口已按本节修正移除。

## v0.3 已执行并通过

- `go test -tags=nodynamic ./... -v`：SQLite 完整接口流程通过；增加账号、相册授权、访客只读、普通用户本人图片管理、管理员全量管理及普通用户管理接口越权测试。
- 检查列表、计数、原图、缩略图及 HEAD 的权限隔离；公开列表不会因浏览器携带管理员 Cookie 泄露私有相册。取消授权、停用账号、撤销 Token 后拒绝访问，网络应急开关不能绕过资源权限。
- 配额覆盖单张原文件限制、容量不足、失败上传释放预占、删除后释放用量、并发预占边界；单实例下最后一份容量只能被一个并发请求取得。
- 分享覆盖单图与相册目标、密码解锁、到期时间、时区转换、目标范围、撤销与已解锁 Cookie 失效、缩略图/HEAD/ETag 防绕过。检查令牌与密码以哈希或密文保存。
- 补充重复用户名返回 409 的回归测试，修复 SQLite 驱动重复键翻译差异；修复后重新执行 `go test -tags=nodynamic ./internal/app -v` 通过，并在最终运行服务中确认明确错误提示。
- `go vet -tags=nodynamic ./...`，`CGO_ENABLED=0` Windows 构建和 Linux 交叉编译通过；未运行 Linux 二进制。最终账号提示修复后重新构建两个目标。
- `npm run build`：Vue TypeScript 检查及 Vite 构建通过。
- 本地浏览器使用合成数据实际检查用户配额、账号编辑字段、相册访客/指定账号授权、分享密码与到期时间展示；普通用户完成授权相册上传、本人图片改名，他人图片详情只读且隐藏管理菜单。上传后个人用量正常刷新。
- 公开页面只展示访客可见相册；密码分享解锁后缩略图与原图正常解码，撤销后重新加载显示失效。
- 本轮三张合成图片与两个相册已清理，测试分享已撤销，测试账号 `qa_member_v03` 已停用；保留验收前已有图片和相册。
- 替换本地后端前备份 `.cache/preview` 到 `.cache/backups/preview-before-v03-20261021-140005`。最终服务继续运行于 `127.0.0.1:8080`，前端 `127.0.0.1:5173`；健康接口与前端 HTTP 请求通过。

规则说明见 [permissions.md](permissions.md)。浏览器验收截图保存在 `.cache/preview/v03-album-access.png`、`v03-member.png`、`v03-users.png`。

## v0.2 已执行并通过

- `go test -tags=nodynamic ./... -v`：SQLite 完整接口流程增加多存储选择、切换默认、停用后读取/删除、引用保护、压缩默认/覆盖、管理规则防自锁、上传/图片规则独立性、HEAD 与缓存校验。
- 使用本机 HTTP 模拟 S3 服务，验证 SigV4 请求形态、对象前缀、Content-Type、读写删除、幂等删除，并通过完整 API 执行 S3 上传、缩略图、网关读取和删除。验证凭据加密、不回显、空字段保留以及应用重启后恢复。
- v1 SQLite 表结构升级到 v2，重复迁移，验证旧图 key、StorageID、大小保持，original_size 正确回填。
- 无损 WebP 逐像素检查透明 RGBA；JPEG EXIF 方向及转换尺寸；ICC / PNG 特殊元数据 / 16 位 PNG / 大图跳过；独立主密钥重复加载、密文篡改、错误密钥和配置 ID 绑定校验。
- IPv4 / IPv6 / CIDR、Referer 子域名边界、空与无效 Referer、可信代理链、伪造 X-Forwarded-For、应急开关测试。
- `go vet -tags=nodynamic ./...`，`CGO_ENABLED=0` Windows 构建和 Linux 交叉编译；未运行 Linux 二进制。
- `npm run build`，通过 Vue TypeScript 检查及 Vite 构建。
- 本地浏览器实际验证管理分组、三个新页面、S3 配置字段、本地存储探测、新建第二存储、上传选项、智能压缩及元数据回退。800×600 合成透明 PNG 从 91109 字节保存为 30106 字节，图库与详情正常解码 WebP，显示上传/保存大小；测试图片与临时存储配置已清理。
- 升级运行中的本地服务前，完整备份 `.cache/preview` 到 `.cache/backups/preview-before-v02-20261021-131151`；沿用原账号与数据库，前后端继续运行。默认压缩和访问规则均保持关闭。

当前压缩行为及其限制见 [compression.md](compression.md)。旧版浏览器验收截图保存在 `.cache/preview/smart-upload.png`、`compression-settings.png`、`access-settings.png`。

## v0.1 已执行并通过

- Go 1.26.10 工具链下执行 `go test ./... -v`，包括 SQLite 完整接口流程、首次启动密码校验、图片域名校验和本地存储路径安全测试。
- 接口流程覆盖登录、CSRF 与来源校验、图片格式/大小/像素限制、原图内容保留、缩略图、搜索与相册筛选、重命名、删除相册保留图片、Token 哈希/权限/撤销、删除失败重试、修改密码撤销会话和 Token、退出登录。
- 执行 `go vet ./...`。
- 使用 `GOOS=linux CGO_ENABLED=0` 编译后端，确认 Linux 目标可构建；没有运行该 Linux 二进制。
- 前端执行 `npm run build`，通过 vue-tsc 类型检查和 Vite 生产构建。
- 本地浏览器实际验证登录、PNG 上传、图库原图/缩略图展示、创建相册、图片重命名与移动相册、无结果搜索、Token 页面和设置页。测试使用独立的 `.cache/preview` 数据目录；验收图片和相册已删除。

## 上传画质与自定义菜单（2026-10-21）

- `go test -tags=nodynamic ./... -v` 与 `go vet -tags=nodynamic ./...` 通过；PostgreSQL / MySQL 未配置连接，明确跳过。
- 覆盖 PNG / WebP 不同画质的实际输出差异，RGBA 与索引 PNG 的逐像素透明度，PNG 画质 100 完整色阶，JPEG 原格式、静态 WebP 再压缩及安全回退。
- SQLite 上传链路检查模式与画质覆盖默认设置，非法画质返回 400；原文件选项保持字节不变。
- 前端 `npm run build` 通过 TypeScript 检查及 Vite 构建。浏览器检查自定义菜单、方向键选择、Escape / 点击外部关闭、滑块实时数值、桌面及窄屏布局；选择框实测高 34px。
- 浏览器上传 800×600 合成透明 PNG：原格式、画质 20，91109 → 75400 字节；修改后续选项，已上传队列仍显示原格式与画质 20。仅清理此合成测试图片，原有两张图片保留。
- 后端以 `CGO_ENABLED=0 -tags=nodynamic` 构建并替换本地预览进程，替换前备份预览目录。此次未重新执行 Linux 交叉编译。
- 截图：`.cache/preview/upload-custom-menu.png`、`.cache/preview/upload-quality-controls.png`。

## 尚未执行

- 本机没有可用的 PostgreSQL、MySQL 或 Docker，两个数据库的集成用例明确跳过。
- 已配置 `.github/workflows/ci.yml`，包含 PostgreSQL 18、MySQL 8.4 服务及相同的集成流程；当前未触发或验证远端 CI。
- Docker 镜像构建和 Compose 启动尚未实测。
- 未做容量压测，不承诺固定内存占用或吞吐量。

- 尚未连接真实 AWS S3、MinIO 等服务，模拟服务验证不能代替目标服务验收。
- 未做真实照片质量对比、400 万像素边界容量压测或固定资源预算验证；不承诺固定压缩率、峰值内存或吞吐量。
