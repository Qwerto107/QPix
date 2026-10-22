# 更新记录

[文档中心](README.md) · [下载版本](https://github.com/Qwerto107/QPix/releases) · [发布流程](releasing.md)

## 未发布

- 完整程序内嵌前端，增加 Linux amd64、arm64、armv6、armv5、loong64，Windows amd64 和 macOS arm64 二进制打包。
- 统一镜像扩展为五种 Linux 架构，使用 `scratch` 运行层和程序自身健康检查；CI 复用打包程序，不重复构建前后端。
- Release 先创建草稿，七份压缩包及七份 SHA-256 文件上传、核对完整后公开，提供可选竞态与简单容器健康检查。
- 重整 README 与文档导航，同步部署、权限、访问规则、压缩、API 及使用说明，补充实际界面预览。
- 容器配置统一为根目录 `docker-compose.yml`，支持直接下载部署，数据挂载到运行目录的 `./data`。

以上为当前工作区改动，尚未随新版本发布；历史 `v0.1.0` 标签不会自动获得这些二进制附件。

## v0.1.0 · 2026-10-22

首版代码标签，提交 [`f3130c6`](https://github.com/Qwerto107/QPix/commit/f3130c6)。镜像和 Release 的实际发布状态以对应 Actions 运行结果为准。

- Go + GORM 后端、Vue + TypeScript 前端，支持 SQLite、PostgreSQL、MySQL，本地与 S3 多存储。
- 统一登录、管理员创建账号、个人与全站管理分离，每用户容量及单张限制，支持角色调整、账号停用和受约束的删除。
- 单层相册、可更换默认相册、访客直链权限及独立 Referer；删除相册时明确转移图片或连同删除。
- 多图上传、原格式／WebP 压缩、画质滑块和透明度保留；图库三种视图、图片缩放及批量整理。
- 单图密码／期限分享、撤销与记录删除；个人 Token 按相册授权，支持永久、自定义期限和使用记录。
- 站点域名、可信代理、HTTPS Cookie、上传大小／像素上限迁入后台，移除对应运行环境变量读取。
- 统一 Docker 镜像及版本标签触发的 GitHub Actions Release。
