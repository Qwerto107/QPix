# API 参考

[文档中心](README.md) · [使用指南](usage.md) · [部署指南](deployment.md)

| 内容 | 章节 |
| --- | --- |
| 接入约定 | [请求与错误](#请求约定) · [登录会话](#登录会话) · [个人 Token](#用户-api-token) · [分页](#列表分页) |
| 图片资源 | [上传与管理](#图片与上传) · [相册](#相册) · [分享](#单图分享) · [有效期](#有效期) · [图片访问](#图片访问) |
| 后台接口 | [用户、设置、存储、图片处理与访问控制](#系统管理) |

## 请求约定

API 前缀为 `/api/v1`。下文表格省略此前缀，图片网关 `/i/`、分享图片 `/si/` 和前端分享页 `/s/` 除外。

成功返回 JSON，错误返回 `{"error":"说明"}`。上传使用 multipart/form-data，其余带请求体的接口使用 JSON。

| 状态码 | 含义 |
| --- | --- |
| 400 | 参数错误或当前密码不正确 |
| 401／403 | 未登录，或身份、权限、来源校验失败 |
| 404 | 资源不存在或不可访问 |
| 409 | 容量不足、资源仍有引用或操作状态冲突 |
| 413 | 超过大小或像素上限 |
| 415 | 不支持的图片格式 |
| 429 | 请求过于频繁，可按 Retry-After 重试 |
| 500 | 服务内部错误，具体原因记录在服务端日志 |

服务不开放 CORS。浏览器与 API 使用同源地址，开发环境通过 Vite 代理。写请求校验 Origin；Bearer 请求同样受来源及对应 IP 规则约束。

### 请求频率

| 操作 | 计数范围 | 上限 |
| --- | --- | --- |
| 登录 | 来源 IP | 每 15 分钟 15 次 |
| 分享解锁 | 来源 IP | 每 15 分钟 15 次 |
| 上传图片 | 账号，会话与个人 Token 共用 | 每分钟 120 次 |
| 创建分享 | 账号，会话与个人 Token 共用 | 每分钟 30 次 |

超限返回 429，重试等待时间见 `Retry-After`。分享创建超限不影响已有分享访问。

## 登录会话

管理员与普通用户统一登录，角色由后端识别。系统管理和 Token 管理接口只接受登录会话。

| 方法与路径 | 请求或返回 |
| --- | --- |
| `POST /auth/login` | username、password；返回 user、csrf_token 并设置 Cookie |
| `GET /auth/me` | 当前 user 和 csrf_token |
| `POST /auth/logout` | 退出当前会话 |
| `PUT /auth/password` | current_password、new_password；成功后撤销本人登录会话，保留个人 Token |
| `GET /health` | 公共健康检查，包含数据库连接检查 |

会话有效期 7 天，Cookie 使用 HttpOnly、SameSite=Strict。修改请求通过 `X-CSRF-Token` 提交 CSRF 值。刷新页面后使用 `/auth/me` 恢复，密码确认由网页完成，改密 API 接受表中两个字段。

管理员登录及会话操作应用管理端网络规则；普通用户 API 不应用图片直链规则。代理地址解析见 [访问控制](deployment.md#访问控制)。

## 用户 API Token

通过 `Authorization: Bearer <token>` 提交 Token。Token 请求无需 Cookie 或 CSRF；无效 Bearer 不会回退到附带的 Cookie。

管理员和普通用户的个人 Token 都只操作本人授权相册，不继承全站管理权限。Token 不能访问 `/auth`、`/tokens` 或系统管理接口，也不接受 `scope=all`。

### 授权字段

创建或修改 Token 时，authorization 接受以下结构。相册 ID 需替换为该用户的真实相册 ID：

```json
{
  "all_albums": false,
  "permissions": [],
  "albums": [
    {"album_id": "album-id", "permissions": ["read", "upload", "edit", "share"]}
  ]
}
```

| 字段 | 规则 |
| --- | --- |
| all_albums=true | 使用顶层 permissions，albums 为空；包括以后新建的本人相册 |
| all_albums=false | 使用 albums 的独立授权，顶层 permissions 为空；最多 200 个本人相册 |
| permissions | 非空权限数组；未知权限或其他用户相册返回 400 |

| 权限 | 行为 |
| --- | --- |
| read | 读取授权图库、统计、原图和缩略图 |
| upload | 上传至授权相册，包括省略 album_id 时的默认相册 |
| edit | 重命名、移动图片；移动还需目标相册的 upload |
| delete | 删除图片 |
| albums | 管理相册；指定相册模式不能新建相册，连同图片删除还需 delete |
| share | 管理授权相册中本人图片的分享 |

创建时省略 authorization，默认为全部本人相册仅 upload；修改时必须提供。撤销 Token 不会撤销既有分享。脚本上传示例：

```sh
curl -H "Authorization: Bearer $QPIX_TOKEN" \
  -F "file=@photo.png" \
  -F "compression=preserve" \
  -F "quality=82" \
  https://qpix.example.com/api/v1/images
```

### Token 管理接口

以下接口需要登录会话。默认管理本人 Token，管理员列表可加 `scope=all` 和 `user_id`。

| 方法与路径 | 请求或行为 |
| --- | --- |
| `GET /tokens` | 列表，支持 q、status、sort、order 和分页参数 |
| `POST /tokens` | name、expires_at、authorization；仅创建时返回完整 token |
| `PUT /tokens/{id}` | 修改 name、expires_at、authorization；已撤销不可修改 |
| `DELETE /tokens/{id}` | 撤销并保留记录 |
| `DELETE /tokens/{id}/record` | 永久删除已撤销记录；未撤销返回 409 |

列表返回前缀及元数据，包括 authorization、last_used_at、revoked_at、创建人信息，不返回明文或哈希。持续使用时 last_used_at 最多约每分钟更新一次，权限、撤销及到期仍逐请求校验。

## 列表分页

图片、分享和 Token 列表默认只返回本人记录。管理员会话可用 `scope=all` 查看全站，并通过 `user_id` 筛选账号。

| 参数 | 说明 |
| --- | --- |
| q | 搜索名称，保留子串匹配语义 |
| limit | 每页最多 100 条；图片默认 36，分享和 Token 默认 20 |
| order | asc 或 desc，默认 desc；相同排序值按 ID 降序 |
| status | 分享和 Token 支持 active、expired、revoked |
| pagination=cursor | 使用游标分页 |
| cursor | 上一页返回的 next_cursor；首次省略 |

游标模式首次返回 items、total、page、limit、has_more、next_cursor。下一页原样传回 next_cursor，并保持筛选、排序和每页数量不变。后续页 total 为 null，page 固定为 1；客户端按 has_more 判断结束。游标无效返回 400，每次请求仍重新检查权限。

| 列表 | 游标排序 |
| --- | --- |
| 图片 | sort=created_at、name、size |
| 分享 | 创建时间，使用 order 指定方向 |
| Token | sort=created_at、name |

默认按创建时间排序。未设置 `pagination=cursor` 时，使用 page、limit 和逐页计数。Token 页码模式还支持 expires_at、last_used_at 排序。

## 图片与上传

| 方法与路径 | 请求或返回 |
| --- | --- |
| `GET /images` | 分页图片列表；可加 album_id，`unfiled` 表示历史未归类图片 |
| `POST /images` | 单张 multipart 上传，成功返回 201 和图片信息 |
| `PATCH /images/{id}` | name、album_id；album_id 为空字符串时转入上传者默认相册 |
| `POST /images/batch-move` | ids、album_id；1～100 张，同一上传者，目标为该账号相册 |
| `POST /images/batch-delete` | ids，1～100 项；返回 deleted 数量和 failed ID 数组 |
| `GET /upload-options` | 当前账号有效单张限制、容量、默认处理设置、可用存储和 formats 扩展名数组 |
| `GET /stats` | images、bytes、albums、today、pending_deletions；today 按 UTC 统计 |

### 上传字段

| 字段 | 说明 |
| --- | --- |
| file | 必填，每个请求一张图片 |
| album_id | 省略时使用上传者当前默认相册，Token 也需具备该相册 upload 权限 |
| storage_id | 省略时使用默认存储 |
| compression | default、none、preserve、webp；省略或 default 使用后台默认值 |
| quality | 1～100 的整数，省略使用默认画质 |
| random_name | true／false，省略为 true；使用随机 ID 作为显示名称，false 保留原文件名 |

none 保留原文件，preserve 保留原格式压缩，webp 转为有损 WebP。画质表示编码质量，透明通道保留；只有结果更小时才采用。空字符串、小数、重复或非法画质字段返回 400。random_name 只接受单个 true 或 false，其余值返回 400；存储键和直链始终使用随机 ID。格式及元数据处理见 [压缩细节](usage.md#压缩细节)。

接收阶段最多 8 个全局在途请求、每账号 2 个，图片处理全局并发 2 个。多图客户端应拆分请求，建议最多两个并发。重复上传产生独立记录，不自动去重。

### 图片字段

每张图片包含 id、name、album_id、storage_id、format、size、original_size、optimized、compression_status、width、height、created_at、url、preview_url、thumbnail_url 和 can_manage。owner_id、owner_name、owner_role 仅管理员或上传者可见。

format 和链接后缀表示实际保存格式，name 是显示名称。未配置图片域名时，URL 为相对路径，客户端可补全当前站点地址。预览和缩略图使用返回的 preview_url、thumbnail_url，不自行拼接后缀；无法生成时可为空。分享返回的这些地址使用 `/si/`，同样校验密码和期限。

| compression_status | 含义 |
| --- | --- |
| original | 保留原文件 |
| optimized | 采用压缩结果 |
| unsupported | 文件不重新编码 |
| metadata | 为保留精度或元数据而跳过 |
| pixel_limit | 超过 400 万压缩像素上限 |
| not_smaller | 结果未变小 |
| failed | 编码失败，保留原文件 |

删除先标记 deleting，使直链及关联分享失效，再清理文件和记录。失败保留待删除记录，启动及每 10 分钟重试，也可用原 ID 再次调用批量删除。容量不足返回 409，超过单张或像素限制返回 413。

## 相册

| 方法与路径 | 请求或行为 |
| --- | --- |
| `GET /albums` | 本人相册及图片计数；管理员可加 scope=all、user_id |
| `POST /albums` | name |
| `PATCH /albums/{id}` | name |
| `PUT /albums/{id}/default` | 将该账号拥有的相册设为默认 |
| `PUT /albums/{id}/access` | guest_visible、referer_mode、referers、allow_empty_referer |
| `DELETE /albums/{id}` | action=move 和 target_id，或 action=delete；默认相册返回 409 |

相册返回 guest_visible、can_upload、owner_id、owner_name、owner_role、is_default 及 Referer 配置。referer_mode 为 inherit、custom 或 unrestricted。referers 为域名数组。

guest_visible 仅允许访客通过已知图片链接访问，不提供公开目录或成员授权。普通用户只管理本人相册，管理员可管理全部。转移图片不改变上传人和容量归属。

## 单图分享

| 方法与路径 | 请求或返回 |
| --- | --- |
| `GET /shares` | 本人分享列表，支持分页、q、status、order；管理员可加 scope=all、user_id |
| `POST /shares` | kind=image、target_id、可选 password、expires_at |
| `DELETE /shares/{id}` | 撤销，保留记录 |
| `DELETE /shares/{id}/record` | 永久删除已撤销记录及解锁会话；仅到期未撤销返回 409 |
| `GET /public-shares/{token}` | 未解锁返回 locked=true；解锁后返回 title、items、total、page、limit（36）、expires_at |
| `POST /public-shares/{token}/unlock` | password；成功设置 HttpOnly Cookie，密码错误返回 403 |

分享列表包含 url、has_password、expires_at、revoked_at、title 及创建人信息。本人管理本人分享，管理员会话可针对所有图片创建和管理；Token 需目标相册 share 权限。

## 有效期

Token 和分享的 expires_at 接受 RFC3339 时间，保存为 UTC，必须晚于当前时刻；省略或 null 表示永久。API 接受绝对时刻，网页负责把常用期限换算为时刻。

网页 Token 默认 90 天，分享默认永久。分享 password 可空；非空密码最多 72 字节。

## 图片访问

| 路径 | 说明 |
| --- | --- |
| `GET/HEAD /i/{id}.{format}` | 保存的图片 |
| `GET/HEAD /i/{id}-thumb.jpg` 或 `.png` | 缩略图；小图可能复用原图 |
| `GET/HEAD /i/{id}-preview.jpg` 或 `.png` | 独立预览，最长边 1920 px；仅部分格式生成 |
| `GET/HEAD /si/{token}/{file}` | 分享目标图片，校验分享解锁状态及期限 |
| `/s/{token}` | 前端分享页 |

访客直链需相册允许访客访问。私有直链仅限所有者、管理员会话或具备相册 read 权限的个人 Token；历史未归类图片仅上传者或管理员可见。不可访问时返回 404。

返回真实 Content-Type、nosniff 和 ETag。无有效网络限制、未携带 Token 的公开直链使用 `private, no-cache, must-revalidate`；其他图片和分享使用 `private, no-store`。权限校验先于 304，ETag 匹配时不读取存储对象。

预览图和缩略图沿用原图的权限、Referer 和删除状态。SVG 原文件额外返回 `Content-Disposition: attachment` 及限制执行的 CSP；反向代理需保留这些响应头。

分享图片独立检查密码 Cookie、目标范围、到期、撤销和删除状态，不应用直链的 IP／Referer 规则。无效分享返回 404，已下载副本不受撤销控制。

## 系统管理

本节接口只允许管理员登录会话，并要求修改请求提交 CSRF。

### 用户

| 方法与路径 | 行为 |
| --- | --- |
| `GET /users` | 账号列表及 used_bytes |
| `POST /users` | 创建账号，返回 200 和 id |
| `PUT /users/{id}` | 更新完整账号配置；密码两项均留空时保留原值 |
| `DELETE /users/{id}` | 删除其他账号；有图片或分享、当前账号或最后一个启用管理员时返回 409 |

账号字段为 username、role（user／admin，创建省略默认为 user）、enabled、quota_bytes、max_file_bytes、password、confirm_password。密码非空、最多 72 字节且确认一致。容量和大小单位为字节，0 分别表示不限累计容量、跟随服务器单张上限。容量口径见 [使用指南](usage.md#容量与上传限制)。

### 站点设置

`GET /settings` 返回站点与图片地址、Cookie 策略、代理、上传限制、数据库、默认存储、处理配置及版本信息。`PATCH /settings` 部分更新以下字段：

| 字段 | 约束 |
| --- | --- |
| app_url、public_url | HTTP(S) 源地址，不含路径、凭据或查询参数；空值使用当前站点／请求地址 |
| trusted_proxies | 最多 200 项 IP／CIDR 数组，JSON 最多 2048 字节 |
| secure_cookie | 启用时，当前请求须为 TLS，或来自可信代理且 X-Forwarded-Proto=https |
| max_upload_bytes | 1～100 MiB 的整数字节数 |
| max_pixels | 100 万～1 亿的整数 |

设置原子保存并持久化，无效请求不部分覆盖。更改 Cookie 策略清理全部登录和分享解锁会话，返回 reauthenticate=true，个人 Token 保留。上传有效单张上限取服务器和用户限制中的较小者；网页与 Token 使用同一校验。

### 存储

| 方法与路径 | 行为 |
| --- | --- |
| `GET /storages` | 配置列表，含 is_default、has_credentials、images，不含凭据 |
| `POST /storages` | 新增配置 |
| `PUT /storages/{id}` | 更新完整配置；凭据两项均留空时保留原值 |
| `DELETE /storages/{id}` | 删除未引用、非默认且非初始 local 的配置，否则 409 |
| `POST /storages/{id}/default` | 将已启用配置设为默认 |
| `POST /storages/{id}/test` | 写入、读取、删除探测对象；失败返回 502 |

请求字段为 name、kind（local／s3）、enabled、root、endpoint、region、bucket、prefix、path_style、access_key、secret_key。root 用于本地，后续字段用于 S3。不接受 id、images 等只读字段；存在图片引用时不可更改存储位置或类型。连接探测最多 30 秒，补偿清理最多 10 秒。

### 图片处理与访问控制

| 方法与路径 | 请求或返回 |
| --- | --- |
| `GET /processing` | mode、quality |
| `PUT /processing` | mode=none／preserve／webp，quality=1～100；仅影响新上传 |
| `GET /access` | items（三组规则）、requester_ip、referer_host、bypassed |
| `PUT /access/{scope}` | 保存 admin、upload 或 images 规则 |

规则字段为 scope、ip_enabled、ips、referer_enabled、referers、allow_empty_referer。scope 可省略，提供时需与路径一致。两类名单各最多 200 项，管理规则拒绝会阻止当前请求的设置。

upload 只支持 IP；启用 Referer 或提交非空 Referer 名单返回 400。images 只约束 `/i/`；分享与普通用户 API 使用各自规则。相册 inherit 使用全局图片直链规则。完整语义见 [访问控制](deployment.md#访问控制)。
