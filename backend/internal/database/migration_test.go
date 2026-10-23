package database

import (
	"path/filepath"
	"testing"
	"time"
)

func TestUpgradeV1PreservesImage(t *testing.T) {
	db, err := Open("sqlite", filepath.Join(t.TempDir(), "v1.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	for _, model := range []any{&User{}, &Session{}, &Album{}, &APIToken{}, &Setting{}} {
		if err = db.Migrator().CreateTable(model); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Create(&User{ID: "user", Username: "legacy-user", PasswordHash: "legacy-hash"}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Migrator().DropColumn(&User{}, "UsedBytes"); err != nil {
		t.Fatal(err)
	}
	// v1 图片表不含压缩列，模拟已发布版本升级，而不是只验证新建库。
	if err = db.Exec(`CREATE TABLE images (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, album_id TEXT, name TEXT NOT NULL, storage_id TEXT NOT NULL, key TEXT NOT NULL, thumb_key TEXT, format TEXT NOT NULL, size INTEGER NOT NULL, width INTEGER NOT NULL, height INTEGER NOT NULL, state TEXT NOT NULL, created_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Migrator().CreateTable(&SchemaMigration{}); err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&SchemaMigration{Version: 1, AppliedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(`INSERT INTO images(id,user_id,name,storage_id,key,format,size,width,height,state) VALUES ('legacy','user','old.png','local','2026/old.png','png',1234,10,20,'ready')`).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(`INSERT INTO images(id,user_id,name,storage_id,key,format,size,width,height,state) VALUES ('pending','user','pending.png','local','2026/pending.png','png',20,1,1,'deleting')`).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = Migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	var row Image
	for _, name := range []string{"idx_images_owner_ready_date", "idx_images_album_ready_date", "idx_images_ready_date", "idx_images_storage_id"} {
		if !db.Migrator().HasIndex(&Image{}, name) {
			t.Fatalf("升级后缺少索引：%s", name)
		}
	}
	if err = db.First(&row, "id = ?", "legacy").Error; err != nil {
		t.Fatal(err)
	}
	if row.Size != 1234 || row.OriginalSize != 1234 || row.StorageID != "local" || row.Key != "2026/old.png" || row.Optimized || row.CompressionStatus != "original" {
		t.Fatalf("旧记录发生异常变更: %+v", row)
	}
	var owner User
	if err = db.First(&owner, "id = ?", "user").Error; err != nil || owner.UsedBytes != 1254 {
		t.Fatal("用量迁移应包含待删除图片，且重复迁移不能重复累加", owner.UsedBytes, err)
	}
}

func TestTokenExpiryBecomesNullable(t *testing.T) {
	db, err := Open("sqlite", filepath.Join(t.TempDir(), "v3.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err = db.Migrator().CreateTable(&SchemaMigration{}); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&User{}, &Session{}, &Album{}, &Image{}, &Share{}, &ShareSession{}, &AlbumMember{}, &StorageProfile{}, &AccessRule{}} {
		if err = db.Migrator().CreateTable(model); err != nil {
			t.Fatal(err)
		}
	}
	for _, version := range []int{1, 2, 3} {
		if err = db.Create(&SchemaMigration{Version: version, AppliedAt: time.Now().UTC()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Exec(`CREATE TABLE api_tokens (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT NOT NULL, hash TEXT UNIQUE NOT NULL, prefix TEXT NOT NULL, created_at DATETIME, expires_at DATETIME NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if err = db.Omit("LastUsedAt", "RevokedAt", "Authorization").Create(&APIToken{ID: "existing", UserID: "user", Name: "保留期限", Hash: "existing-hash", Prefix: "qpix_old", ExpiresAt: &expires}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = Migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Create(&APIToken{ID: "permanent", UserID: "user", Name: "永久", Hash: "permanent-hash", Prefix: "qpix_new"}).Error; err != nil {
		t.Fatal("未允许永久 Token", err)
	}
	var existing APIToken
	if err = db.First(&existing, "id = ?", "existing").Error; err != nil || existing.ExpiresAt == nil || !existing.ExpiresAt.Equal(expires) {
		t.Fatal("旧 Token 期限未保留", err)
	}
	if err = db.Create(&APIToken{ID: "duplicate", UserID: "user", Name: "重复哈希", Hash: existing.Hash, Prefix: "qpix_dup"}).Error; err == nil {
		t.Fatal("迁移丢失了令牌哈希唯一约束")
	}
}

func TestTokenPermissionMigration(t *testing.T) {
	db, err := Open("sqlite", filepath.Join(t.TempDir(), "v5.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err = Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrator().DropColumn(&APIToken{}, "Authorization"); err != nil {
		t.Fatal(err)
	}
	if err = db.Where("version = ?", 6).Delete(&SchemaMigration{}).Error; err != nil {
		t.Fatal(err)
	}
	for _, user := range []User{{ID: "admin", Username: "admin", Role: "admin", Enabled: true}, {ID: "user", Username: "user", Role: "user", Enabled: true}} {
		if err = db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"admin", "user"} {
		row := APIToken{ID: id, UserID: id, Name: "历史 Token", Hash: id + "-hash", Prefix: "qpix_old"}
		if err = db.Omit("Authorization").Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err = Migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	var admin, user APIToken
	if err = db.First(&admin, "id = ?", "admin").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.First(&user, "id = ?", "user").Error; err != nil {
		t.Fatal(err)
	}
	if admin.RevokedAt == nil || user.RevokedAt != nil || user.Hash != "user-hash" || user.ExpiresAt != nil {
		t.Fatal("旧 Token 状态或凭据被错误迁移")
	}
	if user.Authorization != `{"all_albums":true,"permissions":["upload"],"albums":[]}` {
		t.Fatal("旧用户 Token 权限被扩大或丢失")
	}
}
