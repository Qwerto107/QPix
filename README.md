<div align="center">

<img src="frontend/public/favicon.svg" width="72" height="72" alt="QPix">

# QPix

**上传、整理，分享你的图片。**

可自托管的轻量图床，用相册整理个人图片，通过直链或受密码、期限保护的链接分享。

[快速部署](#快速部署) · [文档中心](docs/README.md) · [API 参考](docs/api.md) · [下载版本](https://github.com/Qwerto107/QPix/releases)

**Go + Vue** · **SQLite / PostgreSQL / MySQL** · **本地 / S3**

**统一镜像** · Linux amd64 / arm64 / armv6 / armv5 / loong64<br>
**完整程序** · Linux / Windows amd64 / macOS arm64

</div>

---

## 界面一览

个人图库以相册为入口，图片支持大图标、小图标和详细信息视图。个人空间与系统管理分开，常用操作保持紧凑。

![我的图库：按相册整理图片](docs/assets/library.jpg)

<details>
<summary><strong>查看更多：上传、预览、权限与系统设置</strong></summary>

![上传图片：选择相册、存储与压缩画质](docs/assets/upload.jpg)

![图片详情：缩放、原始大小和图片信息](docs/assets/image-detail.jpg)

![个人 Token：按相册选择操作权限](docs/assets/token-permissions.jpg)

![系统设置：站点、代理与上传限制](docs/assets/settings.jpg)

截图使用独立演示数据，不包含真实凭据。

</details>

## 功能

| 能力 | 使用方式 |
| --- | --- |
| **图片上传** | 拖拽、选择、粘贴与多图队列，展示进度、失败重试，单图一条链接 |
| **相册整理** | 单层相册、可更换默认相册、搜索与排序、批量移动及删除 |
| **图片压缩** | 保留原文件、原格式压缩或转 WebP，画质滑块 1–100，保留透明度 |
| **图片预览** | 大小图标与详细列表，详情支持缩放、100% 原始大小、适应窗口及拖动 |
| **链接分享** | 直链、Markdown、HTML；单图分享可设置密码、有效期、撤销及删除记录 |
| **账号与配额** | 统一登录，关闭注册，管理员创建账号，每用户容量和单张上限 |
| **个人 Token** | 全部本人相册或逐相册授权，设置操作权限、期限、撤销与清理记录 |
| **多存储** | 多个本地目录和 S3 兼容配置，测试连接、选择默认、上传时切换 |
| **访问控制** | 管理端及直链分别配置 IP／Referer；上传只校验 IP 与身份，相册覆盖 Referer |
| **系统管理** | 全站图片、分享、Token、用户及运行设置，与管理员自己的资源分开 |

未指定相册时上传至本人默认相册。新相册默认允许访客凭已知直链查看，可关闭访客访问；不提供公开图库、相册成员授权或相册分享。单图分享默认永久，个人 Token 默认 90 天，均可选常用期限或自定义时间。

## 快速部署

推荐使用统一镜像，一个容器提供页面、API 和图片访问。安装 Docker 与 Compose 后，创建运行目录并下载配置，无需克隆源码。

以下权限命令用于 Linux 新建专用目录；已有目录或 Docker Desktop 的权限设置见 [部署指南](docs/deployment.md)。

```sh
mkdir -p qpix/data
sudo chown 65532:65532 qpix/data
sudo chmod 700 qpix/data
cd qpix
curl -fL https://raw.githubusercontent.com/Qwerto107/QPix/main/docker-compose.yml -o docker-compose.yml
```

在 `qpix` 目录新建 `.env`，填写自己的管理员密码（非空，最多 72 字节，不强制最短长度或复杂度）：

```dotenv
QPIX_ADMIN_PASSWORD='replace-this-password'
```

```sh
docker compose up -d --pull always
```

1. 打开 <http://127.0.0.1:7979>，使用 `admin` 和设置的密码登录。
2. 按需添加相册与存储，设置站点地址、上传限制及压缩默认值。
3. 上传图片并复制直链；需要密码或期限时创建单图分享。

密码只用于首次初始化，更改环境变量不会重置已有密码。数据保存在运行目录的 `data/`，端口默认仅本机可访问；固定镜像版本或更改端口时直接编辑 `docker-compose.yml`。Windows、反向代理、数据库与升级见 [部署指南](docs/deployment.md)。

不使用 Docker 时，从 [Releases](https://github.com/Qwerto107/QPix/releases) 下载内嵌前端的完整程序，按 [二进制部署](docs/binary.md) 校验并启动。平台与附件以相应版本实际发布结果为准，历史仅镜像版本不会自动补充二进制。

## 使用边界

- 支持 JPEG、PNG、GIF、静态 WebP，不接受 SVG 或动画 WebP；GIF 保留原动画。
- 默认单张 20 MiB、2500 万像素，在后台“系统设置 → 上传限制”修改；用户上限与服务器上限取较小者。
- 默认保留原文件。压缩最多处理 400 万像素，只采用更小的结果；采用后不另存原文件副本，详见 [使用指南](docs/usage.md#压缩细节)。
- 个人资源按账号隔离。管理员拥有个人空间，系统管理权限仅用于登录会话，个人 Token 不继承全站权限。
- 原图、缩略图和分享通过 QPix 网关访问；S3 Bucket 保持私有，授权图片与分享不能由 CDN 共享缓存。
- 当前按单应用实例运行，SQLite 使用本机磁盘；不包含旧图床数据自动迁移、回收站或存储自动迁移。

## 文档导航

| 想了解什么 | 文档 |
| --- | --- |
| 相册、上传、预览与分享 | [使用指南](docs/usage.md) |
| Docker、环境变量、反向代理与升级 | [部署指南](docs/deployment.md) |
| Linux、Windows、macOS 单文件运行 | [二进制部署](docs/binary.md) |
| API 请求、参数与返回值 | [API 参考](docs/api.md) |
| 本地开发、构建与验证 | [开发指南](docs/development.md) |
| 二进制、多架构镜像与自动 Release | [发布流程](docs/releasing.md) |
| 版本变化 | [更新记录](docs/changelog.md) |

---

<div align="center">Go + GORM + Vue · 自托管图片空间</div>
