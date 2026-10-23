# QPix 性能分析报告

- 分析版本：`a6a6dac`（v0.1.2 开发中）
- 分析范围：`backend/` 请求链路、并发控制、图片处理、数据库访问
- 分析方式：静态代码审阅；**未执行压测或 EXPLAIN**，文中耗时均为数量级估算
- 结构：第一部分为通用问题；第二、三、四部分分别为 PostgreSQL（主要生产环境）、SQLite、MySQL 专项；第五部分为横向对比

## 总体评价

单实例、小规模使用没有明显问题。随着并发和数据量增长，瓶颈依次是：

1. **全局锁导致请求串行化**：与数据库无关，换 PG 也不会缓解
2. **列表接口 N+1 查询**：在 PG 上每次查询都是一次网络往返，代价比 SQLite 更高
3. **图片响应完全不可缓存**：每次浏览都完整回源
4. **COUNT + OFFSET 分页、前导通配 LIKE**：数据量上来后在 PG 上会退化为全表扫描

## 问题汇总

| 编号 | 影响 | 问题 | 与数据库相关性 |
|---|---|---|---|
| P1 | 高 | 全局 `policyMu`：写操作全站串行，读写互相阻塞 | 无关 |
| P2 | 高 | 图片响应始终 `no-store`，ETag / 304 不生效 | 无关 |
| P3 | 高 | 列表接口 N+1 查询 | PG / MySQL 更严重 |
| P4 | 高 | 上传持有 `settingsMu` 读锁覆盖存储 I/O | 无关 |
| P5 | 中 | 每个 Token 请求都写一次 `last_used_at` | SQLite 最严重 |
| P6 | 中 | 缩略图固定为 480px PNG | 无关 |
| P7 | 中 | 单次上传内存峰值高 | 无关 |
| P8 | 中 | PNG 量化逐像素接口调用，压缩编码全局串行 | 无关 |
| P9 | 中 | 每次上传都对用户全部图片做 `SUM(size)` | 三者均需扫描该用户全部行 |
| P10 | 中 | 每次图片访问查询 4–5 次数据库 | PG / MySQL 往返成本高 |
| P11–P13 | 低 | 限流表扫描、搜索、S3 中转 | 部分相关 |
| D1–D7 | — | PostgreSQL 专项 | PG |
| L1–L8 | — | SQLite 专项 | SQLite |
| M1–M7 | — | MySQL 专项 | MySQL |

## 一、通用问题（与数据库无关）

### P1. 全局 `policyMu` 导致请求串行化

位置：`app/app.go:221`

- 任何用户的写操作（改名、移动、删除、建分享、建 Token）都获取同一把排他锁，并且要等所有在途读请求结束，包括图片流式传输和 `Maintain`（最长 1 分钟）
- `deleteImages` 在锁内逐个同步删除存储对象；S3 下 100 张图约 200 次 HTTP 请求
- `deleteAlbum` 在锁内同步调用 `cleanupDeleted`，处理的是**全站**所有 `deleting` 状态的图片（`app/albums.go:172`），而不仅是当前相册

建议：锁只保护“读取权限状态 → 写入数据库”的临界区；存储 I/O 和图片传输移到锁外；`deleteAlbum` 只同步清理本相册，其余交给后台任务。与安全报告 S1 一起处理。

### P2. 图片响应完全不可缓存

位置：`app/images.go:493`、`:525`、`:534`

- `serveStoredImage` 开头就设置 `Cache-Control: private, no-store`，后面的 `if Cache-Control == ""` 永远不成立，`public, max-age=0, must-revalidate` 从不生效
- 浏览器不缓存，也就不会发送 `If-None-Match`，ETag / 304 逻辑形同虚设；CDN 也无法缓存
- `store.Open` 在 304 判断之前执行，S3 下即使命中 304 也会多一次 GetObject

建议：仅在 IP / Referer 规则启用时设置 `no-store`（`enforceScope` 中已有这段逻辑）；把 ETag 判断移到 `store.Open` 之前。改动小，收益明显。

### P3. 列表接口 N+1 查询

