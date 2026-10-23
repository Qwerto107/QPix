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

	"gorm.io/gorm"
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
			func() {
				var queries atomic.Int64
				callback := db.Callback().Query()
				if err := callback.After("gorm:query").Register("qpix:test-list-queries", func(*gorm.DB) { queries.Add(1) }); err != nil {
					t.Fatal(err)
				}
				defer callback.Remove("qpix:test-list-queries")
				if err = json.Unmarshal(c.request("GET", "/api/v1/images?album_id="+album.ID+"&q=猫", nil, 200), &listed); err != nil {
					t.Fatal(err)
				}
				// 两次会话查询加列表、总数、上传人各一次，不再按图片数量增长。
				if queries.Load() > 5 {
					t.Fatalf("图库出现逐条查询：%d 次", queries.Load())
				}
			}()
			if listed.Total != 1 || len(listed.Items) != 1 {
				t.Fatal("相册或搜索筛选错误")
			}
			c.request("PATCH", "/api/v1/images/"+uploaded.ID, map[string]string{"name": "新名称.png"}, 200)
			var owner database.User
			if err := db.First(&owner, "role = ?", "admin").Error; err != nil {
				t.Fatal(err)
			}
			c.request("DELETE", "/api/v1/albums/"+album.ID, map[string]string{"action": "move", "target_id": owner.DefaultAlbumID}, 200)
			c.request("GET", uploaded.URL, nil, 200)
			if err = json.Unmarshal(c.request("GET", "/api/v1/images?album_id="+owner.DefaultAlbumID, nil, 200), &listed); err != nil {
				t.Fatal(err)
			}
			if listed.Total != 1 || listed.Items[0].AlbumID == nil || *listed.Items[0].AlbumID != owner.DefaultAlbumID {
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
			c.request("POST", "/api/v1/tokens", map[string]string{"name": "管理员个人上传"}, 201)
			store.fail.Store(true)
			c.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{uploaded.ID}}, 200)
			c.request("GET", uploaded.URL, nil, 404)
			if used, err := a.usedBytes(db, owner.ID); err != nil || used != uploaded.Size {
				t.Fatal("物理删除失败时不能提前释放用量", used, err)
			}
			store.fail.Store(false)
			a.Maintain(context.Background())
			var remaining int64
			db.Model(&database.Image{}).Where("id = ?", uploaded.ID).Count(&remaining)
			if remaining != 0 {
				t.Fatal("待删除记录未被重试清理")
			}
			if used, err := a.usedBytes(db, owner.ID); err != nil || used != 0 {
				t.Fatal("物理删除成功后未结算用量", used, err)
			}
			c.request("GET", "/api/v1/stats", nil, 200)
			testSecurityRequests(t, a, c, fixture)
			testFeatures(t, a, c, root, fixture)
			testPasswordPolicyAndTokenExpiry(t, a, c, fixture)
			testPermissionsAndShares(t, a, c, fixture)
			c.request("PUT", "/api/v1/auth/password", map[string]string{"current_password": cfg.AdminPassword, "new_password": "new-test-password-qpix"}, 200)
			c.request("GET", "/api/v1/auth/me", nil, 401)
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

func TestThumbnailFormats(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 640, 360))
	for i := 3; i < len(source.Pix); i += 4 {
		source.Pix[i] = 255
	}
	for _, transparent := range []bool{false, true} {
		want := "jpeg"
		if transparent {
			source.Pix[3] = 0
			want = "png"
		}
		data, _, err := thumbnail(source, "png", 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		decoded, format, err := image.Decode(bytes.NewReader(data))
		if err != nil || format != want || decoded.Bounds().Dx() != 480 || decoded.Bounds().Dy() != 270 {
			t.Fatal("缩略图格式或尺寸异常", format, err)
		}
		if transparent && color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA).A == 255 {
			t.Fatal("透明缩略图被填充背景色")
		}
	}
	if data, _, err := thumbnail(image.NewNRGBA(image.Rect(0, 0, 8, 8)), "png", 1024); err != nil || len(data) != 0 {
		t.Fatal("小图不应重复生成缩略图", err)
	}
	if data, _, err := thumbnail(image.NewNRGBA(image.Rect(0, 0, 8, 8)), "png", 1<<20); err != nil || len(data) == 0 {
		t.Fatal("小尺寸大文件仍应生成缩略图", err)
	}
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
