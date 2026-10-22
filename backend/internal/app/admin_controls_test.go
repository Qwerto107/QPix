package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"qpix/internal/config"
	"qpix/internal/database"
	"qpix/internal/storage"
)

func newControlClient(t *testing.T, server *httptest.Server, name string) *testClient {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &testClient{t: t, server: server, client: &http.Client{Jar: jar}}
	var auth struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(client.request("POST", "/api/v1/auth/login", map[string]string{"username": name, "password": "1"}, 200), &auth); err != nil {
		t.Fatal(err)
	}
	client.csrf = auth.CSRF
	return client
}

func TestAdminControls(t *testing.T) {
	root := t.TempDir()
	db, err := database.Open("sqlite", filepath.Join(root, "controls.db"))
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
	cfg := config.Config{DBDriver: "sqlite", StorageRoot: filepath.Join(root, "uploads"), AdminUsername: "admin", AdminPassword: "1", MaxUploadBytes: 1 << 20, MaxPixels: 100}
	stores := map[string]storage.Store{"local": local}
	a, err := New(cfg, db, stores)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	login := func(name string) *testClient {
		t.Helper()
		// 每次新客户端都使用独立 CookieJar，验证角色变更会使原会话失效。
		return newControlClient(t, server, name)
	}
	admin := login("admin")
	var administrator database.User
	db.First(&administrator, "username = ?", "admin")
	create := func(name string) database.User {
		t.Helper()
		admin.request("POST", "/api/v1/users", userInput{Username: name, Enabled: true, Password: "1", ConfirmPassword: "1"}, 200)
		var user database.User
		if err := db.First(&user, "username = ?", name).Error; err != nil {
			t.Fatal(err)
		}
		return user
	}
	owner, other := create("owner"), create("other")
	member, outsider := login("owner"), login("other")
	admin.request("PUT", "/api/v1/users/"+administrator.ID, userInput{Enabled: true, Role: "user"}, 409)
	admin.request("DELETE", "/api/v1/users/"+administrator.ID, nil, 409)
	member.request("DELETE", "/api/v1/users/"+other.ID, nil, 403)
	admin.request("PUT", "/api/v1/users/"+owner.ID, userInput{Enabled: true, Role: "invalid"}, 400)
	var issued struct {
		Token string
		Item  tokenView
	}
	json.Unmarshal(member.request("POST", "/api/v1/tokens", tokenInput{Name: "个人权限"}, 201), &issued)
	admin.request("PUT", "/api/v1/users/"+owner.ID, userInput{Enabled: true, Role: "admin"}, 200)
	member.request("GET", "/api/v1/auth/me", nil, 401)
	promoted := login("owner")
	promoted.request("GET", "/api/v1/users", nil, 200)
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/users", nil)
	req.Header.Set("Authorization", "Bearer "+issued.Token)
	promoted.send(req, 401)
	admin.request("PUT", "/api/v1/users/"+owner.ID, userInput{Enabled: true, Role: "user"}, 200)
	promoted.request("GET", "/api/v1/auth/me", nil, 401)
	member = login("owner")
	var image imageView
	json.Unmarshal(member.upload(testPNG(t, 3, 2), "owned.png", "", "", 201), &image)
	var share shareView
	json.Unmarshal(member.request("POST", "/api/v1/shares", map[string]string{"kind": "image", "target_id": image.ID}, 201), &share)
	admin.request("DELETE", "/api/v1/users/"+owner.ID, nil, 409)
	member.request("DELETE", "/api/v1/shares/"+share.ID+"/record", nil, 409)
	outsider.request("DELETE", "/api/v1/shares/"+share.ID+"/record", nil, 404)
	member.request("DELETE", "/api/v1/shares/"+share.ID, nil, 200)
	db.Create(&database.ShareSession{ID: randomID(), ShareID: share.ID, ExpiresAt: time.Now().Add(time.Hour)})
	req, _ = http.NewRequest("DELETE", server.URL+"/api/v1/shares/"+share.ID+"/record", nil)
	member.send(req, 403)
	member.request("DELETE", "/api/v1/shares/"+share.ID+"/record", nil, 200)
	member.request("DELETE", "/api/v1/shares/"+share.ID+"/record", nil, 404)
	var count int64
	db.Model(&database.ShareSession{}).Where("share_id = ?", share.ID).Count(&count)
	if count != 0 {
		t.Fatal("分享会话未清理")
	}
	member.request("GET", image.URL, nil, 200)
	// 已撤销分享也会阻止账号删除，必须先明确删除记录。
	json.Unmarshal(member.request("POST", "/api/v1/shares", map[string]string{"kind": "image", "target_id": image.ID}, 201), &share)
	member.request("DELETE", "/api/v1/shares/"+share.ID, nil, 200)
	member.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{image.ID}}, 200)
	admin.request("DELETE", "/api/v1/users/"+owner.ID, nil, 409)
	admin.request("DELETE", "/api/v1/shares/"+share.ID+"/record", nil, 200)
	admin.request("DELETE", "/api/v1/users/"+owner.ID, nil, 200)
	member.request("GET", "/api/v1/auth/me", nil, 401)
	for _, model := range []any{&database.Album{}, &database.Session{}, &database.APIToken{}, &database.UploadReservation{}} {
		db.Model(model).Where("user_id = ?", owner.ID).Count(&count)
		if count != 0 {
			t.Fatal("账号附属记录未清理", model)
		}
	}
	outsider.request("GET", "/api/v1/auth/me", nil, 200)
	admin.request("PUT", "/api/v1/access/upload", ruleInput{Scope: "upload", RefererEnabled: true, Referers: []string{"example.com"}}, 400)
	admin.request("PUT", "/api/v1/access/images", ruleInput{Scope: "images", IPEnabled: true, IPs: []string{"192.0.2.1"}}, 200)
	outsider = login("other")
	outsider.request("GET", "/api/v1/albums", nil, 200)
	outsider.upload(testPNG(t, 3, 2), "independent.png", "", "", 201)

	// 运行设置原子保存，重启恢复；伪造 HTTPS 转发头不能绕过可信代理校验。
	admin.request("PATCH", "/api/v1/settings", map[string]any{"max_pixels": 999999, "public_url": "https://changed.example"}, 400)
	admin.request("PATCH", "/api/v1/settings", map[string]any{"trusted_proxies": []string{"invalid"}}, 400)
	admin.request("PATCH", "/api/v1/settings", map[string]any{"secure_cookie": true}, 400)
	body := []byte(`{"secure_cookie":true}`)
	req, _ = http.NewRequest("PATCH", server.URL+"/api/v1/settings", bytes.NewReader(body))
	req.Header.Set("X-CSRF-Token", admin.csrf)
	req.Header.Set("X-Forwarded-Proto", "https")
	admin.send(req, 400)
	admin.request("PATCH", "/api/v1/settings", map[string]any{"app_url": "https://qpix.example", "max_pixels": 2_000_000, "trusted_proxies": []string{"127.0.0.1"}, "secure_cookie": false}, 200)
	restarted, err := New(cfg, db, stores)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.appURL != "https://qpix.example" || restarted.pixelLimit() != 2_000_000 || len(restarted.trustedProxies) != 1 || restarted.publicURL != "" {
		t.Fatal("运行设置恢复或原子校验失败")
	}
	request := httptest.NewRequest("POST", "http://qpix.example/api/v1/albums", nil)
	request.Header.Set("Origin", "https://qpix.example")
	if !restarted.sameOrigin(request) {
		t.Fatal("未采用后台站点地址")
	}
	request.Header.Set("Origin", "https://other.example")
	if restarted.sameOrigin(request) {
		t.Fatal("错误 Origin 被接受")
	}
	req, _ = http.NewRequest("PATCH", server.URL+"/api/v1/settings", bytes.NewReader(body))
	req.Header.Set("X-CSRF-Token", admin.csrf)
	req.Header.Set("X-Forwarded-Proto", "https")
	admin.send(req, 200)
	if !a.cookiesSecure() {
		t.Fatal("HTTPS Cookie 未生效")
	}
	admin.request("GET", "/api/v1/auth/me", nil, 401)
}