| 接口 | 每条记录的额外查询 | limit=100 时的总查询数 |
|---|---|---|
| `GET /images`（`viewFor`，`app/permissions.go:64`） | 1 次 COUNT（判断 can_manage）+ 1 次查用户 | 约 202 |
| `GET /albums`（`app/manage.go`） | 1 次 COUNT + 1 次查用户 | 2N + 1 |
| `GET /shares`（`shareView`） | 1 次 AES 解密 + 1 次查图片 + 1 次查用户 | 2N + 2 |
| `GET /tokens` | 1 次查用户 | N + 2 |
| `GET /users` | 每个用户 1 次 `SUM(size)` | N + 1 |
| `GET /storages` | 每个配置 1 次 COUNT（`storage_id` 无索引） | N + 1 |

建议：

- `viewFor` 的 COUNT 可在内存中判断：管理员会话恒为 true，普通会话为 `row.UserID == p.User.ID`，Token 分支已单独计算
- 先收集 `user_id`，再用一次 `WHERE id IN (...)` 批量查询用户
- 相册计数、用户用量、存储引用数改为一次 `GROUP BY`

### P4. 上传在存储 I/O 期间持有 `settingsMu` 读锁

位置：`app/images.go:218` 一直到事务提交

S3 的 Put（原图 + 缩略图）最长可达 90 秒。期间任何设置写操作都会排队，而排队的写者会阻塞 `uploadLimit()`、`cookiesSecure()`、`trusted()`、`getStore()` 等几乎所有请求都会调用的读锁。

建议：在锁内复制 `profile`、`store`、`processingSettings` 快照后立即释放；删除存储配置时已检查图片引用，残余竞态可通过引用计数或删除前重新检查解决。

### P5. 每个 Token 请求都写一次数据库

位置：`app/app.go:280`

每次 API 调用都执行一次 `UPDATE api_tokens SET last_used_at`，读请求也会变成“读 + 同步提交的写”。

建议：在内存中记录上次写入时间，间隔超过 1 分钟才落库。

### P6. 缩略图固定为 480px PNG

位置：`app/images.go:288`、`:338`

照片类缩略图编码为 PNG，通常几百 KB，可能比 JPEG / WebP 原图还大；原图本身不超过 480px 时也会再生成一份。

建议：照片用 JPEG 或 WebP 生成缩略图；原图不超过 480px 时直接复用原图。

### P7. 单次上传内存峰值高

`io.ReadAll` 读入原图（最大 100 MiB）→ 完整解码（1 亿像素约 400 MB NRGBA）→ 生成缩略图 → 可能再编码一次。2 个槽位并发时可能超过 1 GB。默认 2500 万像素约 100 MB，相对安全。

建议：文档写明 `max_pixels` 与内存的对应关系；容器部署时设置内存限制并相应调低 `max_pixels`。

### P8. PNG 量化与压缩编码

位置：`processing/processing.go`

- `quantizePNG` 逐像素调用 `source.At()` + `color.NRGBAModel.Convert`，接口调用和内存分配开销大
- 全局 `encodeMu` 使所有压缩编码完全串行

建议：对 `*image.NRGBA`、`*image.RGBA` 做类型断言后直接操作 `Pix` 切片。

### P9. 配额检查对全部图片求和

`reserveUpload` 每次上传都执行 `SUM(size) WHERE user_id = ?`，并且在全局 `quotaMu` 下执行；`uploadOptions` 也会执行一次。图片越多越慢。

建议：在 `users` 表维护 `used_bytes` 计数列，上传 / 删除时在同一事务内增减。

### P10. 每次图片访问查询 4–5 次数据库

带 Cookie 的请求：查会话 → 查用户 → 查图片（含 `guest_visible` 子查询）→ 查相册（Referer 规则）；自定义 Referer 时每次还要 `compileRule`。

建议：图片与相册合并为一次 JOIN；相册规则使用短 TTL 内存缓存（写入时主动失效）。

### 低影响

