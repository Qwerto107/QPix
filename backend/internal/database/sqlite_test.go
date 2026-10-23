package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteReconnectPreservesSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "图库 # 100%.db")
	db, err := Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("URI 编码改变了数据库路径：%v", err)
	}
	// 模拟连接池丢弃坏连接，接下来的查询必须重新建连。
	conn, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = conn.Raw(func(any) error { return driver.ErrBadConn })
	conn.Close()
	if !errors.Is(err, driver.ErrBadConn) {
		t.Fatal("未能模拟坏连接", err)
	}
	for pragma, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 5000, "synchronous": 2, "wal_autocheckpoint": 1000} {
		var got int
		if err := db.Raw("PRAGMA " + pragma).Row().Scan(&got); err != nil || got != want {
			t.Fatalf("重建后 %s=%d，期望 %d：%v", pragma, got, want, err)
		}
	}
	var mode string
	if err := db.Raw("PRAGMA journal_mode").Row().Scan(&mode); err != nil || mode != "wal" {
		t.Fatal("重建后 WAL 模式丢失", mode, err)
	}
}

func TestSQLiteMaintenanceWithExternalReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maintenance.db")
	db, err := Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := db.Exec("CREATE TABLE checkpoint_probe (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO checkpoint_probe VALUES (1)").Error; err != nil {
		t.Fatal(err)
	}
	uri, err := sqliteDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := reader.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM checkpoint_probe").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO checkpoint_probe VALUES (2)").Error; err != nil {
		t.Fatal(err)
	}
	// 读者停留在旧快照时不能等待其退出；否则会占满应用的唯一连接。
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := MaintainSQLite(db.WithContext(ctx)); err != nil || ctx.Err() != nil {
		t.Fatal("维护被外部读事务阻塞", err, ctx.Err())
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := MaintainSQLite(db); err != nil {
		t.Fatal("读事务结束后维护失败", err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM checkpoint_probe").Row().Scan(&count); err != nil || count != 2 {
		t.Fatal("维护改变了数据", count, err)
	}
}

func TestSQLiteTimeMigration(t *testing.T) {
	db, err := Open("sqlite", filepath.Join(t.TempDir(), "times.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&SchemaMigration{}, "version = ?", 9).Error; err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	local := time.FixedZone("CST", 8*3600)
	for i, at := range []time.Time{today.Add(-time.Hour), today.Add(time.Hour)} {
		row := Image{ID: fmt.Sprint(i), UserID: "owner", State: "ready", CreatedAt: at}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		// 直接写入旧驱动格式，模拟迁移前的本地时区文本。
		if err := db.Model(&row).UpdateColumn("created_at", at.In(local).String()).Error; err != nil {
			t.Fatal(err)
		}
	}
	expired := time.Now().UTC().Add(-time.Hour)
	token := APIToken{ID: "token", UserID: "owner", Name: "legacy", Hash: "hash"}
	if err := db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&token).UpdateColumn("expires_at", expired.In(local).String()).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Model(&Image{}).Where("created_at >= ?", today).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("UTC 今日统计错误", count, err)
	}
	if err := db.First(&token, "id = ?", token.ID).Error; err != nil || token.ExpiresAt == nil || !token.ExpiresAt.Equal(expired) || token.RevokedAt != nil {
		t.Fatal("迁移改变了到期时间或 NULL 撤销状态", token, err)
	}
	if err := db.Model(&APIToken{}).Where("expires_at > ?", time.Now().UTC()).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("迁移后已到期 Token 被视为有效", count, err)
	}
	var raw string
	if err := db.Model(&Image{}).Select("CAST(created_at AS TEXT)").Where("id = ?", "0").Scan(&raw).Error; err != nil || raw != today.Add(-time.Hour).String() {
		t.Fatal("旧时间未规范为 UTC 文本", raw, err)
	}
	user := User{ID: "owner", Username: "owner", PasswordHash: "hash"}
	if err := db.Create(&user).Error; err != nil || user.CreatedAt.Location() != time.UTC {
		t.Fatal("新记录未使用 UTC", user.CreatedAt, err)
	}
}
