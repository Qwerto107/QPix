package app

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"qpix/internal/config"
	"qpix/internal/database"
	"qpix/internal/storage"
)

func TestPanelUploadLimit(t *testing.T) {
	root := t.TempDir()
	db, err := database.Open("sqlite", filepath.Join(root, "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	local, err := storage.NewLocal(filepath.Join(root, "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{AdminUsername: "admin", AdminPassword: "1", StorageRoot: filepath.Join(root, "uploads"), MaxUploadBytes: 1 << 20, MaxPixels: 100}
	stores := map[string]storage.Store{"local": local}
	a, err := New(cfg, db, stores)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	admin := &testClient{t: t, server: server, client: &http.Client{Jar: jar}}
	var auth struct {
		CSRF string `json:"csrf_token"`
	}
	if err = json.Unmarshal(admin.request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "1"}, 200), &auth); err != nil {
		t.Fatal(err)
	}
	admin.csrf = auth.CSRF
	var user struct{ ID string }
	if err = json.Unmarshal(admin.request("POST", "/api/v1/users", userInput{Username: "member", Enabled: true, Password: "1", ConfirmPassword: "1"}, 200), &user); err != nil {
		t.Fatal(err)
	}
	member := memberClient(t, server, "member", "1")
	member.request("PATCH", "/api/v1/settings", map[string]int64{"max_upload_bytes": 2 << 20}, 403)
	for _, limit := range []int64{0, -1, (1 << 20) - 1, (100 << 20) + 1} {
		admin.request("PATCH", "/api/v1/settings", map[string]int64{"max_upload_bytes": limit}, 400)
	}
	admin.request("PATCH", "/api/v1/settings", map[string]any{}, 400)
	admin.request("PATCH", "/api/v1/settings", map[string]any{"public_url": "https://img.example.com"}, 200)
	fixture := append(testPNG(t, 3, 2), make([]byte, 1<<20)...)
	member.upload(fixture, "too-large.png", "", "", 413)
	admin.request("PATCH", "/api/v1/settings", map[string]int64{"max_upload_bytes": 2 << 20}, 200)
	checkLimit := func(want int64) {
		t.Helper()
		var options struct {
			Limit int64 `json:"max_upload_bytes"`
		}
		if err := json.Unmarshal(member.request("GET", "/api/v1/upload-options", nil, 200), &options); err != nil || options.Limit != want {
			t.Fatal("用户有效上限没有同步", options.Limit, want, err)
		}
	}
	checkLimit(2 << 20)
	var token struct{ Token string }
	if err = json.Unmarshal(member.request("POST", "/api/v1/tokens", map[string]string{"name": "上传限制验证"}, 201), &token); err != nil {
		t.Fatal(err)
	}
	anonymous := &testClient{t: t, server: server, client: &http.Client{}}
	var image imageView
	if err = json.Unmarshal(anonymous.upload(fixture, "allowed.png", "", token.Token, 201), &image); err != nil {
		t.Fatal(err)
	}
	admin.request("PUT", "/api/v1/users/"+user.ID, userInput{Enabled: true, MaxFileBytes: 2 << 20}, 200)
	admin.request("PATCH", "/api/v1/settings", map[string]int64{"max_upload_bytes": 1 << 20}, 200)
	checkLimit(1 << 20)
	member.upload(fixture, "cookie-limit.png", "", "", 413)
	anonymous.upload(fixture, "token-limit.png", "", token.Token, 413)
	member.request("GET", "/i/"+image.ID+"."+image.Format, nil, 200)
	admin.request("PUT", "/api/v1/users/"+user.ID, userInput{Enabled: true, MaxFileBytes: 2 << 20}, 400)
	admin.request("PATCH", "/api/v1/settings", map[string]int64{"max_upload_bytes": 3 << 20}, 200)
	checkLimit(2 << 20)
	// 重建应用时数据库值优先于启动默认；无关字段更新与无效请求均不能覆盖已有配置。
	admin.request("PATCH", "/api/v1/settings", map[string]any{"public_url": "https://other.example.com", "max_upload_bytes": 0}, 400)
	restarted, err := New(cfg, db, stores)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.uploadLimit() != 3<<20 || restarted.publicURL != "https://img.example.com" {
		t.Fatal("上传上限或独立域名未正确持久化")
	}
}
