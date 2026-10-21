# API

所有管理接口位于 `/api/v1`，成功返回 JSON，失败返回 `{"error":"说明"}`。未登录返回 401，权限校验失败返回 403，参数错误返回 400，超过限流返回 429，超过上传大小或像素限制返回 413，不支持的图片返回 415。

## 浏览器登录

`POST /auth/login` 接受 `{"username":"admin","password":"..."}`，返回 `user` 与 `csrf_token`，同时设置 HttpOnly、SameSite=Strict 的会话 Cookie。会话有效期为 7 天。前端将 CSRF 值保存在内存，所有修改请求通过 `X-CSRF-Token` 提交；刷新页面用 `GET /auth/me` 恢复。

管理员和普通用户统一使用 `/auth/login`、`/auth/me`、`/auth/logout`、`/auth/password`，无需提交账号类型。后端从数据库识别角色，验证账号密码后应用对应网络规则，再创建会话：管理员使用 admin，普通用户使用 images；会话恢复、退出与改密同样按当前角色检查。旧 `/member/auth/*` 路由已移除。User 返回 role、enabled、quota_bytes、max_file_bytes，不返回密码哈希。所有系统管理接口仅管理员可用；图库和 Token 接口也支持普通账号，但按权限过滤。

服务不开放 CORS。开发使用 Vite 代理，生产使用同源反向代理；若配置 QPIX_APP_URL，浏览器写请求的 Origin 必须与其一致。客户端不应提交或伪造 Origin。登录每个来源 IP 每 15 分钟最多 15 次；只有配置可信代理后才使用对应 X-Forwarded-For，详见 [访问控制](access.md)。

## 接口清单

| 方法与路径 | 请求 / 行为 |
| --- | --- |
| GET /health | 公共健康检查，检查数据库连接 |
| POST /auth/login | 登录 |
| GET /auth/me | 当前账号和 CSRF 值 |
| POST /auth/logout | 退出当前会话 |
| PUT /auth/password | current_password、new_password；成功后撤销全部会话和 Token |
| GET /images | page、limit（最多 100）、q、album_id、mine=true；管理员可加 user_id。unfiled 表示未分类 |
| POST /images | multipart/form-data，每次一张，字段 file；可选 album_id、storage_id、compression（default / none / preserve / webp）、quality（1–100 整数） |
| PATCH /images/{id} | name、album_id；album_id 传空字符串表示移回未分类 |
| POST /images/batch-delete | ids 数组，1–100 项；返回 deleted 数量和 failed ID 数组 |
| GET /albums | 返回相册与图片计数 |
| POST /albums | name |
| PATCH /albums/{id} | name |
| DELETE /albums/{id} | 删除相册，保留图片并设为未分类 |
| GET /tokens | 列出元数据与前缀，不返回 Token 明文或哈希 |
| POST /tokens | name、expires_at（RFC3339，自定义到期时刻；省略/null 表示永久）；只在创建时返回完整 token |
| DELETE /tokens/{id} | 撤销 Token |
| GET /stats | images、bytes、albums、unfiled、today、pending_deletions；today 按 UTC 统计 |
| GET /settings | 图片域名、上传限制、数据库与存储类型 |
| PATCH /settings | 可单独或同时提交 public_url、max_upload_bytes；域名空字符串使用当前站点，上传上限为 1–100 MiB 的整数字节数 |
| GET /storages | 存储列表，包含 is_default、has_credentials、images，不含凭据 |
| POST /storages | 新增配置，字段见下方 |
| PUT /storages/{id} | 更新配置，凭据均留空保留原值；位置已被图片引用时不可改 |
| DELETE /storages/{id} | 删除未引用、非默认且非初始 local 的配置，否则 409 |
| POST /storages/{id}/default | 设为默认，配置必须启用 |
| POST /storages/{id}/test | 写入、读取、删除探测对象，失败返回 502 |
| GET /processing | mode（none / preserve / webp）、quality（1–100） |
| PUT /processing | 保存 mode、quality，仅影响新上传 |
| GET /access | items（三组规则）、requester_ip、referer_host、bypassed |
| PUT /access/{scope} | 保存 admin / upload / images 的独立规则 |
| GET /users | 管理员列出账号及 used_bytes |
| POST /users | 管理员创建普通用户，返回 200 与 id |
| PUT /users/{id} | 管理员更新 enabled、quota_bytes、max_file_bytes；password 和 confirm_password 均为空保留密码 |
| PUT /albums/{id}/access | guest_visible、member_ids（账号 ID 数组，最多 200 项） |
| GET /upload-options | 登录账号的有效单张限制、容量用量、处理设置、可用存储名称与 ID |
| GET /shares | 管理员列出分享，包含 url、has_password、expires_at、revoked_at、title |
| POST /shares | 管理员创建：kind=image/album、target_id、password（可选）、expires_at（RFC3339，可选/null） |
| DELETE /shares/{id} | 管理员撤销分享，返回 ok |
| GET /public-shares/{token} | 未解锁返回 locked=true；解锁后返回 title、items、total、page、limit（36）、expires_at |
| POST /public-shares/{token}/unlock | password；验证成功设置 HttpOnly Cookie，分享密码错误 403 |

