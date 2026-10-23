package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"qpix/internal/database"
	"qpix/internal/storage"
)

func TestLimiterCapacityAndIsolation(t *testing.T) {
	a := &App{}
	a.loginLimits.allow("known", 2, time.Minute)
	for i := 0; i < 10000; i++ {
		a.loginLimits.allow(fmt.Sprint(i), 2, time.Minute)
		a.shareLimits.allow(fmt.Sprint(i), 2, time.Minute)
	}
	if !a.loginLimits.allow("known", 2, time.Minute) || a.loginLimits.allow("known", 2, time.Minute) {
		t.Fatal("容量满时应保留已有来源的剩余次数与限制")
	}
	if !a.passwordLimits.allow("user", 1, time.Minute) || a.passwordLimits.allow("user", 1, time.Minute) {
		t.Fatal("分享和登录限流不应占用改密限流容量")
	}
	// 过期记录应回收，不因历史来源永久占满容量。
	a.loginLimits.entries["known"] = window{until: time.Now().Add(-time.Second)}
	a.loginLimits.nextCleanup = time.Time{}
	if !a.loginLimits.allow("new", 1, time.Minute) {
		t.Fatal("未回收过期限流记录")
	}
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "[2001:db8:1:2::1]:1234"
	key := a.limiterIP(r)
	r.RemoteAddr = "[2001:db8:1:2::ffff]:5678"
	if key != a.limiterIP(r) || key != "2001:db8:1:2::/64" {
		t.Fatal("IPv6 来源未按 /64 聚合")
	}
}

type pausedReader struct {
	io.Reader
	started chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (r *pausedReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started); <-r.resume })
	return r.Reader.Read(p)
}

func waitSecurityRequest(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("请求未在预期时间内继续执行")
	}
}

func securityUpload(t *testing.T, c *testClient, fixture []byte, token string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "security.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(fixture); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", c.server.URL+"/api/v1/images", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	} else {
		r.Header.Set("X-CSRF-Token", c.csrf)
		u, _ := url.Parse(c.server.URL)
		for _, cookie := range c.client.Jar.Cookies(u) {
			r.AddCookie(cookie)
		}
	}
	return r
}

type pausedStore struct {
	storage.Store
	started   chan struct{}
	resume    chan struct{}
	once      sync.Once
	pauseOpen bool
}

func (s *pausedStore) Put(ctx context.Context, key string, r io.Reader) error {
	if !s.pauseOpen {
		s.once.Do(func() { close(s.started); <-s.resume })
	}
	return s.Store.Put(ctx, key, r)
}

func (s *pausedStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if s.pauseOpen {
		s.once.Do(func() { close(s.started); <-s.resume })
	}
	return s.Store.Open(ctx, key)
}

