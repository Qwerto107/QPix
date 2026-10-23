# QPix 整改复核报告

- 复核对象：工作区未提交改动（基线 `a6a6dac`，27 个文件 +1116/−393，另有 8 个新文件）
- 对照文档：[remediation-report.md](remediation-report.md)、`docs/deployment.md`、`docs/api.md`
- 复核方式：逐项阅读代码和文档。复核开始时执行过一次 `go vet` 和 `go test -tags=nodynamic ./...`，结果全部通过；之后按要求不再运行测试。PG/MySQL 实例、压测、EXPLAIN 均未执行
- 范围说明：S5、S6、S7、S9、S10 属于需求，不在复核范围内

## 结论

整改说明与代码基本一致，原报告中的高风险项均已落实。复核发现 1 个中等级别的功能缺陷（N1，仅影响 SQLite + 非 UTC 时区）和 1 个中等级别的残留 DoS（N2），其余为低风险或待验证项。

| 编号 | 等级 | 问题 | 类型 |
|---|---|---|---|
| N1 | 中 | SQLite 在非 UTC 时区下，游标分页会漏掉记录 | 新引入，正确性 |
| N2 | 中 | 全局策略锁内执行 bcrypt，普通用户可串行化全站写操作 | S1 残留 |
| N3 | 低 | 登录限流表可被大量 IPv6 /64 填满，新来源 IP 被拒绝 | S2 残留 |
| N4 | 低 | 每张图片的上传临时文件写在 tmpfs，并发大文件可能写满，且报错文案有误导 | 新引入，配置 |
| N5 | 低 | 首页仍然执行 COUNT；统计接口的“今日”同样受 N1 的时区问题影响 | 残留 |
| N6 | 低 | 写请求重复鉴权；全局锁抵消了部分 D7 并行收益 | 性能，提示 |
| N7 | 待验证 | MySQL 预编译缓存与迁移 DDL 共存；PG trgm 索引是否实际命中 | 未实测 |
| N8 | 低 | API 文档的列表返回字段描述未同步 | 文档 |

## 一、整改项核对

| 编号 | 核对结果 | 依据 |
|---|---|---|
| S1 / P1 | 属实，有残留（N2） | 读请求、图片传输、分享网关、`Maintain` 不再持锁；写请求在锁外完成 64 KiB 请求体接收，加锁后重新鉴权并校验 Origin；`policyResponseWriter` 在写响应前释放锁；`deleteImages`、`deleteAlbum`、`testProfile` 在存储 I/O 前调用 `releasePolicy` |
| S2 | 属实，有残留（N3） | 登录、改密、分享解锁三张限流表独立；`limiterIP` 将 IPv6 聚合到 /64；表满只拒绝新 key，每分钟清理一次 |
| S3 | 属实 | `beginUpload` 在接收前限制全站 8 个、单账号 2 个；处理槽位在请求体收完之后才占用 |
| S4 | 属实 | 账号不存在时用 cost 10 的假哈希比对（与 `DefaultCost` 相同）；bcrypt 在锁外执行，加锁后重新读取并比较哈希、状态和角色 |
| S8 | 文档属实 | `deployment.md` 给出 Nginx 的 HSTS 和收紧版 CSP，并说明 `proxy_hide_header` 和 `add_header` 的继承规则 |
| S11 | 属实 | `main.go` 启动时输出 WARN |
| P2 | 属实 | 无网络限制的访客直链使用 `private, no-cache, must-revalidate`；ETag 判断在 `store.Open` 之前；打开失败时清除 ETag 和缓存头 |
| P3 | 属实 | 图片、相册、Token、分享、用户、存储列表都改为批量查询 / `GROUP BY`；`can_manage` 在内存中判断，Token 分支额外要求归属本人 |
| P4 | 属实，有残留（N4） | 上传只在锁内读取配置快照，并通过 `storageUploads` 引用计数阻止修改或删除在用的存储 |
| P5 / D6 | 属实 | 复用鉴权时读到的 `last_used_at`，超过 1 分钟才执行条件更新 |
| P6 | 属实 | 不透明图片生成 JPEG 缩略图，透明图片生成 PNG；`thumbnailFile` 和 Content-Type 按扩展名推断，兼容旧 PNG 缩略图 |
| P7 | 属实 | 接收落临时文件；原格式上传直接从临时文件写入存储；只有需要压缩时才 `ReadAll` |
| P8 | 属实 | NRGBA / RGBA 快速路径正确处理子图步长；RGBA 反预乘公式与标准库一致；只有 WebP 编码串行 |
| P9 | 属实 | 迁移 v7 回填 `used_bytes`；提交事务内增加用量；`removeImageRecord` 以 `RowsAffected` 防止重复扣减；提交时按压缩后的大小复核配额 |
| P10 | 属实 | 图片与相册一次 JOIN；Referer 规则缓存与当前规则内容比对，修改或删除相册时主动失效 |
| P11 | 属实 | 同 S2 的定时清理 |
| D1 / M1 | 属实 | 空闲连接数 = 最大连接数 = 10，空闲 5 分钟回收；MySQL 连接最长存活 3 分钟 |
| D2 | 属实 | `SkipDefaultTransaction: true`；多语句操作保留显式事务 |
| D3 / M5 / L6 | 属实 | 迁移 v8 建立四个复合索引，逐个检查后创建，可安全重试 |
| D4 / P12 | 代码属实，需实测（N7） | 每次启动检测 `pg_trgm`；应用不自行创建扩展；索引名和表名经过引用 |
| D5 | 属实，SQLite 有缺陷（N1） | 升序 / 降序条件构造正确；游标绑定排序方式，并校验长度和类型；后续页不再 COUNT 或 OFFSET |
| D7 / M6 | 属实 | PG / MySQL 按用户主键 `FOR UPDATE`；SQLite 保留进程锁；不对预占表做范围锁 |
| L3 | 属实 | 普通路径在代码内转为 `file:` URI，并对路径编码；`_pragma` 在每次建连时生效；Windows 盘符处理正确 |
| L4 / L5 | 属实 | 启动时 `PRAGMA optimize=0x10002`，维护任务执行 `optimize` 和 `wal_checkpoint(PASSIVE)` |
| M2 | 属实，需实测（N7） | 只有 MySQL 开启预编译缓存（100 条，5 分钟） |
| M7 | 属实 | 过期会话按主键每批 1000 条删除 |

