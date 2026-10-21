package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm/schema"
	"qpix/internal/config"
	"qpix/internal/database"
	"qpix/internal/storage"
)

type testClient struct {
	t      *testing.T
	client *http.Client
	server *httptest.Server
	csrf   string
}

func (c *testClient) request(method, path string, value any, want int) []byte {
	c.t.Helper()
	var body io.Reader
	if value != nil {
		encoded, err := json.Marshal(value)
		if err != nil {
			c.t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, c.server.URL+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	return c.send(req, want)
}

func (c *testClient) send(req *http.Request, want int) []byte {
	c.t.Helper()
	res, err := c.client.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	if res.StatusCode != want {
		c.t.Fatalf("%s %s: got %d, want %d: %s", req.Method, req.URL.Path, res.StatusCode, want, body)
	}
	return body
}

func (c *testClient) upload(content []byte, name, album, token string, want int, options ...map[string]string) []byte {
	c.t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err = part.Write(content); err != nil {
		c.t.Fatal(err)
	}
	if album != "" {
		if err = writer.WriteField("album_id", album); err != nil {
			c.t.Fatal(err)
		}
	}
	for _, fields := range options {
		for key, value := range fields {
			if err = writer.WriteField(key, value); err != nil {
				c.t.Fatal(err)
			}
		}
	}
	if err = writer.Close(); err != nil {
		c.t.Fatal(err)
	}
	req, err := http.NewRequest("POST", c.server.URL+"/api/v1/images", &buffer)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	return c.send(req, want)
}

func TestLifecycle(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			dsn := os.Getenv("QPIX_TEST_" + strings.ToUpper(driver) + "_DSN")
			if driver != "sqlite" && dsn == "" {
				t.Skip("未配置对应数据库的集成测试连接")
			}
			root := t.TempDir()
			if driver == "sqlite" {
				dsn = filepath.Join(root, "test.db")
			}
			db, err := database.Open(driver, dsn)
			if err != nil {
				t.Fatal(err)
			}
			// 测试使用独立表前缀，避免清理到连接中已有的业务表。
			db.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "qpix_test_" + randomHex(4) + "_"}
			pool, _ := db.DB()
			t.Cleanup(func() {
				for _, model := range []any{&database.ShareSession{}, &database.Share{}, &database.UploadReservation{}, &database.AlbumMember{}, &database.Image{}, &database.Album{}, &database.APIToken{}, &database.Session{}, &database.User{}, &database.Setting{}, &database.AccessRule{}, &database.StorageProfile{}, &database.SchemaMigration{}} {
					if err := db.Migrator().DropTable(model); err != nil {
						t.Error(err)
					}
				}
				pool.Close()
			})
			if err = database.Migrate(db); err != nil {
				t.Fatal(err)
			}
			if err = database.Migrate(db); err != nil {
				t.Fatalf("重复执行迁移失败：%v", err)
			}
			local, err := storage.NewLocal(filepath.Join(root, "uploads"))
			if err != nil {
				t.Fatal(err)
			}
			store := &failingStore{Store: local}
			cfg := config.Config{DBDriver: driver, StorageRoot: filepath.Join(root, "uploads"), AdminUsername: "admin", AdminPassword: "test-password-for-qpix", MaxUploadBytes: 4096, MaxPixels: 100}
			a, err := New(cfg, db, map[string]storage.Store{"local": store})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(a.Handler())
			defer server.Close()
			jar, _ := cookiejar.New(nil)
			c := &testClient{t: t, client: &http.Client{Jar: jar}, server: server}
			c.request("GET", "/api/v1/health", nil, 200)
			c.request("GET", "/api/v1/images", nil, 401)
			login := c.request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": cfg.AdminPassword}, 200)
			var auth struct {
				CSRF string        `json:"csrf_token"`
				User database.User `json:"user"`
			}
			if err = json.Unmarshal(login, &auth); err != nil {
				t.Fatal(err)
			}
			if auth.User.Role != "admin" {
				t.Fatal("统一登录未识别管理员角色")
			}
			c.request("POST", "/api/v1/albums", map[string]string{"name": "缺少 CSRF"}, 403)
			c.csrf = auth.CSRF
			cross, _ := http.NewRequest("POST", server.URL+"/api/v1/albums", strings.NewReader(`{"name":"非法来源"}`))
			cross.Header.Set("Origin", "https://untrusted.example")
			cross.Header.Set("X-CSRF-Token", c.csrf)
			c.send(cross, 403)
			var album database.Album
			if err = json.Unmarshal(c.request("POST", "/api/v1/albums", map[string]string{"name": "测试相册"}, 201), &album); err != nil {
				t.Fatal(err)
			}
			c.request("PATCH", "/api/v1/albums/"+album.ID, map[string]string{"name": "新相册名称"}, 200)
			foreignAlbum := database.Album{ID: randomID(), UserID: "another-user", Name: "其他人的相册"}
			if err = db.Create(&foreignAlbum).Error; err != nil {
				t.Fatal(err)
			}
			fixture := testPNG(t, 3, 2)
			c.upload(fixture, "photo.png", randomID(), "", 400)
			c.upload([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), "fake.png", "", "", 415)
			c.upload(make([]byte, 5000), "large.png", "", "", 413)
			c.upload(testPNG(t, 11, 10), "pixels.png", "", "", 413)
			var uploaded imageView
			if err = json.Unmarshal(c.upload(fixture, "猫咪.png", album.ID, "", 201), &uploaded); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(c.request("GET", uploaded.URL, nil, 200), fixture) {
				t.Fatal("原图内容发生变化")
			}
			c.request("GET", uploaded.ThumbnailURL, nil, 200)
			c.request("GET", "/i/"+uploaded.ID+".svg", nil, 404)
			var listed struct {
				Items []imageView
				Total int64
			}
			if err = json.Unmarshal(c.request("GET", "/api/v1/images?album_id="+album.ID+"&q=猫", nil, 200), &listed); err != nil {
				t.Fatal(err)
			}
			if listed.Total != 1 || len(listed.Items) != 1 {
				t.Fatal("相册或搜索筛选错误")
			}
			c.request("PATCH", "/api/v1/images/"+uploaded.ID, map[string]string{"name": "新名称.png"}, 200)
			c.request("DELETE", "/api/v1/albums/"+album.ID, nil, 200)
			c.request("GET", uploaded.URL, nil, 200)
			if err = json.Unmarshal(c.request("GET", "/api/v1/images?album_id=unfiled", nil, 200), &listed); err != nil {
				t.Fatal(err)
			}
			if listed.Total != 1 || listed.Items[0].AlbumID != nil {
				t.Fatal("删除相册未正确保留图片")
			}
			c.request("PATCH", "/api/v1/settings", map[string]string{"public_url": "javascript:alert(1)"}, 400)
			c.request("PATCH", "/api/v1/settings", map[string]string{"public_url": "https://img.example.com"}, 200)
			var configuration map[string]any
			if err = json.Unmarshal(c.request("GET", "/api/v1/settings", nil, 200), &configuration); err != nil {
				t.Fatal(err)
			}
			if configuration["public_url"] != "https://img.example.com" {
				t.Fatal("域名设置未保存")
			}
			c.request("PATCH", "/api/v1/settings", map[string]string{"public_url": ""}, 200)
			var token struct {
				Token string
				Item  database.APIToken
			}
			if err = json.Unmarshal(c.request("POST", "/api/v1/tokens", map[string]any{"name": "客户端", "expires_at": time.Now().UTC().AddDate(0, 0, 30)}, 201), &token); err != nil {
				t.Fatal(err)
			}
			if token.Token == "" {
				t.Fatal("未生成上传 Token")
			}
			var stored database.APIToken
			if err = db.First(&stored, "id = ?", token.Item.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Hash == token.Token || stored.Hash != digest(token.Token) {
				t.Fatal("Token 未正确哈希存储")
			}
			anonymous := &testClient{t: t, client: &http.Client{}, server: server}
			anonymous.upload(fixture, "token.png", "", token.Token, 201)
			req, _ := http.NewRequest("GET", server.URL+"/api/v1/images", nil)
			req.Header.Set("Authorization", "Bearer "+token.Token)
			anonymous.send(req, 401)
			c.request("DELETE", "/api/v1/tokens/"+token.Item.ID, nil, 200)
			anonymous.upload(fixture, "revoked.png", "", token.Token, 401)
			store.fail.Store(true)
			c.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{uploaded.ID}}, 200)
			c.request("GET", uploaded.URL, nil, 404)
			store.fail.Store(false)
			a.Maintain(context.Background())
			var remaining int64
			db.Model(&database.Image{}).Where("id = ?", uploaded.ID).Count(&remaining)
			if remaining != 0 {
				t.Fatal("待删除记录未被重试清理")
			}
			c.request("GET", "/api/v1/stats", nil, 200)
			testFeatures(t, a, c, root, fixture)
			testPasswordPolicyAndTokenExpiry(t, a, c, fixture)
			testPermissionsAndShares(t, a, c, fixture)
			if err = json.Unmarshal(c.request("POST", "/api/v1/tokens", map[string]any{"name": "改密撤销测试"}, 201), &token); err != nil {
				t.Fatal(err)
			}
			c.request("PUT", "/api/v1/auth/password", map[string]string{"current_password": cfg.AdminPassword, "new_password": "new-test-password-qpix"}, 200)
			c.request("GET", "/api/v1/auth/me", nil, 401)
			anonymous.upload(fixture, "old-token.png", "", token.Token, 401)
			if err = json.Unmarshal(c.request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "new-test-password-qpix"}, 200), &auth); err != nil {
				t.Fatal(err)
			}
			c.csrf = auth.CSRF
			c.request("POST", "/api/v1/auth/logout", nil, 200)
			c.request("GET", "/api/v1/auth/me", nil, 401)
		})
	}
}

type failingStore struct {
	storage.Store
	fail atomic.Bool
}

func (s *failingStore) Delete(ctx context.Context, key string) error {
	if s.fail.Load() {
		return errors.New("模拟存储删除失败")
	}
	return s.Store.Delete(ctx, key)
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			picture.Set(x, y, color.NRGBA{R: uint8(x * 30), G: uint8(y * 50), B: 120, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, picture); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestBootstrapRequiresPassword(t *testing.T) {
	root := t.TempDir()
	db, err := database.Open("sqlite", filepath.Join(root, "bootstrap.db"))
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
	if _, err = New(config.Config{}, db, map[string]storage.Store{"local": local}); err == nil {
		t.Fatal("不应允许使用空初始密码")
	}
	if _, err = New(config.Config{AdminUsername: "admin", AdminPassword: strings.Repeat("汉", 25)}, db, map[string]storage.Store{"local": local}); err == nil {
		t.Fatal("不应允许超过 bcrypt 字节上限的密码")
	}
	if _, err = New(config.Config{AdminUsername: "admin", AdminPassword: "1", StorageRoot: filepath.Join(root, "uploads")}, db, map[string]storage.Store{"local": local}); err != nil {
		t.Fatal("非空短密码应允许初始化", err)
	}
}
