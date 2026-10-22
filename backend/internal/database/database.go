package database

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
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
		// 使用普通文件路径，避免把 DSN 参数误当成文件名；SQLite 参数在连接后统一配置。
		if err := os.MkdirAll(filepath.Dir(dsn), 0700); err != nil {
			return nil, fmt.Errorf("创建 SQLite 数据目录失败：%w", err)
		}
		dialector = sqlite.New(sqlite.Config{DriverName: "sqlite", DSN: dsn})
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
	db, err := gorm.Open(dialector, &gorm.Config{Logger: dbLogger, TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("连接 %s 数据库失败：%w", driver, err)
	}
	pool, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取 %s 数据库连接池失败：%w", driver, err)
	}
	pool.SetConnMaxLifetime(30 * time.Minute)
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(3)
	if driver == "sqlite" {
		// 单连接串行执行短事务，避免 SQLite 写锁竞争；不能将数据库放到网络文件系统。
		pool.SetMaxOpenConns(1)
		pool.SetMaxIdleConns(1)
		pool.SetConnMaxLifetime(0)
		for _, sql := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON"} {
			if err = db.Exec(sql).Error; err != nil {
				pool.Close()
				return nil, fmt.Errorf("配置 SQLite（%s）失败：%w", sql, err)
			}
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
	if err := db.Model(&SchemaMigration{}).Where("version = ?", 6).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	if !db.Migrator().HasColumn(&APIToken{}, "Authorization") {
		if err := db.Migrator().AddColumn(&APIToken{}, "Authorization"); err != nil {
			return err
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
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
}
