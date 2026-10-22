# API

所有管理接口位于 `/api/v1`，成功返回 JSON，失败返回 `{"error":"说明"}`。未登录返回 401，权限校验失败返回 403，参数错误返回 400，超过限流返回 429，超过上传大小或像素限制返回 413，不支持的图片返回 415。

## 浏览器登录

`POST /auth/login` 接受 `{"username":"admin","password":"..."}`，返回 `user` 与 `csrf_token`，同时设置 HttpOnly、SameSite=Strict 的会话 Cookie。会话有效期为 7 天。前端将 CSRF 值保存在内存，所有修改请求通过 `X-CSRF-Token` 提交；刷新页面用 `GET /auth/me` 恢复。

管理员和普通用户统一使用 `/auth/login`、`/auth/me`、`/auth/logout`、`/auth/password`，无需提交账号类型。后端从数据库识别角色；管理员登录及会话操作使用 admin 网络规则，普通账号不应用图片直链规则。旧 `/member/auth/*` 路由已移除。User 返回 role、enabled、quota_bytes、max_file_bytes、default_album_id、created_at，不返回密码哈希。所有系统管理接口仅管理员会话可用；图库和 Token 接口也支持普通账号，但按权限过滤。

服务不开放 CORS。开发使用 Vite 代理，生产使用同源反向代理；若在后台配置站点地址，浏览器写请求的 Origin 必须与其一致。客户端不应提交或伪造 Origin。登录每个来源 IP 每 15 分钟最多 15 次；只有配置可信代理后才使用对应 X-Forwarded-For，详见 [访问控制](access.md)。

## 接口清单

| 方法与路径 | 请求 / 行为 |
| --- | --- |
| GET /health | 公共健康检查，检查数据库连接 |
| POST /auth/login | 登录 |
| GET /auth/me | 当前账号和 CSRF 值 |
| POST /auth/logout | 退出当前会话 |
| PUT /auth/password | current_password、new_password；成功后撤销全部登录会话，保留 Token |
| GET /images | page、limit（最多 100）、q、album_id、sort（created_at/name/size）、order（asc/desc）；默认本人，管理员可加 scope=all、user_id |
| POST /images | multipart/form-data，每次一张，字段 file；可选 album_id、storage_id、compression（default / none / preserve / webp）、quality（1–100 整数） |
| PATCH /images/{id} | name、album_id；album_id 传空字符串表示转入上传者默认相册，不能转入其他账号相册 |
| POST /images/batch-delete | ids 数组，1–100 项；返回 deleted 数量和 failed ID 数组 |
| POST /images/batch-move | ids 数组（1–100 项）、album_id；图片须属于同一上传者，目标为该账号相册 |
| GET /albums | 本人相册与图片计数；管理员可加 scope=all、user_id |
| POST /albums | name |
| PATCH /albums/{id} | name |
| DELETE /albums/{id} | 请求 action=move 与 target_id，或 action=delete；默认相册返回 409 |
| GET /tokens | 分页列出元数据与前缀，支持 q、status、order、page、limit；管理员可加 scope=all、user_id，不返回 Token 明文或哈希 |
| POST /tokens | 任意登录账号创建本人的 Token：name、expires_at、authorization；只在创建时返回完整 token |
| PUT /tokens/{id} | 本人或管理员会话修改 name、expires_at、authorization；已撤销 Token 不可修改 |
| DELETE /tokens/{id} | 本人撤销；管理员可撤销任意 Token，保留 revoked_at 记录 |
| DELETE /tokens/{id}/record | 本人或管理员会话永久删除已撤销的 Token 记录；未撤销返回 409，无权或不存在返回 404 |
| GET /stats | 本人 images、bytes、albums、today、pending_deletions；today 按 UTC 统计，管理员可加 scope=all |
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
| POST /users | 管理员创建账号，role 为 user/admin，返回 200 与 id |
| PUT /users/{id} | 管理员更新 role、enabled、quota_bytes、max_file_bytes；password 和 confirm_password 均为空保留密码 |
| PUT /albums/{id}/access | guest_visible、referer_mode（inherit/custom/unrestricted）、referers（域名数组）、allow_empty_referer；本人或管理员可修改 |
| PUT /albums/{id}/default | 将该账号拥有的相册设为默认 |
| GET /upload-options | 登录账号的有效单张限制、容量用量、处理设置、可用存储名称与 ID |
| GET /shares | 分页列出本人创建的单图分享，包含 url、has_password、expires_at、revoked_at、title；支持 q、status、order、page、limit，管理员可加 scope=all、user_id |
| POST /shares | 本人创建单图分享，管理员可针对所有图片创建：kind=image、target_id、password（可选）、expires_at（RFC3339，可选/null） |
| DELETE /shares/{id} | 本人撤销；管理员可撤销所有分享，返回 ok |
| GET /public-shares/{token} | 未解锁返回 locked=true；解锁后返回 title、items、total、page、limit（36）、expires_at |
| POST /public-shares/{token}/unlock | password；验证成功设置 HttpOnly Cookie，分享密码错误 403 |