- **P11**：限流表超过 1024 条时，每个请求都会在锁内遍历整张 map（`app/app.go:368`）。建议改为定时清理。
- **P12**：搜索使用 `LOWER(name) LIKE '%x%'`，无法利用普通 B-Tree 索引（PG 方案见 D4）。
- **P13**：S3 存储的图片全部经 QPix 中转，带宽消耗两次。规模较大时，公开图片可 302 跳转到 CDN 或预签名 URL。

## 二、PostgreSQL 专项

当前 PG 相关配置（`database/database.go`）：`MaxOpenConns=10`、`MaxIdleConns=3`、`ConnMaxLifetime=30m`，GORM 使用默认配置（写操作自动包裹事务、不缓存预编译语句）。部署文档示例 DSN 使用 `sslmode=require`。

### D1. 连接池空闲数过小，导致连接反复建立

`MaxIdleConns=3` 小于 `MaxOpenConns=10`。突发并发结束后，超出 3 个的连接会被立即关闭，下一次突发再重新建立。PG 每个连接对应一个后端进程（fork），再加上 `sslmode=require` 的 TLS 握手，建连成本约数毫秒到十几毫秒，远高于单条查询。

建议：`MaxIdleConns` 设为与 `MaxOpenConns` 相同；增加 `SetConnMaxIdleTime(5 * time.Minute)` 回收长期空闲的连接；连接上限可做成配置项，并与 PG 的 `max_connections` 协调。

### D2. GORM 默认事务使每次写入多两次往返

GORM 默认对每个 `Create` / `Update` / `Delete` 包裹 `BEGIN ... COMMIT`。在 PG 上，一次单语句写入变成 3 次网络往返。P5（Token 写入）、上传预占的插入与删除、会话写入都受影响。

建议：`gorm.Config{SkipDefaultTransaction: true}`。代码中需要原子性的地方已显式使用 `db.Transaction`，单语句写入本身就是原子的。可选开启 `PrepareStmt: true` 减少解析开销；如果前面有 PgBouncer（transaction 模式），不要开启。

### D3. 索引与实际查询不匹配

现有 `images` 索引：`(user_id, created_at)`、`(album_id)`、`(state)`。

| 查询 | 现状 | 建议 |
|---|---|---|
| 图库列表：`user_id = ? AND state = 'ready' ORDER BY created_at DESC, id DESC` | 可用 `(user_id, created_at)`，但 `state` 需回表过滤，`id` 排序需要额外排序 | 部分索引 `(user_id, created_at DESC, id DESC) WHERE state = 'ready'` |
| 相册内列表：`album_id = ? AND state = 'ready' ORDER BY created_at DESC` | 只能用 `(album_id)`，然后排序 | `(album_id, created_at DESC, id DESC) WHERE state = 'ready'` |
| 管理员全站列表：`scope=all ORDER BY created_at DESC` | 没有以 `created_at` 开头的索引，全表扫描后排序 | `(created_at DESC, id DESC) WHERE state = 'ready'` |
| 存储引用计数：`storage_id = ?` | **无索引**，全表扫描 | `(storage_id)` |
| 后台清理：`state = 'deleting'` | `(state)` 选择性极低（几乎全是 `ready`），优化器通常不用 | 改为部分索引 `(id) WHERE state = 'deleting'`，删除原 `(state)` 索引 |
| 用户用量：`SUM(size) WHERE user_id = ?` | 走 `(user_id, created_at)` 后回表取 `size` | 覆盖索引 `(user_id) INCLUDE (size)`，或采用 P9 的计数列 |

按 `name` / `size` 排序没有索引，属于低频操作，可以接受。

迁移时注意：项目同时支持 SQLite 和 MySQL，部分索引和 `INCLUDE` 只有 PG 支持（SQLite 支持部分索引，MySQL 都不支持），需要按方言分支，或者只为 PG 额外建索引。

### D4. 搜索无法使用索引

`LOWER(name) LIKE '%x%'` 带前导通配，B-Tree 索引无效，必然全表扫描（图片表、分享搜索中的子查询都是如此）。

建议：PG 上启用 `pg_trgm` 扩展，并建立 `GIN (lower(name) gin_trgm_ops)` 索引，现有 SQL 无需修改即可走索引。需要数据库账号有创建扩展的权限。

