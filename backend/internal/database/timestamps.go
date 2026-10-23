package database

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

func migrateSQLiteTimes(db *gorm.DB) error {
	var count int64
	if err := db.Model(&SchemaMigration{}).Where("version = ?", 9).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if db.Dialector.Name() == "sqlite" {
			// TEXT 时间需统一时区才能正确排序；连同到期、撤销等字段一起规范，保留 NULL。
			for _, model := range []any{&User{}, &Session{}, &Album{}, &Image{}, &APIToken{}, &Share{}, &ShareSession{}, &StorageProfile{}, &SchemaMigration{}} {
				stmt := &gorm.Statement{DB: tx}
				if err := stmt.Parse(model); err != nil {
					return err
				}
				for _, field := range stmt.Schema.Fields {
					if field.DataType == schema.Time {
						if err := normalizeSQLiteTime(tx, model, stmt.Schema.PrioritizedPrimaryField.DBName, field.DBName); err != nil {
							return fmt.Errorf("统一 SQLite 时间 %s.%s 失败：%w", stmt.Table, field.DBName, err)
						}
					}
				}
			}
		}
		return tx.Create(&SchemaMigration{Version: 9, AppliedAt: time.Now().UTC()}).Error
	})
}

func normalizeSQLiteTime(tx *gorm.DB, model any, primary, column string) error {
	last := ""
	for {
		var batch []struct {
			RecordKey  string
			RecordedAt time.Time
		}
		q := tx.Model(model).Select("? AS record_key, ? AS recorded_at", clause.Column{Name: primary}, clause.Column{Name: column}).
			Where(clause.Neq{Column: clause.Column{Name: column}, Value: nil}).
			Order(clause.OrderByColumn{Column: clause.Column{Name: primary}}).Limit(500)
		if last != "" {
			q = q.Where(clause.Gt{Column: clause.Column{Name: primary}, Value: last})
		}
		if err := q.Find(&batch).Error; err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		// Find 已关闭游标，随后复用同一事务连接更新，避免 SQLite 单连接死锁。
		for _, row := range batch {
			if err := tx.Model(model).Where(clause.Eq{Column: clause.Column{Name: primary}, Value: row.RecordKey}).
				UpdateColumn(column, row.RecordedAt.UTC()).Error; err != nil {
				return err
			}
		}
		last = batch[len(batch)-1].RecordKey
	}
}