图片列表返回 `items`、`total`、`page`、`limit`。每张图片包含 id、name、album_id、format、size、original_size、optimized、compression_status、storage_id、width、height、created_at、url、thumbnail_url、can_manage。owner_id 仅管理员或上传者可见。format / URL 后缀表示实际保存格式，name 保留显示名称。没有配置图片域名时 URL 为相对路径，客户端可用当前站点地址补全。

账号创建字段：username、enabled、quota_bytes、max_file_bytes、password、confirm_password。密码非空、最多 72 字节且确认一致，不强制最短长度或复杂度；更新使用完整配置，密码均留空时保留原值。初始化及修改本人密码也采用相同长度政策，分享密码可留空表示无需密码。容量与单张大小为字节，0 分别表示不限累计容量、跟随服务器单张上限。容量不足返回 409，超过单张限制返回 413。详细计费范围和预占行为见 [permissions.md](permissions.md)。

相册返回 guest_visible、can_upload，管理员额外获取 member_ids。guest_visible 允许未登录访客凭图片链接访问，不提供公开相册或图片列表接口。图库与相册列表均需登录。删除相册将图片转入上传者的私有未分类空间，图片归属不变。

服务器上传上限在管理员控制面板保存后立即对新请求生效，使用 settings 表的 max_upload_bytes 持久化，重启时优先于 QPIX_MAX_UPLOAD_MB。未提供可更新值或上传上限越界时拒绝；无效请求不会部分覆盖其他字段。GET /settings 返回服务器上限，GET /upload-options 返回当前账号与服务器两者中更小的有效上限。网页上传和上传 Token 使用同一校验，已经保存的图片不受调低上限影响。

compression_status：original（未压缩）、optimized（采用压缩结果）、unsupported（不重新编码）、metadata（为保留精度/元数据而跳过）、pixel_limit（超过 400 万像素）、not_smaller（结果未变小）、failed（编码失败，保留原文件）。

compression 为 none 时保留原文件；preserve 保留 JPEG / PNG / 静态 WebP 格式压缩；webp 转为有损 WebP。省略或 default 使用全局默认方式，省略 quality 使用全局默认画质；显式提交的方式和画质优先，非法画质（含空字符串、小数、重复字段）返回 400。quality 表示画质，不是体积缩减百分比。PNG 量化 RGB 颜色并逐像素保留 alpha，画质 100 不减少色阶；WebP 有损编码保留透明通道。网页队列入队时固定参数，重试保持原参数。旧开发配置的 smart 默认模式在启动时解释为 webp，新请求不再接受 smart。

存储请求字段：name、kind（local / s3）、enabled、root（本地）、endpoint、region、bucket、prefix、path_style、access_key、secret_key（S3）。更新提交完整配置；不接受只读 id、images 等字段。连接探测最多 30 秒，之后最多 10 秒补偿清理。

访问规则字段：scope（可省略，提供时须与路径一致）、ip_enabled、ips（字符串数组）、referer_enabled、referers（域名数组）、allow_empty_referer。每类最多 200 项；管理规则拒绝阻止当前请求的设置。三组独立，上传 Token 同样遵守 upload 规则。所有新增管理接口只接受登录会话和 CSRF，不接受上传 Token。

## 上传 Token

Token 只接受 `POST /api/v1/images`，其他管理接口均不接受。上传接口每账号每分钟最多 120 个请求，同时处理最多两张图片；429 可按 Retry-After 延迟重试。Token 请求不需要 Cookie 或 CSRF。

创建时使用 `expires_at` 指定到期时间，必须晚于当前时刻，统一存储 UTC；省略或传 null 为永久有效。界面默认 90 天，可选常用期限、自定义日期时间或永久。列表的 expires_at 为 null 时表示永久，已有的固定期限保持不变。永久 Token 同样受账号状态、相册授权、上传规则和配额限制，撤销或修改账号密码后立即失效。旧的 days 请求字段已移除。

```bash
curl -H "Authorization: Bearer $QPIX_TOKEN" \
  -F "file=@photo.png" \
  https://qpix.example.com/api/v1/images
```

上传成功返回 201 和图片信息。多图上传客户端应拆分请求，建议最多两个并发；重复上传会产生独立记录，不做自动去重。

## 图片访问

`GET /i/{id}.{format}` 访问保存的图片，`GET /i/{id}-thumb.png` 访问缩略图，也支持 HEAD。按登录身份检查相册授权，未分类只允许上传者和管理员，无权限返回 404。返回真实 Content-Type、nosniff、ETag，始终 private, no-store，访问校验先于 304。

分享资源使用 `GET/HEAD /si/{token}/{file}`，仅可读取分享目标中的图片。每次检查密码解锁 Cookie、撤销状态与期限；不能用其他图片 ID 绕过目标范围。分享不存在、到期、撤销或目标删除返回 404；未解锁不会返回图片列表或内容。口令验证按来源 IP 每 15 分钟最多 15 次。分享页为前端 `/s/{token}`；令牌不在查询参数中传播。已下载副本不受撤销控制。

删除先将记录设为 deleting，停止外链访问，再清理文件和记录。文件清理失败时保留待删除记录，服务启动及每 10 分钟重试；同样可以用原 ID 再次调用批量删除。