### D5. COUNT + OFFSET 分页

图库、分享、Token 列表每次翻页都执行一次 `COUNT(*)` 再 `OFFSET (page-1)*limit`：

- PG 的 MVCC 没有行数缓存，`COUNT(*)` 需要扫描全部匹配行（可以走 index-only scan，但依赖 visibility map 是否及时更新）
- `OFFSET` 需要扫描并丢弃前面的所有行，翻页越深越慢；`page` 上限为 100 万

建议：

- 主图库改用游标（keyset）分页：`WHERE (created_at, id) < (?, ?)`，配合 D3 的索引，每页成本固定
- 总数只在首页或筛选条件变化时计算；或者在没有筛选条件时用 `pg_class.reltuples` 估算

### D6. 高频小写入与 MVCC

- `last_used_at` 每请求一次 UPDATE（P5）。该列不在任何索引中，PG 可以走 HOT 更新，表膨胀不严重；但每次都是一次同步提交（WAL fsync），延迟受磁盘影响
- `upload_reservations` 每次上传插入一行、删除一行，产生死元组，依赖 autovacuum 回收；表很小，影响有限
- `Maintain` 每 10 分钟批量删除过期会话，有 `expires_at` 索引，正常

建议：采用 P5 的节流方案后，这一项基本消失。

### D7. 配额检查可以利用 PG 行锁替代进程锁

`reserveUpload` 在进程级的 `quotaMu` 下执行，**所有用户**的上传预占互相串行。在 PG / MySQL 上，可以在事务内对用户行执行 `SELECT ... FOR UPDATE`，只让同一用户的上传串行，不同用户可以并行。SQLite 只有单连接，保留进程锁即可。

注意：这只解决了单实例内的并发。项目文档声明为单实例运行；如果以后要多实例，P1 的全局锁、内存中的限流表、设置缓存都需要一起重新设计。

## 三、SQLite 专项

当前配置（`database/database.go`）：驱动为纯 Go 的 `modernc.org/sqlite`（`CGO_ENABLED=0` 构建）；`MaxOpenConns=1`、`MaxIdleConns=1`、`ConnMaxLifetime=0`；连接后执行 `journal_mode=WAL`、`busy_timeout=5000`、`foreign_keys=ON`。

### L1. 单连接使全部数据库访问串行

所有请求共用一个连接。数据库操作排队时，请求在 `database/sql` 连接池中等待，直到拿到连接或请求 context 取消。

- P3 的 N+1 查询在 SQLite 上单条只要几十微秒，但 limit=100 的图库列表约 200 次查询，会**连续占用连接**，其他请求只能等待
- 任何慢查询（全表 COUNT、LIKE 搜索、深分页 OFFSET）都会让全站所有数据库操作停顿
- 结合 P1 的全局锁，SQLite 部署实际上是“锁串行 + 连接串行”两层串行

建议：优先解决 P3；如果需要更高的读并发，可以使用“1 个写连接 + N 个只读连接”（WAL 允许并发读，只读连接使用 `mode=ro` 或 `PRAGMA query_only`）。这需要区分读写路径，改动较大，个人使用场景不必要。

### L2. 未设置 `synchronous=NORMAL`

WAL 模式下默认 `synchronous=FULL`，每次提交都要 fsync WAL 文件。`NORMAL` 在 WAL 模式下仍能保证数据库不损坏，只是在断电时可能丢失最后几个事务，对图床可以接受。

收益：写入延迟明显下降，尤其在机械盘、云盘或 Docker 绑定挂载上。P5（Token 每次请求写一次）和登录、上传预占都会受益。

### L3. PRAGMA 只在首次连接时执行

PRAGMA 通过 `db.Exec` 执行一次。`busy_timeout`、`foreign_keys`、`synchronous` 都是**连接级**设置。目前 `ConnMaxLifetime=0` 且只有一个连接，正常情况下不会丢失；但如果连接因错误被连接池关闭并重建，新连接就没有这些设置：

