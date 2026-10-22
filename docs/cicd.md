# CI/CD 与统一镜像

## 构建与发布

工作流为 `.github/workflows/ci.yml`。前后端代码保持分离，使用根目录 Dockerfile 在一次构建中生成，并放进同一个 Alpine 运行镜像。生产只运行非 root 的 Go 进程，由同一个 8080 端口提供 API、图片及 Vue 静态资源；不需要 Nginx 或 Node.js 常驻进程。

参考 CloudPaste 的版本输入、Buildx 多架构和缓存方式，工作流只执行一次统一镜像构建：

1. 检出代码，生成镜像标签，设置 Buildx。
2. 同一个 Dockerfile 完成前端 `npm ci`、TypeScript 检查、Vite 构建和 Go 编译。
3. push / 手动事件使用 `GITHUB_TOKEN` 登录 GHCR，构建并发布包含 amd64 / arm64 的单个镜像。PR 只构建 amd64，不登录或推送镜像。
4. 推送版本标签时，镜像发布成功后由独立的 release 任务创建 GitHub Release，附上镜像拉取命令、部署文档和自动生成的更新说明。预发布标签标记为 Pre-release，不更新最新正式版；重复运行不会重复创建已有 Release。

跨架构阶段使用构建机的 Node.js 生成静态文件，Go 交叉编译后端，无需在 QEMU 中执行整个编译。流水线不再启动测试数据库、重复构建验证镜像或执行独立容器启动测试；单元和数据库集成测试保留在仓库，修改相关功能时按需运行。

## 触发与标签

| 事件 | 镜像标签 | 是否更新 latest |
| --- | --- | --- |
| PR | 只构建、验证 | 否 |
| push main | dev、sha-前12位提交号 | 否 |
| push v1.2.3 | 1.2.3、latest、sha-前12位提交号 | 是 |
| push v1.2.3-rc.1 | 1.2.3-rc.1、sha-前12位提交号 | 否 |
| 手动运行，版本留空 | sha-前12位提交号 | 否 |
| 手动运行，填写 candidate-1 | candidate-1、sha-前12位提交号 | 否 |

手动发布仅允许选择 main。自定义标签必须为小写 Docker 标签，不能使用 latest、dev 或 sha-*；这三个名称由流水线管理。`v*` 推送必须符合 `vX.Y.Z` 或带预发布标识的格式。首次正式发布前不会产生 latest，验证开发版本请使用 dev。

正式版本使用 `git tag -a vX.Y.Z -m "QPix vX.Y.Z"` 和 `git push origin vX.Y.Z` 触发。仅推送 main 或手动填写镜像标签不会创建 GitHub Release。镜像构建失败时不会发布 Release。

## GitHub 配置

- 保持仓库 Actions 启用。构建使用 `contents: read` 和 `packages: write`；仅标签发布的 release 任务使用 `contents: write` 创建 Release，使用内置 `GITHUB_TOKEN`，无需额外凭据。组织策略如果限制 Packages 或 Release 写入，需由仓库管理员允许相应权限。
- 无需手动创建 GHCR Token 或 Docker Hub Secrets。镜像名称自动使用仓库所有者和名称的小写形式，本仓库为 `ghcr.io/qwerto107/qpix`。
- GHCR 新包可能默认为私有。首次发布后，如需匿名拉取，由所有者在包的设置中将可见性设为 Public；保持私有时，部署机器需要自行登录 GHCR。公开代码仓库不代表镜像包已公开。
- 缓存按 qpix 分组复用。同一 ref 的发布串行执行；PR 的重复运行会取消旧构建。
- 不自动配置分支保护；如需强制合并检查，可在仓库规则中要求 build 检查成功。

## 部署与升级

根目录 `.env` 中填写初始管理员密码和 `QPIX_IMAGE`，例如固定版本 `ghcr.io/qwerto107/qpix:1.2.3`。该版本仅为格式示例，须先成功发布对应标签。

```sh
docker compose --env-file .env -f deploy/compose.yaml pull
docker compose --env-file .env -f deploy/compose.yaml up -d --no-build
docker compose --env-file .env -f deploy/compose.yaml ps
```

本地源码构建使用 `up -d --build --pull never`。默认宿主机地址为 `127.0.0.1:8090`，服务名 qpix，数据卷沿用 qpix_data。旧双服务部署切换前先按 [备份说明](operations.md) 备份，再停止旧 backend / web 容器，启动新配置；确认备份后再清理旧容器，不删除数据卷。回滚涉及数据库版本时必须结合数据快照恢复。

服务器更新由操作者执行以上命令。流水线负责构建与交付镜像，不需要保存服务器 SSH 凭据。

## 参考

参考 [CloudPaste 的 Docker 发布工作流](https://github.com/ling-drag0n/CloudPaste/blob/main/.github/workflows/docker-build-all.yml) 的手动版本、Buildx 多架构和构建缓存做法。QPix 只发布一个包含前后端的统一镜像；不复制其中的 Cloudflare Workers / D1 部署流程。

GHCR 凭据与权限依据 [GitHub 官方说明](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images)，多架构构建依据 [Docker 官方说明](https://docs.docker.com/build/ci/github-actions/multi-platform/)。