func testSecurityRequests(t *testing.T, a *App, admin *testClient, fixture []byte) {
	t.Helper()
	handler := a.Handler()
	var uploaded imageView
	// 请求体保持未读完时，匿名写请求仍须立即拒绝，管理操作不应等待它。
	for _, login := range []bool{true, false} {
		r := securityUpload(t, admin, fixture, "")
		if login {
			r = httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"missing-security-user","password":"1"}`))
		}
		paused := &pausedReader{Reader: r.Body, started: make(chan struct{}), resume: make(chan struct{})}
		r.Body = io.NopCloser(paused)
		w, done := httptest.NewRecorder(), make(chan struct{})
		go func() { defer close(done); handler.ServeHTTP(w, r) }()
		func() {
			defer close(paused.resume)
			waitSecurityRequest(t, paused.started)
			if !a.policyMu.TryLock() {
				t.Fatal("慢速请求体持有全局策略锁")
			}
			a.policyMu.Unlock()
			if !login && len(a.processing) != 0 {
				t.Fatal("慢速上传提前占用了图片处理名额")
			}
			denied := httptest.NewRecorder()
			handler.ServeHTTP(denied, httptest.NewRequest("POST", "/api/v1/albums", nil))
			if denied.Code != 401 {
				t.Fatal("匿名写请求未被拒绝")
			}
		}()
		waitSecurityRequest(t, done)
		want := 201
		if login {
			want = 401
		}
		if w.Code != want {
			t.Fatalf("请求恢复后状态异常：%d %s", w.Code, w.Body.String())
		}
		if !login {
			if err := json.Unmarshal(w.Body.Bytes(), &uploaded); err != nil {
				t.Fatal(err)
			}
		}
	}
	// 密码分享的初次查询不持有策略锁，计算后须在锁内重新检查目标。
	func() {
		var phases atomic.Int32
		callback := a.db.Callback().Query()
		if err := callback.After("gorm:query").Register("qpix:test-share-policy", func(tx *gorm.DB) {
			if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "Image" {
				return
			}
			phase := phases.Add(1)
			free := a.policyMu.TryLock()
			if phase == 1 {
				if !free {
					t.Error("分享准备阶段仍持有全局锁")
					return
				}
				// 模拟密码计算期间另一请求删除目标，旧的图片快照不能用于提交。
				err := a.db.Model(&database.Image{}).Where("id = ?", uploaded.ID).UpdateColumn("state", "deleting").Error
				a.policyMu.Unlock()
				if err != nil {
					t.Error(err)
				}
			} else if free {
				a.policyMu.Unlock()
				t.Error("分享提交前未在策略锁内重新检查目标")
			}
		}); err != nil {
			t.Fatal(err)
		}
		defer callback.Remove("qpix:test-share-policy")
		defer a.db.Model(&database.Image{}).Where("id = ?", uploaded.ID).UpdateColumn("state", "ready")
		admin.request("POST", "/api/v1/shares", map[string]string{"kind": "image", "target_id": uploaded.ID, "password": "secret"}, 404)
		if phases.Load() != 2 {
			t.Fatal("密码计算后未重新查询分享目标", phases.Load())
		}
	}()
	t.Run("upload-temporary-storage-error", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing")
		for _, name := range []string{"TMP", "TEMP", "TMPDIR"} {
			t.Setenv(name, missing)
		}
		a.settingsMu.Lock()
		previous := a.maxUploadBytes
		a.maxUploadBytes = 3 << 20
		a.settingsMu.Unlock()
		defer func() { a.settingsMu.Lock(); a.maxUploadBytes = previous; a.settingsMu.Unlock() }()
		r := securityUpload(t, admin, make([]byte, 2<<20), "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 500 || !strings.Contains(w.Body.String(), "临时存储") || strings.Contains(w.Body.String(), os.TempDir()) {
			t.Fatal("临时文件创建失败应返回明确的 500，且不泄露服务器路径", w.Code, w.Body.String())
		}
	})
	// 匿名图片读取阻塞在存储时，不能阻止管理员写入。
	func() {
		a.settingsMu.Lock()
		original := a.stores["local"]
		paused := &pausedStore{Store: original, started: make(chan struct{}), resume: make(chan struct{}), pauseOpen: true}
		a.stores["local"] = paused
		a.settingsMu.Unlock()
		resume := sync.OnceFunc(func() { close(paused.resume) })
		defer resume()
		defer func() {
			a.settingsMu.Lock()
			a.stores["local"] = original
			a.settingsMu.Unlock()
		}()
		cached, checked := httptest.NewRecorder(), make(chan struct{})
		var queries atomic.Int64
		callback := a.db.Callback().Query()
		if err := callback.After("gorm:query").Register("qpix:test-image-queries", func(*gorm.DB) { queries.Add(1) }); err != nil {
			t.Fatal(err)
		}
		defer callback.Remove("qpix:test-image-queries")
		conditional := httptest.NewRequest("GET", uploaded.URL, nil)
		conditional.Header.Set("If-None-Match", `"old", W/"`+uploaded.ID+"."+uploaded.Format+`"`)
		go func() { defer close(checked); handler.ServeHTTP(cached, conditional) }()
		waitSecurityRequest(t, checked)
		if queries.Load() != 1 {
			t.Fatalf("匿名图片与相册应合并查询：%d 次", queries.Load())
		}
		if cached.Code != 304 || cached.Header().Get("Cache-Control") != "private, no-cache, must-revalidate" {
			t.Fatal("公开直链协商缓存未生效", cached.Code, cached.Header())
		}
		select {
		case <-paused.started:
			t.Fatal("304 请求不应读取存储对象")
		default:
		}
		w, done := httptest.NewRecorder(), make(chan struct{})
		go func() {
			defer close(done)
			handler.ServeHTTP(w, httptest.NewRequest("GET", uploaded.URL, nil))
		}()
		func() {
			defer resume()
			waitSecurityRequest(t, paused.started)
			if !a.policyMu.TryLock() {
				t.Fatal("匿名图片存储读取持有全局策略锁")
			}
			a.policyMu.Unlock()
		}()
		waitSecurityRequest(t, done)
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), fixture) {
			t.Fatal("图片读取恢复后内容异常")
		}
	}()
	// 存储写入尚未结束时撤销 Token，提交阶段必须拒绝并清理已写入对象。
	var token struct {
		Token string
		Item  database.APIToken
	}
	if err := json.Unmarshal(admin.request("POST", "/api/v1/tokens", map[string]string{"name": "并发撤销验证"}, 201), &token); err != nil {
		t.Fatal(err)
	}
	a.settingsMu.Lock()
	original := a.stores["local"]
	paused := &pausedStore{Store: original, started: make(chan struct{}), resume: make(chan struct{})}
	a.stores["local"] = paused
	a.settingsMu.Unlock()
	defer func() {
		a.settingsMu.Lock()
		a.stores["local"] = original
		a.settingsMu.Unlock()
	}()
	w, done := httptest.NewRecorder(), make(chan struct{})
	r := securityUpload(t, admin, fixture, token.Token)
	go func() { defer close(done); handler.ServeHTTP(w, r) }()
	func() {
		defer close(paused.resume)
		waitSecurityRequest(t, paused.started)
		if !a.policyMu.TryLock() {
			t.Fatal("存储网络 I/O 持有全局策略锁")
		}
		a.policyMu.Unlock()
		if !a.settingsMu.TryLock() {
			t.Fatal("上传网络 I/O 持有全局设置锁")
		}
		a.settingsMu.Unlock()
		previousTimeout := admin.client.Timeout
		admin.client.Timeout = 5 * time.Second
		defer func() { admin.client.Timeout = previousTimeout }()
		admin.request("DELETE", "/api/v1/tokens/"+token.Item.ID, nil, 200)
	}()
	waitSecurityRequest(t, done)
	if w.Code != 401 {
		t.Fatalf("已撤销 Token 的在途上传仍然提交：%d %s", w.Code, w.Body.String())
	}
	var remaining int64
	if err := a.db.Model(&database.UploadReservation{}).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatal("上传失败未释放容量预占", remaining, err)
	}
}