- `foreign_keys` 丢失：`ON DELETE SET NULL` 等外键约束失效（属于正确性问题）
- `busy_timeout` 丢失：使用外部工具备份时，可能立即返回 `SQLITE_BUSY`

建议：让每个新连接自动执行 PRAGMA。可以在 `Open` 内部给文件路径拼接 modernc 驱动支持的 `_pragma` 参数（如 `?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)`），也可以使用驱动的连接钩子。注意：现有代码会对 DSN 执行 `MkdirAll(filepath.Dir(dsn))`，并且约定 DSN 是普通文件路径，所以不要让用户直接在 `QPIX_DB_DSN` 里写参数，应在代码内部拼接。`journal_mode=WAL` 持久化在数据库文件中，不受影响。

### L4. 从未执行 `ANALYZE`，查询优化器没有统计信息

没有 `sqlite_stat1` 时，SQLite 用固定的估算值选择索引。在 `images` 上有多个可选索引（`user_id`、`album_id`、`state`）时，可能选中低选择性的 `state` 索引。

建议：启动时及 `Maintain` 中定期执行 `PRAGMA optimize`（SQLite 3.46 起会自动在需要时执行 ANALYZE，成本很低）。

### L5. WAL 文件增长与检查点

WAL 默认在达到 1000 页（约 4 MB）时自动检查点。单连接下没有长时间读事务阻止检查点，一般没有问题。但以下情况会让 WAL 持续增长、读性能下降：

- 外部工具（备份脚本、`sqlite3` 命令行、监控）长时间打开读事务
- 批量删除（`cleanupDeleted`、`Maintain` 清理会话）后，文件不会收缩

建议：在 `Maintain` 中定期执行 `PRAGMA wal_checkpoint(TRUNCATE)`；大量删除后可以在维护窗口执行 `VACUUM`（会锁库，需要停服或低峰执行），或者开启 `auto_vacuum=INCREMENTAL`（需要在建库时设置，已有库需 VACUUM 一次才生效）。

### L6. 索引能力

- 支持部分索引：D3 中的 `... WHERE state = 'ready'` 部分索引可以直接使用。注意 SQLite 只有在查询的 WHERE 条件**字面上**蕴含索引条件时才会使用部分索引，`state = ?` 传参的写法可能用不上，需要写成 `state = 'ready'` 常量
- 不支持 `INCLUDE`：覆盖索引改为把 `size` 加进复合索引，例如 `(user_id, size)`
- 没有 trigram 索引：搜索可选 FTS5 的 `trigram` 分词器（SQLite 3.34+），但需要额外的虚拟表和同步触发器，个人使用规模下不必要
- `COUNT(*)` 和 `OFFSET` 同样需要线性扫描，D5 的游标分页建议同样适用

### L7. 纯 Go 驱动的开销

`modernc.org/sqlite` 是把 C 源码转译成 Go，无需 CGO，便于交叉编译和使用 scratch 镜像。代价是 CPU 密集的查询（大表扫描、排序、聚合）通常比 CGO 版 `mattn/go-sqlite3` 慢一些，具体差距与查询类型有关，应以实测为准。对于以点查为主的图床负载影响不大，当前选型合理。

### L8. 正确的地方

- 事务内一律使用 `tx` 而不是 `a.db`。单连接下如果在事务里使用 `a.db`，会因为拿不到第二个连接而**永久死锁**；目前代码没有这个问题，后续修改要保持这一点
- 单连接天然避免了 `SQLITE_BUSY` 和写锁竞争
- 文档已提示不要把数据库放在网络文件系统上（WAL 依赖共享内存）

### SQLite 适用规模参考（估算）

| 规模 | 预期 |
|---|---|
| 1–5 个用户、图片数万张 | 完全够用，开启 L2 后基本感受不到数据库开销 |
| 10+ 个并发活跃用户、图片 10 万张以上 | P3 的 N+1 和 L1 的单连接排队开始明显，图库翻页、管理员全站列表变慢 |
| 多个上传脚本持续调用 API | P5 的 Token 写入与上传事务竞争同一个连接，建议迁移到 PG |

## 四、MySQL 专项

