# 部署指南

[文档中心](README.md) · [二进制部署](binary.md) · [使用指南](usage.md)

推荐使用 Docker Compose。一个容器提供页面、API 和图片访问；直接运行发行程序见 [二进制部署](binary.md)。当前按单应用实例部署。

| 阶段 | 内容 |
| --- | --- |
| 安装与配置 | [单容器部署](#单容器部署) · [数据目录](#数据与目录) · [启动参数](#启动参数) · [后台设置](#后台运行设置) |
| 数据与访问 | [数据库](#数据库配置) · [HTTPS](#https-与反向代理) · [访问控制](#访问控制) · [多存储](#多存储与主密钥) |
| 运行维护 | [内存与临时空间](#图片处理与内存) · [备份](#备份与恢复) · [升级](#升级与回退) · [排障](#故障排查) |

## 单容器部署

### 准备运行目录

Linux 上创建专用目录，并授权容器用户 UID/GID `65532:65532` 写入：

```sh
mkdir -p qpix/data
sudo chown 65532:65532 qpix/data
sudo chmod 700 qpix/data
cd qpix
curl -fL https://raw.githubusercontent.com/Qwerto107/QPix/main/docker-compose.yml -o docker-compose.yml
```

以上权限命令用于新目录。已有目录按原所有权或 ACL 授权。Windows Docker Desktop 按宿主机权限设置，在 PowerShell 中准备文件：

```powershell
New-Item -ItemType Directory -Force qpix/data
Set-Location qpix
Invoke-WebRequest https://raw.githubusercontent.com/Qwerto107/QPix/main/docker-compose.yml -OutFile docker-compose.yml
```

### 设置密码并启动

在运行目录新建 `.env`。默认 SQLite 部署只需填写初始密码：

```dotenv
QPIX_ADMIN_PASSWORD='replace-this-password'
```

```sh
docker compose up -d --pull always
docker compose ps
```

打开 <http://127.0.0.1:7979>，使用 `admin` 和设置的密码登录。密码须非空，最多 72 字节，不强制最短长度或复杂度。初始化后在界面“账号安全”中改密，更改环境变量不会重置已有密码。

`docker-compose.yml` 直接指定镜像 `ghcr.io/qwerto107/qpix:latest`。固定版本时修改 `image` 为 `ghcr.io/qwerto107/qpix:<版本号>`；更换端口时修改 `127.0.0.1:7979:8080` 中的 `7979`。默认端口仅本机可访问，公网访问需配置反向代理。

## 数据与目录

Compose 使用 `./data:/data` 绑定宿主机目录：

| 容器路径 | 内容 |
| --- | --- |
| `/data/qpix.db` 及 WAL 文件 | SQLite 数据库，包含账号、图片元数据和后台设置 |
| `/data/uploads` | 初始本地存储的原图、预览图与缩略图 |
| `/data/master.key` | 解密 S3 凭据和分享令牌所需的主密钥 |

容器以非 root 用户运行，根文件系统只读，`/data` 和 `/tmp` 可写。新增本地存储时，需要将实际目录挂载进容器，并授予写入权限。数据库使用本机磁盘，数据和密钥放在前端静态目录之外。

## 启动参数

Compose 读取运行目录的 `.env`，将 Compose 中声明的参数传入容器。直接运行程序或 Go 时，通过终端或服务管理器设置环境变量。

| 环境变量 | 直接运行默认值 | 用途 |
| --- | --- | --- |
| `QPIX_ADDR` | `127.0.0.1:8080` | 监听地址；Compose 内为 `0.0.0.0:8080` |
| `QPIX_ADMIN_USERNAME` | `admin` | 首次初始化用户名 |
| `QPIX_ADMIN_PASSWORD` | 无 | 首次初始化密码 |
| `QPIX_DB_DRIVER` | `sqlite` | `sqlite`、`postgres` 或 `mysql` |
| `QPIX_DB_DSN` | `data/qpix.db` | SQLite 文件路径，或 PostgreSQL、MySQL 连接串 |
| `QPIX_STORAGE_ROOT` | `data/uploads` | 初始本地存储目录 |
| `QPIX_MASTER_KEY_FILE` | `data/master.key` | 主密钥路径，首次自动创建 |
| `QPIX_WEB_ROOT` | 空 | 留空使用内嵌页面；可指定外置静态目录 |
| `QPIX_DISABLE_ACCESS_RULES` | `false` | 应急跳过网络规则，恢复后关闭 |

Compose 中的数据库、存储和密钥路径分别默认为 `/data/qpix.db`、`/data/uploads`、`/data/master.key`。镜像地址和宿主机端口直接在 Compose 文件中设置。

管理员变量只用于首次初始化。初始存储写入数据库后，修改 `QPIX_STORAGE_ROOT` 不会搬迁已有文件或修改存储配置。

## 后台运行设置

以下配置保存到数据库，保存后生效，重启后恢复：

| 后台入口 | 配置 |
| --- | --- |
| 系统设置 → 站点与代理 | 站点地址、可信代理、HTTPS Cookie |
| 系统设置 → 图片访问 | 图片域名，留空使用当前站点 |
| 系统设置 → 上传限制 | 单张大小 1～100 MiB、像素 100 万～1 亿；默认 20 MiB、2500 万像素 |
| 图片处理 | 默认压缩方式与画质 |
| 存储管理 | 本地、S3 配置及默认存储 |
| 访问控制 | 管理端、上传接口和直链规则 |
| 用户管理 | 账号、角色、容量及单张限制 |

这些运行设置只从后台读取，无需添加对应环境变量。账号操作和配额含义见 [使用指南](usage.md#管理后台)。

## 数据库配置

### SQLite 维护

SQLite 无需额外进程。`QPIX_DB_DSN` 填普通本地文件路径，程序自动配置 WAL、外键、锁等待和 FULL 刷盘。重建连接会恢复配置。

时间统一按 UTC 写入。升级会规范历史时间文本，包括到期、撤销及使用时间，并保留原时刻和空值。升级前备份数据库，已有大量记录时预留迁移时间。

启动后及每 10 分钟自动按需更新统计信息，并执行 PASSIVE 检查点。外部读事务存在时允许部分完成，不等待读事务退出。WAL 页会被复用，文件不一定立即缩小。

大量清理后确需回收空间时，先备份并停止应用及其他数据库工具，再用 SQLite 命令行执行：

```sql
PRAGMA wal_checkpoint(TRUNCATE);
VACUUM;
```

### PostgreSQL 与 MySQL

先创建专用数据库和账号，再设置连接串。切换连接不会迁移数据。

```dotenv
QPIX_DB_DRIVER=postgres
QPIX_DB_DSN='host=127.0.0.1 port=5432 user=qpix password=YOUR_PASSWORD dbname=qpix sslmode=require'
```

```dotenv
QPIX_DB_DRIVER=mysql
QPIX_DB_DSN='qpix:YOUR_PASSWORD@tcp(127.0.0.1:3306)/qpix?charset=utf8mb4&parseTime=True&loc=UTC'
```

容器中的 `127.0.0.1` 指容器自身，请替换为数据库服务地址。MySQL 使用 `utf8mb4`，TLS 按实际数据库环境配置。

应用连接池最多使用 10 个连接，PostgreSQL 连接最长存活 30 分钟，MySQL 为 3 分钟。数据库及代理的连接超时应高于应用回收时间，并为其他客户端预留连接。完整连接池配置见 [开发指南](development.md#连接与维护)。

### PostgreSQL 搜索索引

图片名称搜索可使用 pg_trgm。由数据库管理员连接 QPix 使用的数据库安装：

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

安装后重启，程序检测扩展并补建名称 GIN 索引。应用账号需要建索引权限，扩展所在 schema 需包含在 `search_path` 中。未安装时输出警告，搜索仍可使用。

升级也会补建列表索引。已有大量数据时，预留首次索引构建时间。短搜索词仍可能扫描，是否采用索引由数据库决定。

## HTTPS 与反向代理

将整个站点代理到 QPix，保留 `/api/`、`/i/`、`/si/` 和 `/s/` 路径。API 和图片错误响应应原样返回。前端与 API 使用同源地址。

下面的 `location` 放入已有 HTTPS 站点。HSTS 和 CSP 由这一层设置；将 `https://img.example.com` 替换为实际图片域名，未使用独立域名时删除该项。

```nginx
location / {
    proxy_pass http://127.0.0.1:7979;
    proxy_set_header Host $http_host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    client_max_body_size 101m;
    proxy_read_timeout 120s;

    add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: https://img.example.com; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'" always;
    add_header Strict-Transport-Security "max-age=31536000" always;
}
```

`101m` 为最大 100 MiB 图片及 multipart 开销预留空间，外层网关也需放行。保持应用的 Cache-Control、nosniff、Referrer-Policy 和 Content-Disposition，关闭图片与分享路径的共享缓存。保留上游 CSP，尤其是 SVG 的 sandbox 限制；外层站点 CSP 可作为额外策略同时发送。已有 `add_header` 配置时合并保留，注意 Nginx 层级继承规则。

HTTP 入口重定向到 HTTPS。启用 HSTS 前确认 TLS 和证书续期可用；确认全部子域支持 HTTPS 后，再考虑 `includeSubDomains`。当前页面需要 CSP 中的内联样式许可。

在后台完成以下设置：

1. 填写实际站点源地址，以及后端实际连接的代理 IP 或 CIDR。
2. 检查访问控制页显示的客户端 IP 是否正确，再启用 IP 白名单。
3. 通过 HTTPS 访问，开启“HTTPS Cookie”。变更会清理登录及分享解锁会话，个人 Token 保留。

独立图片域名代理 `/i/`，需要分享图片时同时代理 `/si/`。图片域名仍指向 QPix 网关，S3 Bucket 保持私有。

## 访问控制

| 范围 | 作用 |
| --- | --- |
| 管理端 | 管理员登录及会话操作；前端静态页面和普通用户登录不受此组限制 |
| 上传接口 | `POST /api/v1/images`，检查 IP 和登录／Token 身份，不检查 Referer |
| 图片直链 | `/i/` 原图、预览图与缩略图，检查 IP 和有效 Referer；不约束普通 API、分享页或 `/si/` |

三组规则默认关闭，分别保存。IP 支持 IPv4、IPv6 和 CIDR；Referer 填域名或 IP，不含协议、端口和路径。`*.example.com` 包含子域名，不包含 `example.com`。IP 和 Referer 同时启用时，两项都须满足。

空 Referer 单独控制；字符串 `null` 和无效 URL 不算空。每类最多 200 项。相册可继承全局、自定义或不限制 Referer，只覆盖 Referer 部分，全局图片 IP 规则仍有效。

可信代理只填写后端实际连接的代理地址，避免信任 `/0`。程序仅在当前一跳可信时从右向左解析转发头。Referer 用于防盗链，身份权限仍独立检查。

### 缓存与规则变更

公开直链在没有有效网络限制、未携带 Token 时，允许浏览器私有缓存，并要求每次重新验证。私有、受限、Token 和分享图片使用 `private, no-store`。权限检查先于 304；命中 ETag 时不读取存储对象。

改变权限、删除图片或撤销分享后，后续请求按当前规则校验。已开始的传输和已下载副本无法收回。已有 CDN 缓存需主动清理，并关闭路径强制缓存。

### 应急恢复

网络规则导致无法登录时，临时设置 `QPIX_DISABLE_ACCESS_RULES=true` 并重启。启动会输出 WARN。修正规则后设回 `false` 并重启。

应急开关只跳过网络规则，登录、角色、资源归属、分享密码与期限及 CSRF 校验继续生效。

## 多存储与主密钥

在“存储管理”添加配置、测试连接、选择默认或停用。上传不指定存储时使用当前默认配置，初始本地配置 ID 为 `local`。

| 类型 | 配置与要求 |
| --- | --- |
| 本地目录 | 服务器或容器内的可写路径；相对路径保存为绝对路径 |
| S3 兼容存储 | Region、Bucket、Access Key、Secret Key；按供应商要求设置 Endpoint、Path Style 和对象前缀 |

Endpoint 留空使用 AWS S3，可填写不带路径的兼容服务地址。Bucket 需提前创建，凭据需要对象读取、写入和删除权限，无需 ListBucket。连接测试写入探测文件、读取校验后删除。

停用只禁止新上传，旧图仍可读和删除。默认存储不能停用；默认存储和初始 `local` 不能删除。存在图片或待删除记录时，存储位置和类型不可修改。切换默认存储不会迁移旧文件。

S3 凭据加密保存，接口不返回明文；编辑时两项都留空保留原值，轮换时两项一起提交。主密钥还保护分享令牌。**保留并备份原主密钥；已有加密数据时，缺失或错误密钥会导致启动失败。**

## 图片处理与内存

按可用内存设置后台像素上限。8 位 RGBA 的一份像素缓冲约为 `宽 × 高 × 4` 字节，2500 万像素约 95 MiB，1 亿像素约 381 MiB。解码、方向校正、缩略图、编码及并发还会增加占用。

按实际内存为 Compose 服务设置 `mem_limit`，同时调低像素上限并用实际图片核对峰值。达到内存限制可能终止进程；压缩行为见 [使用指南](usage.md#压缩细节)。

上传临时空间至少按“8 × 单张大小上限”计算，再为请求开销和其他临时文件留出余量。单张设为 100 MiB 时，仅文件就可能占用 800 MiB。Compose 默认 `/tmp` tmpfs 上限为 1 GiB；已有部署需同步更新 Compose，或自行调整该值。

tmpfs 按实际写入占用内存，上限不等于预分配量。容器总内存还需覆盖图片解码、编码和程序开销。临时文件创建或写入失败时，接口返回 500，具体原因记录在服务端日志。

## 备份与恢复

备份范围包括数据库、所有存储的原图、预览图与缩略图、部署配置和原主密钥。S3 使用供应商备份或版本策略。

SQLite 可停止应用后备份整个 `data/`。运行中的 WAL 可能包含已提交数据，在线备份需使用 SQLite Backup API 或 VACUUM INTO。仅数据库快照无法保证图片与数据库一致，应结合存储快照或暂停写入。

在运行目录执行的 PowerShell 示例：

```powershell
New-Item -ItemType Directory -Force backups
docker compose stop qpix
docker run --rm -v "${PWD}/data:/source:ro" -v "${PWD}/backups:/backup" alpine:3.23 tar czf /backup/qpix-data.tgz -C /source .
docker compose start qpix
```

新增挂载目录、独立密钥路径和 S3 对象需另行备份。PostgreSQL、MySQL 使用 `pg_dump`、`mysqldump`，并同步保留全部存储文件。

在独立实例恢复并核对登录、相册、图片数量和抽样图片后再切换。保留原备份用于回退，恢复时使用原主密钥。

## 升级与回退

1. 停止写入，备份数据库、全部图片存储、配置和原主密钥。
2. 修改 Compose 的镜像版本，执行 `docker compose up -d --pull always`。
3. 检查容器日志、健康状态、登录和抽样图片访问。

程序启动时自动执行版本化迁移。结构迁移后回退，应同时恢复升级前的数据快照。当前只支持单实例执行迁移，流水线不会自动更新服务器部署。

旧部署若使用 `qpix_data` 命名卷，先停止服务并把原数据完整迁入 `./data`，调整权限后再启动。二进制升级见 [启动说明](binary.md#升级)。

## 故障排查

| 现象 | 检查位置 |
| --- | --- |
| 数据库连接或迁移失败 | 查看启动日志的驱动错误、SQL 调用位置与迁移信息；检查地址、TLS、账号权限和连接超时 |
| 容器目录不可写 | 检查挂载路径及 UID/GID `65532:65532` 的权限 |
| 上传返回 413 | 同时检查代理请求大小、后台服务器限制和用户单张／像素限制 |
| 上传返回 429 | 降低并发，并按 Retry-After 重试 |
| 上传提示临时存储不可用（500） | 查看日志中的文件错误，检查 `/tmp` 权限、可用空间及容器内存限制 |
| 图片直链无法访问 | 检查相册访客权限、图片 IP／Referer 规则及当前身份 |
| 已删除图片仍占容量 | 检查后台待清理记录、存储权限及日志，等待重试成功 |

待删除图片在启动时及每 10 分钟重试。出现孤立文件时，暂停写入，按存储 ID、原图、预览图和缩略图 key 对照元数据，先隔离无引用对象；保留数据库记录以便重试清理。