图片、分享和 Token 列表统一返回 `items`、`total`、`page`、`limit`。每张图片包含 id、name、album_id、format、size、original_size、optimized、compression_status、storage_id、width、height、created_at、url、thumbnail_url、can_manage。owner_id、owner_name、owner_role 仅管理员或上传者可见。分享和 Token 列表包含创建人字段；Token 额外返回 authorization、last_used_at、revoked_at。format / URL 后缀表示实际保存格式，name 保留显示名称。没有配置图片域名时 URL 为相对路径，客户端可用当前站点地址补全。

账号创建字段：username、role（省略默认为 user）、enabled、quota_bytes、max_file_bytes、password、confirm_password。密码非空、最多 72 字节且确认一致，不强制最短长度或复杂度；更新使用完整配置，密码均留空时保留原值。初始化及修改本人密码也采用相同长度政策，分享密码可留空表示无需密码。容量与单张大小为字节，0 分别表示不限累计容量、跟随服务器单张上限。容量不足返回 409，超过单张限制返回 413。详细计费范围和预占行为见 [permissions.md](permissions.md)。

相册返回 guest_visible、can_upload、owner_id、owner_name、owner_role、is_default、referer_mode、referers、allow_empty_referer。无成员授权。guest_visible 只允许访客凭已知图片链接访问，不提供公开图库。网页或 Token 上传省略 album_id 时归入上传者当前默认相册。删除相册必须明确转移图片或连同图片删除；转移不改变上传人及容量归属。

服务器上传上限在管理员控制面板保存后立即对新请求生效，使用 settings 表的 max_upload_bytes 持久化，重启时从数据库恢复，不再读取旧上传上限环境变量。未提供可更新值或上传上限越界时拒绝；无效请求不会部分覆盖其他字段。GET /settings 返回服务器上限，GET /upload-options 返回当前账号与服务器两者中更小的有效上限。网页上传和上传 Token 使用同一校验，已经保存的图片不受调低上限影响。

compression_status：original（未压缩）、optimized（采用压缩结果）、unsupported（不重新编码）、metadata（为保留精度/元数据而跳过）、pixel_limit（超过 400 万像素）、not_smaller（结果未变小）、failed（编码失败，保留原文件）。

compression 为 none 时保留原文件；preserve 保留 JPEG / PNG / 静态 WebP 格式压缩；webp 转为有损 WebP。省略或 default 使用全局默认方式，省略 quality 使用全局默认画质；显式提交的方式和画质优先，非法画质（含空字符串、小数、重复字段）返回 400。quality 表示画质，不是体积缩减百分比。PNG 量化 RGB 颜色并逐像素保留 alpha，画质 100 不减少色阶；WebP 有损编码保留透明通道。网页队列入队时固定参数，重试保持原参数。旧开发配置的 smart 默认模式在启动时解释为 webp，新请求不再接受 smart。

