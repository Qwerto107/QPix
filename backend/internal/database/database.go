package database

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite"
)

func Open(driver, dsn string) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch driver {
	case "sqlite":
		uri, err := sqliteDSN(dsn)
		if err != nil {
			return nil, err
		}
		dialector = sqlite.New(sqlite.Config{DriverName: "sqlite", DSN: uri})
	case "postgres":
		dialector = postgres.Open(dsn)
	case "mysql":
		dialector = mysql.Open(dsn)
	default:
		return nil, fmt.Errorf("不支持的数据库类型：%s", driver)
	}
	// 只记录失败 SQL，保留参数占位符，避免将凭据和业务数据插入 SQL 日志。
	dbLogger := logger.New(log.New(os.Stderr, "", log.LstdFlags), logger.Config{
		LogLevel:                  logger.Error,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
	})
	// 多语句操作由业务显式开启事务，单语句写入无需额外的 BEGIN/COMMIT。
	options := &gorm.Config{
		Logger: dbLogger, TranslateError: true, SkipDefaultTransaction: true,
		NowFunc: func() time.Time { return time.Now().UTC() },
	}
	if driver == "mysql" {
		// 缓存有容量与时限边界，减少重复预编译，也兼容已有连接字符集配置。
		options.PrepareStmt = true
		options.PrepareStmtMaxSize = 100
		options.PrepareStmtTTL = 5 * time.Minute
	}
	db, err := gorm.Open(dialector, options)
	if err != nil {
		return nil, fmt.Errorf("连接 %s 数据库失败：%w", driver, err)
	}
	pool, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取 %s 数据库连接池失败：%w", driver, err)
	}
	pool.SetConnMaxLifetime(30 * time.Minute)
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(10)
	pool.SetConnMaxIdleTime(5 * time.Minute)
	if driver == "mysql" {
		// 提前回收连接，减少云数据库和代理关闭长期空闲连接导致的重试。
		pool.SetConnMaxLifetime(3 * time.Minute)
	}
	if driver == "sqlite" {
		// 单连接串行执行短事务，避免 SQLite 写锁竞争；不能将数据库放到网络文件系统。
		pool.SetMaxOpenConns(1)
		pool.SetMaxIdleConns(1)
		pool.SetConnMaxLifetime(0)
		pool.SetConnMaxIdleTime(0)
		if err = db.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
			pool.Close()
			return nil, fmt.Errorf("配置 SQLite 失败：%w", err)
		}
	}
	return db, nil
}

func Migrate(db *gorm.DB) error {
	if !db.Migrator().HasTable(&SchemaMigration{}) {
		if err := db.Migrator().CreateTable(&SchemaMigration{}); err != nil {
			return err
		}
	}
	var count int64
	if err := db.Model(&SchemaMigration{}).Where("version = ?", 1).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		// 首次建表允许失败后重试，兼容 MySQL DDL 不随事务回滚的行为。后续变更须新增版本，不能修改已发布的建表定义。
		for _, model := range []any{&User{}, &Session{}, &Album{}, &Image{}, &APIToken{}, &Setting{}} {
			if !db.Migrator().HasTable(model) {
				if err := db.Migrator().CreateTable(model); err != nil {
					return err
				}
			}
		}
		if err := db.Create(&SchemaMigration{Version: 1, AppliedAt: time.Now().UTC()}).Error; err != nil {
			return err
		}
	}
	if err := db.Model(&SchemaMigration{}).Where("version = ?", 2).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return migratePermissions(db)
	}
	for _, model := range []any{&StorageProfile{}, &AccessRule{}} {
		if !db.Migrator().HasTable(model) {
			if err := db.Migrator().CreateTable(model); err != nil {
				return err
			}
		}
	}
	// 逐项检查使部分执行后的重试保持幂等，兼容 MySQL 的非事务 DDL。
	for _, field := range []string{"OriginalSize", "Optimized", "CompressionStatus"} {
		if !db.Migrator().HasColumn(&Image{}, field) {
			if err := db.Migrator().AddColumn(&Image{}, field); err != nil {
				return err
			}
		}
	}
	if err := db.Model(&Image{}).Where("original_size = ?", 0).Update("original_size", gorm.Expr("size")).Error; err != nil {
		return err
	}
	if err := db.Create(&SchemaMigration{Version: 2, AppliedAt: time.Now().UTC()}).Error; err != nil {
		return err
	}
	return migratePermissions(db)
}

