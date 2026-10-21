package database

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite"
)

func Open(driver, dsn string) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch driver {
	case "sqlite":
		// 使用普通文件路径，避免把 DSN 参数误当成文件名；SQLite 参数在连接后统一配置。
		if err := os.MkdirAll(filepath.Dir(dsn), 0700); err != nil {
			return nil, err
		}
		dialector = sqlite.New(sqlite.Config{DriverName: "sqlite", DSN: dsn})
	case "postgres":
		dialector = postgres.Open(dsn)
	case "mysql":
		dialector = mysql.Open(dsn)
	default:
		return nil, fmt.Errorf("不支持的数据库类型：%s", driver)
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败，请检查连接配置")
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
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
				return nil, err
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
	if err := db.Model(&SchemaMigration{}).Where("version = ?", 4).Count(&count).Error; err != nil || count > 0 {
		return err
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
	return db.Create(&SchemaMigration{Version: 4, AppliedAt: time.Now().UTC()}).Error
}
