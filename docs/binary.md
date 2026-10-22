# 二进制部署

[文档中心](https://github.com/Qwerto107/QPix/blob/main/docs/README.md) · [部署指南](https://github.com/Qwerto107/QPix/blob/main/docs/deployment.md)

QPix 发行程序内嵌前端页面，同时提供界面、API 和图片访问，不需要 Go、Node.js、Nginx 或额外静态目录。不提供分离版前端或独立后端安装包。

## 下载与校验

从 [GitHub Releases](https://github.com/Qwerto107/QPix/releases) 下载对应系统的压缩包及同名 `.sha256` 文件。附件由版本标签工作流生成；只有采用二进制发布流程且成功完成的版本才有这些附件。

| 系统／架构 | 压缩包 |
| --- | --- |
| Linux x86-64 | `qpix-linux-amd64.tar.gz` |
| Linux ARM64 | `qpix-linux-arm64.tar.gz` |
| Linux ARMv6 | `qpix-linux-armv6.tar.gz` |
| Linux ARMv5 | `qpix-linux-armv5.tar.gz` |
| Linux LoongArch64 | `qpix-linux-loong64.tar.gz` |
| Windows x86-64 | `qpix-windows-amd64.zip` |
| macOS Apple Silicon | `qpix-darwin-arm64.tar.gz` |

每份压缩包包含程序和本启动说明。ARMv5、ARMv6 分别指定指令集构建，请按设备选择；`darwin` 表示 macOS。程序使用纯 Go SQLite 和图片编解码实现，不依赖 CGO 或系统 WebP 动态库。

## Linux

以 x86-64 为例：

```sh
sha256sum -c qpix-linux-amd64.tar.gz.sha256
tar -xzf qpix-linux-amd64.tar.gz
cd qpix-linux-amd64
export QPIX_ADMIN_PASSWORD='replace-this-password'
./qpix
```

默认访问 <http://127.0.0.1:8080>，管理员用户名为 `admin`。其他 Linux 平台替换压缩包名称。需监听其他地址时设置 `QPIX_ADDR`，例如 `0.0.0.0:8080`。

## Windows

在 PowerShell 执行：

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

- 直接运行程序不会自动读取 `.env`，请通过终端或服务管理器设置环境变量。
- 初始密码不能为空，最多 72 字节，不强制最短长度或复杂度。只在首次创建管理员时使用；修改环境变量不会重置已有密码。
- 默认 SQLite 为工作目录下的 `data/qpix.db`，图片为 `data/uploads`，主密钥为 `data/master.key`。运行账号必须有写入权限，长期运行建议固定工作目录或设置绝对路径。
- 数据库类型、连接串、初始存储与密钥路径在启动环境中配置。站点地址、图片域名、可信代理、HTTPS Cookie、上传限制、多存储、压缩及访问规则在后台配置。
- `QPIX_WEB_ROOT` 留空使用内嵌页面；显式指定时使用该外置目录。二进制与容器均通过同一端口提供页面及 `/api/`、`/i/`、`/si/`，分享页 `/s/*` 支持直接打开与刷新。
- `./qpix healthcheck`（Windows 为 `./qpix.exe healthcheck`）检查当前 `QPIX_ADDR` 的健康接口，返回非零表示失败；不会初始化数据库或修改数据。

全部启动参数见 [部署指南](https://github.com/Qwerto107/QPix/blob/main/docs/deployment.md#启动参数)，备份数据库、所有存储和原主密钥的方法见 [备份恢复](https://github.com/Qwerto107/QPix/blob/main/docs/deployment.md#备份与恢复)。当前仅支持单应用实例。

## 升级

停止旧进程并备份数据库、图片及密钥，校验并解压新版本，保留原数据路径和启动环境，再启动新程序。前端随程序一起更新，无需单独替换页面。不要把新程序目录中默认的空 `data/` 误当作原数据目录；数据库结构回退应结合升级前快照恢复。

## 源码打包

在仓库根目录构建前端并准备内嵌目录：

```sh
npm --prefix frontend ci
npm --prefix frontend run build
node scripts/prepare-webui.mjs
cd backend
CGO_ENABLED=0 go build -tags='nodynamic,with_frontend' -trimpath -ldflags='-s -w' -o qpix ./cmd/qpix
```

PowerShell 中先设置 `$env:CGO_ENABLED='0'`，再执行同一 `go build` 命令并将输出名改为 `qpix.exe`。普通分离开发仍使用 Vite 和不带 `with_frontend` 的 Go 启动命令；发行包统一内嵌。