## 二、新发现问题

### N1（中）SQLite 在非 UTC 时区下，游标分页会漏掉记录

位置：`app/pagination.go`（`cursorFor`、`parseCursor`），`database/sqlite.go`

成因：

1. GORM 默认的 `NowFunc` 是 `time.Now().Local()`，`created_at` 按进程所在时区写入
2. modernc 驱动未设置 `_time_format` 时，用 `t.String()` 把时间写成**文本**，例如 `2026-10-23 10:00:00.5 +0800 +08`
3. 游标把 `created_at` 转为 UTC（RFC3339Nano），查询时绑定为 `2026-10-23 02:00:00.5 +0000 UTC`
4. SQLite 对 TEXT 列按字典序比较 `(created_at, id) < (?, ?)`。两边时区不同，比较结果错误

后果：在 Windows 或 Linux 主机上直接运行二进制，并且系统时区不是 UTC（例如 Asia/Singapore）时，从第二页开始会**漏掉或重复记录**。默认 Docker 镜像基于 scratch、没有设置 TZ，运行在 UTC，不受影响。PostgreSQL（timestamptz）和 MySQL（DSN 中 `loc=UTC`）不受影响。

`TestCursorPagination` 在测试数据中显式使用 `time.UTC` 构造 `CreatedAt`，所以没有暴露这个问题。原有的 `stats` 接口中 `created_at >= 今日零点(UTC)` 也存在同样的问题（见 N5），属于原有缺陷。

修复方案（任选其一）：

- **推荐**：在 `gorm.Config` 中设置 `NowFunc: func() time.Time { return time.Now().UTC() }`，并在 SQLite DSN 中增加 `_time_format=sqlite`（固定格式、固定小数位，可以按字典序比较）。已有的本地时区数据需要在迁移中统一转换为 UTC 文本
- 或者：在 SQLite 上把游标值格式化成与存储完全相同的格式和时区（脆弱，不推荐）

### N2（中）全局策略锁内执行 bcrypt，普通用户可串行化全站写操作

位置：`app/app.go:236-250`、`app/shares.go:162`、`app/auth.go:113-121`、`app/users.go:90`

`secure()` 对所有非 GET 请求都在 `policyMu` 内调用处理函数。以下处理函数在锁内执行 bcrypt（cost 10，单次约 50–100 ms）：

| 接口 | 锁内 bcrypt | 调用方 | 限流 |
|---|---|---|---|
| `POST /shares`（带 password） | 1 次 Generate | 任意用户，或有分享权限的 Token | **无** |
| `PUT /auth/password` | 1 次 Compare + 1 次 Generate | 任意已登录用户 | 每账号 15 分钟 10 次 |
| `POST/PUT /users` | 1 次 Generate | 管理员 | — |

一个普通账号（或泄露的 Token）每秒发送十几个带密码的建分享请求，就能让 `policyMu` 持续被占用。全站所有写操作、登录的会话创建、上传提交都要排队。这比 S1 的匿名攻击门槛高（需要账号），但没有任何限流。

修复：bcrypt 移到加锁之前（处理函数可以在 `decode` 之后先计算哈希，或者 `secure()` 只对真正需要互斥的部分加锁）；同时给 `createShare` 增加按用户的限流。

### N3（低）登录限流表可被大量 IPv6 /64 填满

S2 修复后，表满时只拒绝**新来源**。攻击者拥有一个 IPv6 /48（很多 VPS 免费提供）就有 65536 个 /64，只需 10000 个就能填满 `loginLimits`。之后 15 分钟内，此前没有登录过的 IP 一律无法登录，包括换了网络的管理员。