当前配置：连接池与 PG 相同（`MaxOpenConns=10`、`MaxIdleConns=3`、`ConnMaxLifetime=30m`）；部署文档示例 DSN 为 `charset=utf8mb4&parseTime=True&loc=UTC`。代码中的多个迁移步骤已兼容 MySQL 的非事务 DDL。

### M1. 连接池与事务开销

- D1：MySQL 使用线程模型，建连比 PG 便宜，但如果启用了 TLS，握手开销仍然存在。`MaxIdleConns` 同样建议与 `MaxOpenConns` 相同
- `ConnMaxLifetime=30m` 需要小于服务端 `wait_timeout`（默认 8 小时，但云数据库和代理常设为几分钟）。否则会拿到已被服务端关闭的连接，出现 `invalid connection` 错误和重试。如果中间有 ProxySQL 或云负载均衡，建议降到 5 分钟以内
- D2：`SkipDefaultTransaction` 同样适用。InnoDB 默认 `innodb_flush_log_at_trx_commit=1`，每次提交都会刷盘，减少无意义的事务收益明显

### M2. 预编译往返

go-sql-driver/mysql 默认对每条参数化查询执行“PREPARE → EXECUTE → CLOSE”，一次查询 3 次往返。结合 P3 的 N+1，往返次数会再放大。

两种方案选一种：

- DSN 增加 `interpolateParams=true`：驱动在客户端转义参数，只需 1 次往返。要求连接字符集为 `utf8mb4` 等安全字符集（文档示例已满足），不能用于 `gbk`、`big5` 等多字节字符集
- 或者 GORM 开启 `PrepareStmt: true`，复用已预编译的语句

### M3. 随机字符串主键与 InnoDB 聚簇索引

所有表的主键都是 `randomID()` 生成的 32 位十六进制字符串（`varchar(32)`）。InnoDB 按主键聚簇存储整行数据：

- 随机主键使插入位置分散在整棵 B+ 树中，造成页分裂和碎片，表越大插入越慢、缓冲池命中率越低
- 每个二级索引都隐式携带主键，`varchar(32)` 主键使所有二级索引都更大

对图床的写入量（每张图一行）影响有限；数据达到百万级时才需要考虑。可选方案：改用时间有序的 ID（如 UUIDv7 或 ULID 的十六进制形式，仍为 32 位，可兼容现有列类型）。这一点对 PG 也有一定影响，但 PG 使用堆表，主键索引与数据分离，影响较小。

### M4. `LOWER(name)` 使索引失效，且在 MySQL 上多余

MySQL 8 的默认排序规则 `utf8mb4_0900_ai_ci` 本身就不区分大小写，`LOWER(name) LIKE ?` 对列使用函数，任何索引都无法使用。如果去掉 `LOWER`，**不带前导通配**的搜索就能走 `name` 索引；但当前是 `%x%`，无论如何都要全表扫描。

- 可选方案：`FULLTEXT INDEX (name) WITH PARSER ngram` 支持中文，查询改为 `MATCH ... AGAINST`。它的分词语义与 LIKE 不同（受最小词长、ngram_token_size 影响），需要按方言分支
- 个人或小团队规模下，全表扫描 `images` 通常也能接受

### M5. 索引能力

- 不支持部分索引和 `INCLUDE`：D3 的建议改为普通复合索引，例如 `(user_id, state, created_at, id)`、`(album_id, state, created_at, id)`、`(state, created_at, id)`、`(storage_id)`
- `(state)` 单列索引选择性很低，和 PG 一样建议删除，改用上面以 `state` 为第二列的复合索引
- MySQL 8 支持降序索引；只要 `ORDER BY created_at DESC, id DESC` 方向一致，升序索引也可以反向扫描，不必专门建降序索引
- `COUNT(*)` 在 InnoDB 上同样需要扫描（会选择最小的二级索引），D5 的游标分页同样适用

### M6. 行锁与间隙锁