func migratePermissions(db *gorm.DB) error {
	var count int64
	if err := db.Model(&SchemaMigration{}).Where("version = ?", 3).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return migrateTokenExpiration(db)
	}
	for _, model := range []any{&AlbumMember{}, &UploadReservation{}, &Share{}, &ShareSession{}} {
		if !db.Migrator().HasTable(model) {
			if err := db.Migrator().CreateTable(model); err != nil {
				return err
			}
		}
	}
	for _, item := range []struct {
		model  any
		fields []string
	}{
		{&User{}, []string{"Role", "Enabled", "QuotaBytes", "MaxFileBytes"}}, {&Album{}, []string{"GuestVisible"}},
	} {
		for _, field := range item.fields {
			if !db.Migrator().HasColumn(item.model, field) {
				if err := db.Migrator().AddColumn(item.model, field); err != nil {
					return err
				}
			}
		}
	}
	// 开发库原先只有初始化管理员，保留该账号以便继续本地验收。
	if err := db.Model(&User{}).Count(&count).Error; err != nil {
		return err
	}
	if count == 1 {
		if err := db.Model(&User{}).Where("role = ?", "user").Update("role", "admin").Error; err != nil {
			return err
		}
	}
	if err := db.Create(&SchemaMigration{Version: 3, AppliedAt: time.Now().UTC()}).Error; err != nil {
		return err
	}
	return migrateTokenExpiration(db)
}

func migrateTokenExpiration(db *gorm.DB) error {
	var count int64
	if err := db.Model(&SchemaMigration{}).Where("version = ?", 4).Count(&count).Error; err != nil {
		return err
	} else if count > 0 {
		return migratePersonalSpace(db)
	}
	// NULL 表示永久有效；仅修改到期列的可空属性，保留已有 Token 的期限和哈希。
	columns, err := db.Migrator().ColumnTypes(&APIToken{})
	if err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name() == "expires_at" {
			if nullable, known := column.Nullable(); !known || !nullable {
				if err = db.Migrator().AlterColumn(&APIToken{}, "ExpiresAt"); err != nil {
					return err
				}
			}
		}
	}
	if err := db.Create(&SchemaMigration{Version: 4, AppliedAt: time.Now().UTC()}).Error; err != nil {
		return err
	}
	return migratePersonalSpace(db)
}

// 逐列迁移可重复执行，兼容 MySQL 非事务 DDL，保留现有图片和凭据。
func migratePersonalSpace(db *gorm.DB) error {
	var count int64
	if err := db.Model(&SchemaMigration{}).Where("version = ?", 5).Count(&count).Error; err != nil {
		return err
	} else if count > 0 {
		return migrateTokenPermissions(db)
	}
	for _, item := range []struct {
		model  any
		fields []string
	}{
		{&User{}, []string{"DefaultAlbumID"}}, {&Album{}, []string{"RefererMode", "Referers", "AllowEmptyReferer"}},
		{&Share{}, []string{"UserID"}}, {&APIToken{}, []string{"LastUsedAt", "RevokedAt"}},
	} {
		for _, field := range item.fields {
			if !db.Migrator().HasColumn(item.model, field) {
				if err := db.Migrator().AddColumn(item.model, field); err != nil {
					return err
				}
			}
		}
	}
	// 历史分享仅由管理员创建；相册分享停用，单图分享保留原链接。
	var admin User
	if err := db.Where("role = ?", "admin").Order("created_at ASC").First(&admin).Error; err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	if admin.ID != "" {
		if err := db.Model(&Share{}).Where("user_id IS NULL OR user_id = ?", "").Update("user_id", admin.ID).Error; err != nil {
			return err
		}
	}
	if err := db.Model(&Share{}).Where("kind = ? AND revoked_at IS NULL", "album").Update("revoked_at", time.Now().UTC()).Error; err != nil {
		return err
	}
	if err := db.Where("1 = 1").Delete(&AlbumMember{}).Error; err != nil {
		return err
	}
	if err := db.Create(&SchemaMigration{Version: 5, AppliedAt: time.Now().UTC()}).Error; err != nil {
		return err
	}
	return migrateTokenPermissions(db)
}

// 旧用户 Token 保持全部本人相册仅上传；管理员 Token 停用但保留记录。
func migrateTokenPermissions(db *gorm.DB) error {
	var count int64
	if err := db.Model(&SchemaMigration{}).Where("version = ?", 6).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return migrateUsedBytes(db)
	}
	if !db.Migrator().HasColumn(&APIToken{}, "Authorization") {
		if err := db.Migrator().AddColumn(&APIToken{}, "Authorization"); err != nil {
			return err
		}
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		// authorization 是 PostgreSQL 保留字，由数据库方言负责引用列名。
		if err := tx.Model(&APIToken{}).Where(clause.Or(
			clause.Eq{Column: clause.Column{Name: "authorization"}, Value: nil},
			clause.Eq{Column: clause.Column{Name: "authorization"}, Value: ""},
		)).Update("authorization", `{"all_albums":true,"permissions":["upload"],"albums":[]}`).Error; err != nil {
			return err
		}
		admins := tx.Model(&User{}).Select("id").Where("role = ?", "admin")
		if err := tx.Model(&APIToken{}).Where("user_id IN (?) AND revoked_at IS NULL", admins).Update("revoked_at", time.Now().UTC()).Error; err != nil {
			return err
		}
		return tx.Create(&SchemaMigration{Version: 6, AppliedAt: time.Now().UTC()}).Error
	})
	if err != nil {
		return err
	}
	return migrateUsedBytes(db)
}

