# 二进制部署

[文档中心](https://github.com/Qwerto107/QPix/blob/main/docs/README.md) · [部署指南](https://github.com/Qwerto107/QPix/blob/main/docs/deployment.md)

发行程序已包含前端，同时提供页面、API 和图片访问。解压后即可运行，无需安装 Go、Node.js 或图片动态库。本说明也随安装包提供。

[下载](#下载与校验) · [Linux](#linux) · [Windows](#windows) · [macOS](#macos) · [配置](#配置与数据) · [升级](#升级)

## 下载与校验

从 [GitHub Releases](https://github.com/Qwerto107/QPix/releases) 下载压缩包及同名 `.sha256` 文件。附件以对应版本实际发布结果为准。

| 系统／架构 | 压缩包 |
| --- | --- |
| Linux x86-64 | `qpix-linux-amd64.tar.gz` |
| Linux ARM64 | `qpix-linux-arm64.tar.gz` |
| Linux ARMv6 | `qpix-linux-armv6.tar.gz` |
| Linux ARMv5 | `qpix-linux-armv5.tar.gz` |
| Linux LoongArch64 | `qpix-linux-loong64.tar.gz` |
| Windows x86-64 | `qpix-windows-amd64.zip` |
| macOS Apple Silicon | `qpix-darwin-arm64.tar.gz` |

每份包包含程序和 README。ARMv5、ARMv6 请按设备指令集选择，darwin 表示 macOS。

## Linux

以下命令在压缩包所在目录执行，以 x86-64 为例：

```sh
sha256sum -c qpix-linux-amd64.tar.gz.sha256
tar -xzf qpix-linux-amd64.tar.gz
cd qpix-linux-amd64
export QPIX_ADMIN_PASSWORD='replace-this-password'
./qpix
```

其他 Linux 平台替换包名。

## Windows

在 PowerShell 中执行：

```powershell
$archive = 'qpix-windows-amd64.zip'
$expected = (Get-Content "$archive.sha256").Split(' ')[0]
if ((Get-FileHash $archive -Algorithm SHA256).Hash -ne $expected) {
    throw '文件校验失败，请重新下载'
}
Expand-Archive $archive -DestinationPath .
Set-Location qpix-windows-amd64
$env:QPIX_ADMIN_PASSWORD = 'replace-this-password'
./qpix.exe
```

## macOS

```sh
shasum -a 256 -c qpix-darwin-arm64.tar.gz.sha256
tar -xzf qpix-darwin-arm64.tar.gz
cd qpix-darwin-arm64
export QPIX_ADMIN_PASSWORD='replace-this-password'
./qpix
```

## 配置与数据

各平台默认访问 <http://127.0.0.1:8080>，用户名为 `admin`。通过反向代理对外提供访问；需要更改监听地址时设置 `QPIX_ADDR`。

直接运行程序不会读取 `.env`。通过终端或服务管理器设置环境变量，首次密码须非空、最多 72 字节。初始化后在账号安全中改密，环境变量不重置已有密码。

| 默认路径 | 内容 |
| --- | --- |
| `data/qpix.db` | SQLite 数据库 |
| `data/uploads` | 初始本地图片存储 |
| `data/master.key` | S3 凭据及分享令牌的主密钥 |

路径相对进程工作目录。运行账号须有写入权限，长期运行应固定工作目录或使用绝对路径。**保留原主密钥并与数据一起备份。**

`QPIX_WEB_ROOT` 留空使用内嵌页面。数据库、初始存储和密钥使用 [启动参数](https://github.com/Qwerto107/QPix/blob/main/docs/deployment.md#启动参数)；站点、代理、上传限制和多存储通过 [后台设置](https://github.com/Qwerto107/QPix/blob/main/docs/deployment.md#后台运行设置) 配置。

运行 `./qpix healthcheck`，Windows 使用 `./qpix.exe healthcheck`，可检查当前 QPIX_ADDR 的健康接口。返回非零表示失败，该命令不会初始化数据库。

## 升级

停止旧进程，备份数据库、全部存储和原主密钥。校验并解压新版本，保持原数据路径、工作目录和启动环境，再启动程序。前端随程序更新。

备份范围、恢复核对及结构迁移后的回退要求统一见 [备份与恢复](https://github.com/Qwerto107/QPix/blob/main/docs/deployment.md#备份与恢复) 和 [升级与回退](https://github.com/Qwerto107/QPix/blob/main/docs/deployment.md#升级与回退)。

## 源码打包

前端构建、内嵌准备及 Go 构建命令统一放在 [开发指南](https://github.com/Qwerto107/QPix/blob/main/docs/development.md#完整程序与镜像)。
