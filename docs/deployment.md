# 部署指南

[文档中心](README.md) · [使用指南](usage.md) · [二进制部署](binary.md)

## 单容器部署（推荐）

一个 `qpix` 容器提供完整页面、API、原图和缩略图。安装 Docker 与 Compose 后，只需准备运行目录、`docker-compose.yml` 和 `.env`。

### 1. 创建运行目录并下载 Compose 文件

Linux 容器使用 UID/GID `65532`。以下权限命令用于新建专用目录；已有目录按原所有权或 ACL 授权，Docker Desktop 按宿主机设置访问权限。

```sh
mkdir -p qpix/data
sudo chown 65532:65532 qpix/data
sudo chmod 700 qpix/data
cd qpix
curl -fL https://raw.githubusercontent.com/Qwerto107/QPix/main/docker-compose.yml -o docker-compose.yml
```

Windows PowerShell：

```powershell
New-Item -ItemType Directory -Force qpix/data
Set-Location qpix
Invoke-WebRequest https://raw.githubusercontent.com/Qwerto107/QPix/main/docker-compose.yml -OutFile docker-compose.yml
```

### 2. 创建环境文件并启动

在运行目录新建 `.env`。下面是完整容器配置示例，替换管理员密码，其余可先采用默认值：

```dotenv
# 首次初始化使用，已有账号在界面中管理。
QPIX_ADMIN_USERNAME=admin
QPIX_ADMIN_PASSWORD='replace-this-password'

# 初始数据库、本地存储与主密钥路径。
QPIX_DB_DRIVER=sqlite
QPIX_DB_DSN=/data/qpix.db
QPIX_STORAGE_ROOT=/data/uploads
QPIX_MASTER_KEY_FILE=/data/master.key

# 仅应急恢复网络访问规则时临时启用。
QPIX_DISABLE_ACCESS_RULES=false
```

首次启动密码不能为空，最多 72 字节，不强制最短长度或复杂度。初始化成功后，更改环境变量不会修改已有管理员密码；在界面右上角“账号安全”修改密码。

```sh
docker compose up -d --pull always
docker compose ps
```

默认访问 <http://127.0.0.1:7979>。容器只绑定宿主机回环地址，适合接入 HTTPS 反向代理。修改访问端口时，直接编辑 `docker-compose.yml` 的 `ports`，例如 `127.0.0.1:7979:8080` 中的 `7979` 为宿主机端口。

镜像地址直接写在 `docker-compose.yml` 的 `image` 中，默认 `ghcr.io/qwerto107/qpix:latest`。生产固定已发布版本时，将该行改为 `ghcr.io/qwerto107/qpix:<版本号>`；`latest` 仅随正式版本更新。统一镜像支持 Linux amd64、arm64、arm/v6、arm/v5、loong64，具体版本以镜像实际发布结果为准。

## 数据与目录

宿主机运行目录的 `./data` 挂载为容器 `/data`，包含：

| 路径 | 内容 |
| --- | --- |
| `/data/qpix.db` 及 WAL 文件 | SQLite 数据库，含账号、图片元数据、运行设置及加密凭据 |
| `/data/uploads` | 初始本地存储的原图与缩略图 |
| `/data/master.key` | 解密 S3 凭据与分享令牌所需的独立主密钥 |

运行用户为 UID/GID `65532:65532`，启动前创建 `data/` 并授予写入权限，避免 Docker 自动创建的目录所有权不匹配。Compose 使用 `./data:/data` 直接绑定宿主机路径，不创建命名卷。容器文件系统只读，数据目录与 `/tmp` 可写。不要将数据库、密钥或图片目录放入前端静态目录。

新增本地存储目录必须在容器中实际挂载并可写。切换默认存储不会迁移旧文件；备份必须覆盖所有本地目录及 S3 对象，不能只保存默认存储。

## 启动参数

直接运行二进制或 Go 不自动读取 `.env`；Compose 自动读取运行目录的 `.env`，并将 `docker-compose.yml` 中声明的参数传入容器。

| 环境变量 | 直接运行默认值 | 说明 |
| --- | --- | --- |
| `QPIX_ADDR` | `127.0.0.1:8080` | 监听地址；容器设为 `0.0.0.0:8080` |
| `QPIX_ADMIN_USERNAME` | `admin` | 首次初始化用户名 |
| `QPIX_ADMIN_PASSWORD` | 无 | 首次初始化密码，非空、最多 72 字节 |
| `QPIX_DB_DRIVER` | `sqlite` | `sqlite`、`postgres`、`mysql` |
| `QPIX_DB_DSN` | `data/qpix.db` | SQLite 本地文件路径，PG／MySQL 必须提供连接串 |
| `QPIX_STORAGE_ROOT` | `data/uploads` | 初始本地存储目录 |
| `QPIX_MASTER_KEY_FILE` | `data/master.key` | 独立主密钥路径，首次自动创建 |
| `QPIX_WEB_ROOT` | 空 | 发行程序使用内嵌页面；可显式指定外置静态目录 |
| `QPIX_DISABLE_ACCESS_RULES` | `false` | 临时跳过网络规则的应急开关，恢复后关闭 |