func migrateUsedBytes(db *gorm.DB) error {
	var count int64
	if err := db.Model(&SchemaMigration{}).Where("version = ?", 7).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return migrateListIndexes(db)
	}
	if !db.Migrator().HasColumn(&User{}, "UsedBytes") {
		if err := db.Migrator().AddColumn(&User{}, "UsedBytes"); err != nil {
			return err
		}
	}
	// 启动迁移时一次性回填，待删除图片仍计入用量；回填与版本记录同一事务，可安全重试。
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("1 = 1").Update("used_bytes", 0).Error; err != nil {
			return err
		}
		var totals []struct {
			UserID string
			Bytes  int64
		}
		if err := tx.Model(&Image{}).Select("user_id, COALESCE(SUM(size),0) AS bytes").Group("user_id").Scan(&totals).Error; err != nil {
			return err
		}
		for _, total := range totals {
			if err := tx.Model(&User{}).Where("id = ?", total.UserID).Update("used_bytes", total.Bytes).Error; err != nil {
				return err
			}
		}
		return tx.Create(&SchemaMigration{Version: 7, AppliedAt: time.Now().UTC()}).Error
	})
	if err != nil {
		return err
	}
	return migrateListIndexes(db)
}

func migrateListIndexes(db *gorm.DB) error {
	var count int64
	if err := db.Model(&SchemaMigration{}).Where("version = ?", 8).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		// 使用完整复合索引兼容三种数据库；逐项检查兼容 MySQL 非事务 DDL 的失败重试。
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(&Image{}); err != nil {
			return err
		}
		for _, suffix := range []string{"owner_ready_date", "album_ready_date", "ready_date", "storage_id"} {
			name := db.NamingStrategy.IndexName(stmt.Table, suffix)
			if !db.Migrator().HasIndex(&Image{}, name) {
				if err := db.Migrator().CreateIndex(&Image{}, name); err != nil {
					return fmt.Errorf("创建图片列表索引 %s 失败：%w", name, err)
				}
			}
		}
		if err := db.Create(&SchemaMigration{Version: 8, AppliedAt: time.Now().UTC()}).Error; err != nil {
			return err
		}
	}
	if err := ensureSearchIndex(db); err != nil {
		return err
	}
	if err := migrateSQLiteTimes(db); err != nil {
		return err
	}
	if err := migrateImagePreviews(db); err != nil {
		return err
	}
	// 建表和新增索引后补充统计信息，避免新索引缺少优化器估算数据。
	return optimizeSQLite(db, true)
}

func migrateImagePreviews(db *gorm.DB) error {
	var count int64
	if err := db.Model(&SchemaMigration{}).Where("version = ?", 10).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	// 只加可空列，旧原图和缩略图不重写；兼容 MySQL 非事务 DDL 的重试。
	if !db.Migrator().HasColumn(&Image{}, "PreviewKey") {
		if err := db.Migrator().AddColumn(&Image{}, "PreviewKey"); err != nil {
			return err
		}
	}
	return db.Create(&SchemaMigration{Version: 10, AppliedAt: time.Now().UTC()}).Error
}

func ensureSearchIndex(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	var installed bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm')").Scan(&installed).Error; err != nil {
		return fmt.Errorf("检测 pg_trgm 扩展失败：%w", err)
	}
	if !installed {
		slog.Warn("未安装 pg_trgm，图片名称搜索使用扫描；数据库管理员可执行 CREATE EXTENSION pg_trgm 后重启以创建搜索索引")
		return nil
	}
	// 每次启动检查，使管理员稍后安装扩展也能补建；不由应用创建数据库扩展。
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&Image{}); err != nil {
		return err
	}
	name := db.NamingStrategy.IndexName(stmt.Table, "name_trgm")
	if db.Migrator().HasIndex(&Image{}, name) {
		return nil
	}
	// 仅拼接 GORM 引用过的内部标识符，兼容自定义 schema 和表名前缀。
	if err := db.Exec("CREATE INDEX " + stmt.Quote(name) + " ON " + stmt.Quote(stmt.Table) + " USING GIN (lower(name) gin_trgm_ops)").Error; err != nil {
		return fmt.Errorf("创建图片名称搜索索引失败（请检查 pg_trgm 是否在 search_path 中及建索引权限）：%w", err)
	}
	return nil
}
