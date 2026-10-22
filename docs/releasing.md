# 发布流程

工作流为 [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)，名称为“构建、检查与发布”。交付内嵌前端的完整程序与统一镜像，不生成分离版程序或镜像。

## 触发方式

| 触发 | 检查 | 打包与发布 |
| --- | --- | --- |
| 相关分支提交／PR | 现有 Go 用例、格式与静态检查、前端类型检查及构建 | 不打包、不发布 |
| 手动运行 | 常规检查 | 七平台打包，产物可在 Actions 下载；不创建 Release |
| 手动勾选 `publish_images` | 常规检查，按 `full_checks` 决定额外检查 | 七平台打包，发布完整提交 SHA 镜像，不更新 latest、不创建 Release；仅允许 main |
| 手动勾选 `full_checks` | 对现有 Go 用例跑竞态及依赖漏洞检查，简单检查统一容器健康及首页 | 按 `publish_images` 决定是否推送镜像 |
| 推送 `vX.Y.Z` | 常规检查及依赖漏洞检查 | 七平台打包、五架构镜像、正式 Release，更新 latest |
| 推送 `vX.Y.Z-rc.1` 等 | 与正式版相同 | 版本镜像及预发布 Release，不更新 latest |

`full_checks` 默认关闭，启用时竞态测试替代普通 Go 测试，不重复执行；CI 不启动 PostgreSQL／MySQL 测试服务。日常检查只保留最新提交，标签发布及手动镜像发布不中途取消。

## 构建顺序

1. 后端运行现有检查；前端只构建一次，安全复制到内嵌目录并保存为 `qpix-webui-dist` 产物。
2. 七个平台复用同一份前端，以 `CGO_ENABLED=0` 和 `nodynamic,with_frontend` 标签生成完整程序，打包并计算 SHA-256。
3. 镜像发布要求前后端和全部二进制成功；如启用可选容器检查，也必须通过。镜像直接复用本次打包的 Linux 程序，不重新构建前端或编译后端。
4. 标签发布在镜像成功后创建 Release 草稿，上传全部压缩包及校验文件，下载并核对服务器上的实际附件，完成后才公开。

任一必要任务失败，不公开新 Release。重跑只补充缺失附件；已有附件的校验文件必须与本次构建一致，不覆盖不匹配的历史附件。

## 程序与附件

完整程序支持 Linux amd64、arm64、armv6、armv5、loong64，Windows amd64 和 macOS arm64。Windows 使用 ZIP，其余使用 tar.gz；包内包含可执行文件和 [启动说明](binary.md)。

每个 Release 提供七份压缩包及七份同名 `.sha256` 文件。Actions 产物名为 `qpix-binaries-{平台}`，保留 30 天；前端产物保留 7 天。打包固定文件排序、时间和所有者，便于同一提交重跑时核对附件。

历史版本只有镜像时，更新 main 不会自动向旧标签补附件；必须由使用新流程的版本标签触发。

## 统一镜像

镜像为 `ghcr.io/qwerto107/qpix`，支持 `linux/amd64`、`linux/arm64`、`linux/arm/v6`、`linux/arm/v5`、`linux/loong64`。

| 场景 | 标签 |
| --- | --- |
| 正式版 `v1.2.3` | `1.2.3`、`latest`、`sha-<完整提交号>` |
| 预发布 `v1.2.3-rc.1` | `1.2.3-rc.1`、`sha-<完整提交号>` |
| 手动勾选镜像发布 | `sha-<完整提交号>` |

日常 main 提交不自动发布 `dev`，手动运行仅发布提交号镜像。正式发布使用符合语义版本格式的 `v*` 标签。

运行层为 `scratch`，包含纯 Go 完整程序、CA 证书、时区及非 root 用户信息，不依赖目标架构系统运行库。ARM 明确使用 `GOARM=5/6`。证书和时区从构建主机架构的 Alpine 阶段取得，不需要 QEMU。健康检查直接执行 `/qpix healthcheck`，镜像中没有 Shell 或 wget。

Dockerfile 默认的 `standalone` 目标保留本地源码构建；CI 使用 `release` 目标，复制已校验的二进制。两者使用同一运行层，前端都已内嵌。

## 仓库权限

检查和打包仅申请 `contents: read`；镜像任务额外申请 `packages: write`，Release 任务申请 `contents: write`。全部使用内置 `GITHUB_TOKEN`，无需额外仓库令牌或 Docker Hub 凭据。

GHCR 包需允许当前仓库写入。匿名拉取需将包可见性设为 Public；公开代码仓库不代表镜像自动公开。镜像默认以 UID/GID `65532:65532` 运行，挂载的数据目录须允许该账号写入。

## 发布与部署

完成验收并提交推送后，创建并推送新标签，例如：

```sh
git tag -a v1.2.3 -m "QPix v1.2.3"
git push origin v1.2.3
```

版本号为格式示例，不表示已发布。无需本地创建 Release，Actions 会完成镜像与附件发布；只推送 main 不会发布正式版本。版本标签不要移动到其他提交。

容器部署只需下载根目录的 `docker-compose.yml`，在运行目录新建 `.env` 并填写初始密码，按 [部署指南](deployment.md) 准备可写的 `data/` 后执行：

```sh
docker compose up -d --pull always
```

默认地址 <http://127.0.0.1:7979>，单个 `qpix` 服务提供页面与 API，数据库、图片和密钥保存在运行目录的 `data/`。镜像版本直接修改 `docker-compose.yml` 中的 `image`；源码镜像构建见 [开发指南](development.md#完整程序与镜像)。

升级前按 [备份说明](deployment.md#备份与恢复) 保存数据库、所有存储和主密钥，修改镜像版本再执行上述更新命令。流水线不保存服务器 SSH 凭据，也不自动更新实际部署。