存储请求字段：name、kind（local / s3）、enabled、root（本地）、endpoint、region、bucket、prefix、path_style、access_key、secret_key（S3）。更新提交完整配置；不接受只读 id、images 等字段。连接探测最多 30 秒，之后最多 10 秒补偿清理。

访问规则字段：scope（可省略，提供时须与路径一致）、ip_enabled、ips（字符串数组）、referer_enabled、referers（域名数组）、allow_empty_referer。每类最多 200 项；管理规则拒绝阻止当前请求的设置。三组独立，上传 Token 同样遵守 upload 规则。用户资源接口按 Token 的操作与相册范围授权；系统管理、账号和 Token 管理接口只接受登录会话和 CSRF。

## 用户 API Token

管理员与普通用户均可创建和使用个人 Token，仅允许操作本人授权相册，不继承管理员的全站权限。个人空间的“我的 Token”管理本人凭据；系统管理的“Token 管理”通过登录会话管理全体用户凭据。通过 `Authorization: Bearer <token>` 提交，不放入 URL；Token 请求不需要 Cookie 或 CSRF，但仍检查 Origin、账号状态及相册操作权限。上传适用 upload IP 规则，读取原图／缩略图直链适用 images 及相册 Referer 规则；普通 API 操作不应用图片规则。无效 Bearer 凭据不会回退到请求附带的有效 Cookie。

`expires_at` 为 RFC3339，必须晚于当前时刻，统一存储 UTC；省略/null 为永久。界面默认 90 天，可选固定期限、自定义日期或永久。撤销、到期或停用账号后失效，修改密码保留个人 Token。此前迁移已停用的旧管理员 Token 保持撤销状态，可创建新的个人 Token；旧普通用户 Token 保持全部本人相册仅上传。

创建/编辑权限字段示例（占位 ID 须替换为该用户的真实相册 ID）：

```json
{
  "name": "博客工具",
  "expires_at": null,
  "authorization": {
    "all_albums": false,
    "permissions": [],
    "albums": [
      {"album_id": "album-id", "permissions": ["read", "upload", "edit", "share"]}
    ]
  }
}
```

`all_albums=true` 使用顶层 permissions，albums 必须为空；`false` 使用 1–200 个相册的独立 permissions，顶层 permissions 必须为空。权限值为 read（读取图库、统计、原图和缩略图）、upload、edit（重命名/移动图片）、delete（删除图片）、albums（相册管理）、share（单图分享管理）。未知权限、空权限或其他用户相册返回 400。创建省略 authorization 时兼容为全部本人相册仅 upload；修改必须提供 authorization。

Token 不接受 `scope=all`。读取列表与统计在数据库查询阶段按授权相册过滤。上传目标需 upload，省略 album_id 时默认相册也必须获授权；移动要求源 edit 与目标 upload；相册管理不能代替图片删除权限。没有所需操作权限返回 403；无法发现的具体目标通常返回 404。Token 不能访问任何 `/tokens`、`/auth` 或系统管理接口。

上传每账号每分钟最多 120 请求，同时处理两张图片；429 可按 Retry-After 重试。更多规则见 [权限说明](permissions.md)。

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


- `DELETE /shares/{id}/record`：永久删除已撤销分享及解锁会话；有效或仅到期但未撤销时 409，越权或不存在 404。个人 Token 需具有目标相册 share 权限。
- `DELETE /users/{id}`：仅管理员会话；存在图片或分享记录、删除当前账号或最后一个启用管理员时 409。
- `PATCH /settings` 支持部分更新 public_url、app_url（HTTP(S) 源地址，空值按请求域名）、trusted_proxies（最多 200 项 IP/CIDR，JSON 最多 2048 字节）、secure_cookie、max_upload_bytes（1–100 MiB）、max_pixels（100 万–1 亿）。原子保存并即时生效；格式无效返回 400。
- 启用 secure_cookie 需当前 TLS 连接，或来自可信代理且 X-Forwarded-Proto 为 https。变更 Cookie 策略清理全部登录和分享会话，返回 reauthenticate=true，客户端重新登录；Token 保留。