如果采用 D7 的 `SELECT ... FOR UPDATE` 替代 `quotaMu`：在默认的 `REPEATABLE READ` 隔离级别下，按 `id` 主键等值加锁只锁住单行，不会产生间隙锁，可以放心使用。注意：不要对 `upload_reservations` 按 `user_id` 范围执行 `FOR UPDATE`，那样会加间隙锁，导致其他用户的插入被阻塞。

### M7. 批量删除

`Maintain` 中的 `DELETE ... WHERE expires_at < ?`、修改 `secure_cookie` 时的 `DELETE ... WHERE 1 = 1`，在 InnoDB 上会逐行加锁并写 undo log。会话表很小，影响可以忽略；如果以后表变大，可以改为分批删除（`LIMIT 1000` 循环）。

### MySQL 适用规模参考（估算）

与 PG 基本相当。在连接池、`interpolateParams`、复合索引都调优后，单实例的瓶颈仍然是 P1 全局锁，而不是数据库本身。

## 五、三种数据库横向对比

### 各项建议的适用性

| 建议 | PostgreSQL | MySQL | SQLite |
|---|---|---|---|
| P3 消除 N+1 | 高收益（网络往返） | 高收益（网络往返 + M2） | 中收益（减少单连接占用） |
| P5 Token 写入节流 | 中 | 中（每次提交刷盘） | 高（与所有请求争用唯一连接） |
| 连接池 `MaxIdleConns` = `MaxOpenConns` | D1，收益明显 | M1，收益较小 | 不适用 |
| `SkipDefaultTransaction` | D2 | M1 | 适用，收益较小 |
| 预编译 / 参数插值 | 可选 `PrepareStmt` | M2，收益明显 | 不需要 |
| 部分索引 | 支持 | 不支持，改用复合索引 | 支持，条件需写常量 |
| 覆盖索引 `INCLUDE` | 支持 | 不支持 | 不支持 |
| 模糊搜索索引 | `pg_trgm` + GIN | FULLTEXT ngram（语义不同） | FTS5 trigram（需虚拟表） |
| 游标分页替代 OFFSET | 适用 | 适用 | 适用 |
| 用行锁替代 `quotaMu` | D7 | 适用（注意 M6 间隙锁） | 不适用，保留进程锁 |
| 提交刷盘调优 | `synchronous_commit` 由 DBA 决定 | `innodb_flush_log_at_trx_commit` 由 DBA 决定 | L2 `synchronous=NORMAL`，应用内设置 |
| 统计信息维护 | autovacuum 自动处理 | 自动采样 | L4 需要应用执行 `PRAGMA optimize` |

### 选型结论

| 场景 | 推荐 |
|---|---|
| 个人使用、单机、低并发 | SQLite 足够，按第三部分开启 L2、L3、L4 |
| 多用户、有并发写入、数据库与应用分离部署 | PostgreSQL，按第二部分调优 |
| 已有 MySQL 运维体系 | 可用，按第四部分调优后与 PG 接近，索引能力稍弱 |

注意：对三种数据库而言，P1（全局锁）都是主要瓶颈。不解决 P1，从 SQLite 换成 PG 对并发能力的提升有限。

## 建议优化顺序

PostgreSQL 部署：

1. **P1** 全局锁收缩（与安全报告 S1 一起处理）
2. **P2** 修正图片缓存头：改动小，收益明显
3. **D1 + D2** 连接池与 `SkipDefaultTransaction`：只改配置，无业务风险
4. **P3** 消除 N+1；**P5** Token 写入节流
5. **D3** 补充索引（需要新增一个迁移版本，并按方言分支）
6. **P4、P9 / D7、D5、D4** 按数据规模逐步推进
7. P6–P8、P10–P13 按需处理

SQLite 部署：P1 → P2 → **L2 + L3**（改动小，L3 还涉及外键正确性）→ P3 + P5 → L4 → 其余按需。

MySQL 部署：P1 → P2 → **M1 + M2**（只改配置和 DSN）→ P3 + P5 → M5 索引 → 其余按需。

上线前建议用真实规模的数据（如 10 万张图片记录）在 PG 上对图库列表、搜索、全站列表执行 `EXPLAIN (ANALYZE, BUFFERS)`，确认 D3 / D4 的索引实际被使用。
