package app

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
	"qpix/internal/database"
)

func TestExpiredSessionBatches(t *testing.T) {
	db, err := database.Open("sqlite", filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().UTC()
	for _, item := range []struct {
		model any
		owner string
	}{{&database.Session{}, "user_id"}, {&database.ShareSession{}, "share_id"}} {
		rows := make([]map[string]any, 1002)
		for i := range rows {
			expires := cutoff.Add(-time.Hour)
			if i == len(rows)-1 {
				expires = cutoff.Add(time.Hour)
			}
			rows[i] = map[string]any{"id": fmt.Sprintf("%064d", i), item.owner: "owner", "expires_at": expires}
			if item.owner == "user_id" {
				rows[i]["csrf"] = "csrf"
			}
		}
		if err := db.Model(item.model).CreateInBatches(rows, 250).Error; err != nil {
			t.Fatal(err)
		}
		batches := 0
		callback := db.Callback().Delete()
		if err := callback.After("gorm:delete").Register("qpix:test-session-batch", func(tx *gorm.DB) {
			batches++
			if tx.RowsAffected > 1000 {
				t.Error("单次清理超过批次上限", tx.RowsAffected)
			}
		}); err != nil {
			t.Fatal(err)
		}
		err := deleteExpiredSessions(db, item.model, cutoff)
		callback.Remove("qpix:test-session-batch")
		var remaining int64
		if err != nil || db.Model(item.model).Count(&remaining).Error != nil || remaining != 1 || batches != 2 {
			t.Fatal("过期会话应分两批清理并保留有效会话", remaining, batches, err)
		}
	}
}