镜像地址和宿主机端口在 Compose 中设置，不需要对应的 `.env` 变量。容器默认数据库、图片与密钥路径分别为 `/data/qpix.db`、`/data/uploads`、`/data/master.key`。

## 后台运行设置

以下配置保存在数据库中，保存后即时生效并在重启后恢复：

| 后台入口 | 配置 |
| --- | --- |
| 系统设置 → 站点与代理 | 站点地址、可信代理、HTTPS Cookie |
| 系统设置 → 图片访问 | 图片域名，空值使用当前站点 |
| 系统设置 → 上传限制 | 单张大小 1–100 MiB、像素 100 万–1 亿；默认 20 MiB／2500 万像素 |
| 图片处理 | 默认压缩方式及 1–100 画质 |
| 存储管理 | 多个本地／S3 配置及默认存储 |
| 访问控制 | 管理端、上传 IP、图片直链 IP／Referer |
| 用户管理 | 账号状态、角色、容量及单张限制 |

旧 `QPIX_PUBLIC_URL`、`QPIX_APP_URL`、`QPIX_MAX_UPLOAD_MB`、`QPIX_SECURE_COOKIE`、`QPIX_TRUSTED_PROXIES` 已移除，不再读取或作为默认值。已有部署应在后台配置这些项目。初始本地路径首次写入数据库后，修改 `QPIX_STORAGE_ROOT` 不会搬迁已配置的存储。

## PostgreSQL 与 MySQL

SQLite 无需额外数据库进程。切换为其他数据库时先创建数据库及账号，再设置连接串；切换只改变连接，不迁移数据。

```dotenv
QPIX_DB_DRIVER=postgres
QPIX_DB_DSN='host=127.0.0.1 port=5432 user=qpix password=YOUR_PASSWORD dbname=qpix sslmode=require'
```

```dotenv
QPIX_DB_DRIVER=mysql
QPIX_DB_DSN='qpix:YOUR_PASSWORD@tcp(127.0.0.1:3306)/qpix?charset=utf8mb4&parseTime=True&loc=UTC'
```

容器中 `127.0.0.1` 指容器自身，请替换为容器能访问的数据库地址；同一 Compose 网络一般使用数据库服务名。MySQL 使用 `utf8mb4`，生产数据库的 TLS 按实际环境配置。SQLite 使用 WAL 和单连接短事务，数据库文件放在本机磁盘。三种数据库目前均按单应用实例运行。

## HTTPS 与反向代理

代理整个站点至同一个 Go 端口，保留 `/api/`、`/i/`、`/si/` 和 `/s/` 路径；不要把 API 或图片的错误响应回退为 HTML。前端与 API 使用同源地址，不开放 CORS。

Nginx 位置块示例（放入已有 HTTPS 站点，地址按实际网络调整）：

```nginx
location / {
    proxy_pass http://127.0.0.1:7979;
    proxy_set_header Host $http_host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    client_max_body_size 101m;
    proxy_read_timeout 120s;
}
```

`101m` 为面板最大 100 MiB 加 multipart 开销；外层 CDN 或网关的请求限制也需要放行。不要为 `/api/`、`/i/`、`/si/` 强制启用共享缓存；图片与分享的授权检查不能被缓存绕过。

在“站点与代理”填写实际访问的站点源地址，以及后端实际连接到的代理 IP／CIDR。默认不信任转发头；确认访问控制页面显示的客户端 IP 正确，再启用 IP 白名单。通过 HTTPS 访问后再启用安全 Cookie，该项变更会使登录和分享解锁会话失效，个人 Token 保留。

独立图片域名只需代理 `/i/`，如分享资源也走该域名则包括 `/si/`。图片域名不应直接指向公开 Bucket。

## 访问控制

三组设置直接展开、独立保存，默认均关闭：

| 范围 | 作用 |
| --- | --- |
| 管理端 | 管理员登录和登录后的操作；不限制前端 HTML／JS／CSS，不影响普通用户登录 |
| 上传接口 | 所有 `POST /api/v1/images`，仅 IP 与登录／Token 身份，不校验 Referer |
| 图片直链 | 仅 `/i/` 原图及缩略图，检查全局 IP 和有效 Referer；不影响普通 API、分享页或 `/si/` 分享图片 |

IP 支持 IPv4、IPv6、CIDR。Referer 填域名或 IP，不含协议、端口和路径；`*.example.com` 包含子域名，不包含 `example.com`。空 Referer 单独控制，字符串 `null` 与无效 URL 不算空。每类最多 200 项；IP 与 Referer 同时启用时必须同时满足。