缓解：IPv6 再按 /48 聚合一层，或者给已有会话的用户 / 可信网段保留独立名额。由于需要具备一定资源，等级为低。

### N4（低）上传临时文件写满 tmpfs，报错文案有误导

`ParseMultipartForm(1 << 20)` 会把超过 1 MiB 的文件写入 `os.TempDir()`。Compose 中 `/tmp` 是 256 MiB 的 tmpfs（同时占用容器内存）。全站 8 个并发接收，如果管理员把单张上限调到 100 MiB，最多需要 800 MiB，超过 tmpfs 容量。

写满后，`ParseMultipartForm` 返回的是磁盘错误，不是 `MaxBytesError`，用户看到的是“请使用 multipart/form-data 上传图片”（400），难以排查。部署文档已提示预留空间，但没有给出计算方式。

建议：文档写明 `tmpfs ≥ 8 × 单张上限`，或者把并发接收数与单张上限关联；非大小类错误改为返回 500 并记录日志。

### N5（低）残留项

- 游标模式的首页、旧页码模式的每一页仍然执行 COUNT。在 PG 上，大表的首页 COUNT 依然是线性扫描。这是有意的取舍，记录在案
- `stats` 接口的“今日上传”在 SQLite + 非 UTC 时区下统计错误，与 N1 同源，一起修复即可

### N6（低）性能提示

- 每个写请求都会**鉴权两次**（锁外一次、锁内一次），会话请求多 2 次查询，Token 请求多 2 次。这是为了安全换来的开销，可以接受；如果要优化，锁内只需要重新查询 `enabled`、会话或 Token 是否撤销
- D7 让不同用户的配额预占可以并行，但上传**提交**仍然在全局 `policyMu` 内，所以 PG 上的整体收益有限。要进一步提升，需要继续缩小 P1 的锁范围

### N7（待验证）需要真实实例确认

- **MySQL 预编译缓存**：`PrepareStmt` 在 `gorm.Open` 时就生效，`Migrate` 中的 `CREATE TABLE`、`ALTER TABLE`、`CREATE INDEX` 也会走预编译。MySQL 8 支持大部分 DDL 预编译，但个别语句或旧版本 / 兼容数据库（TiDB、MariaDB）可能报 `This command is not supported in the prepared statement protocol yet`。建议在真实 MySQL 上跑一次从零开始的迁移；或者迁移使用一个关闭 `PrepareStmt` 的会话（`db.Session(&gorm.Session{PrepareStmt: false})`）
- **PG trgm 索引**：查询条件 `LOWER(name) LIKE ? ESCAPE '!'` 与索引表达式 `lower(name)` 一致，理论上可以命中。但搜索词少于 3 个字符时，trigram 索引无法发挥作用，优化器可能退回全表扫描。需要在真实数据上执行 `EXPLAIN ANALYZE` 确认
- **D7 行锁**：`clause.Locking{Strength: "UPDATE"}` 与 `First` 组合生成的 SQL 是否带 `FOR UPDATE`，需要在 PG / MySQL 上查看实际 SQL 确认（SQLite 已正确跳过）

### N8（低）文档未同步

`docs/api.md:65` 仍然写着“列表统一返回 `items`、`total`、`page`、`limit`”。游标模式下 `total` 可能为 null，并且新增了 `has_more`、`next_cursor`。第 87 行已有说明，建议第 65 行加一句指向第 87 行。

## 三、复核中排除的疑点

以下几点在复核时怀疑过，确认没有问题：

- **修改存储位置的竞态**：`updateProfile` 在锁外统计图片引用，但它本身持有 `policyMu`，上传提交也需要 `policyMu`；加锁后还会再次检查 `storageUploads`。上传在 `updateProfile` 之后开始时，读到的是新的 store 快照。不存在悬空引用
- **用量重复扣减**：接口删除与后台清理并发时，`removeImageRecord` 以 `state = 'deleting'` 加 `RowsAffected` 判断，只有一方会扣减
- **假哈希成本**：常量哈希为 `$2a$10$`，与 `bcrypt.DefaultCost` 一致，时间特征相同
- **游标篡改**：游标不是授权凭据，每次都重新应用权限过滤；排序方式绑定在游标中，长度和类型都经过校验
- **304 越权**：权限和相册规则检查都在 ETag 判断之前，没有权限的请求拿不到 304
- **分享图片读取**：`serveShareImage` 的判断改为 `image.ID == row.TargetID`，与原来的 `shareImages` 查询语义一致

## 四、建议处理顺序

1. **N1**：影响二进制部署的核心功能，修复时一并处理 N5 的统计问题
2. **N2**：把 bcrypt 移出锁外，并给建分享接口加限流
3. **N7**：上线前在真实 PG / MySQL 上验证
4. N3、N4、N8 按需处理