相册可继承全局、自定义白名单或不限制 Referer，只覆盖图片直链 Referer 部分，全局 IP 始终有效。“继承全局”来自“访问控制 → 图片直链”，新安装默认未启用，即不限来源并允许空 Referer；全局变化即时作用于继承的相册。访客访问开关、账号归属和 Token 权限仍独立检查。

Referer 是防盗链条件，不能替代身份验证。管理规则保存前检查当前请求能否通过，网络变化后仍可能被限制。外链规则启用时需要允许管理域名；跨域图片也可能因浏览器策略丢失 Referer，是否允许空值按需求设置。

可信代理仅填写后端实际连接的代理 IP／CIDR。程序仅在当前一跳可信时从右向左解析 `X-Forwarded-For`，默认忽略客户端自行提供的转发头。容器网络不要假定宿主机回环地址就是后端看到的代理地址，不要信任 `/0`。

图片及分享访问检查先于存储读取和 ETag／304，响应使用 `Cache-Control: private, no-store`。CDN 旧缓存需主动清理并关闭路径强制缓存，已下载副本无法撤回。

无法登录时，服务端临时设置 `QPIX_DISABLE_ACCESS_RULES=true` 并重启，修正规则后设回 `false` 再重启。该开关只跳过网络规则，不跳过登录、角色、资源归属、分享密码／期限或 CSRF。

## 多存储与主密钥

在“存储管理”配置多个本地目录或 S3，支持连接测试、设置默认、停用与删除。初始配置 ID 为 `local`，上传时不指定 `storage_id` 则使用当前默认。

- 本地目录是运行服务器的路径，相对路径保存为绝对路径；容器目录需挂载并允许 UID/GID `65532` 写入。
- S3 填名称、Region、Bucket、Access Key 与 Secret Key；Endpoint 留空为 AWS S3，可填写不带路径的兼容服务地址，按供应商要求选择 Path Style。对象前缀可选，不自动创建 Bucket。
- 凭据需对象读、写、删除权限，不需要 ListBucket。连接测试写入随机探测文件、读取校验再删除；不保证所有 S3 兼容服务特性。
- 停用仅禁止新上传，旧图仍可读、删和重试清理。默认存储不能停用；默认与初始 `local` 不能删除。存在图片或待删除记录引用时，不能删除配置或修改存储位置与类型。
- S3 Bucket 保持私有，读取通过 QPix 网关；公开对象地址会绕过相册、网络和分享校验。

S3 凭据使用 AES-256-GCM 加密并绑定配置 ID，接口不返回凭据。编辑时两项留空保留原值，轮换需两项一起提交。主密钥还保护可重复复制的分享令牌，须与数据库一同备份并限制文件权限。已有加密记录时缺失、损坏或错误密钥会导致启动失败，不会自动重置。

## 备份与恢复

同时保存数据库、所有存储的图片及缩略图、部署配置和原主密钥。S3 对象使用供应商备份／版本策略。压缩采用后不另存原文件，不能依靠备份设置找回从未保存的原图。

SQLite 最简单的方式是暂停后端写入后备份整个 `data/`；运行中的 WAL 可能含已提交数据，不能只复制 `.db` 文件。在运行目录执行，PowerShell 示例：

```powershell
New-Item -ItemType Directory -Force backups
docker compose stop qpix
docker run --rm -v "${PWD}/data:/source:ro" -v "${PWD}/backups:/backup" alpine:3.23 tar czf /backup/qpix-data.tgz -C /source .
docker compose start qpix
```

使用在线 SQLite Backup API／VACUUM INTO 只保证数据库快照，图片仍需一致快照或暂停写入。PostgreSQL／MySQL 暂停应用写入后用 `pg_dump`／`mysqldump` 及对象备份工具处理。切换数据库不等于迁移。

在独立实例恢复数据库、全部存储、原主密钥和配置，核对登录、相册、图片数量、抽样原图与缩略图后再切换。保留原备份用于回退；不要对旧加密记录生成新密钥。

待删除记录在启动时及每 10 分钟重试。长期失败先检查存储、权限、空间和日志，不要先删数据库记录。进程异常中断可能遗留孤立文件；暂停写入后按图片元数据中的存储 ID、对象 key、缩略图 key 核对，先隔离无引用对象，不按文件时间批量删除。

## 升级与回退

1. 按上述备份步骤保存数据库、全部图片存储、部署配置及原主密钥。
2. 将 `docker-compose.yml` 中的 `image` 改为目标版本，再执行 `docker compose up -d --pull always`。
3. 查看容器日志与健康状态，确认登录和图片访问。

使用旧版 `qpix_data` 命名卷的部署，切换前需停止旧服务，将数据库、图片和原密钥完整复制到新的 `data/` 并调整权限；不要直接用空目录启动或删除原卷。

`schema_migrations` 记录结构版本，与应用版本独立；当前仅支持单实例执行启动迁移。数据库结构迁移后，不能只换回旧二进制或镜像，回退须配合升级前数据快照。流水线负责构建与交付，不自动连接服务器更新部署。
