package app

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qpix/internal/webui"
)

func TestUnifiedFrontend(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"index.html": "<html>QPix</html>", "assets/app-test.js": "console.log('qpix')", ".env": "private"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	handler, err := newFrontend(root)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{frontend: handler}
	for _, test := range []struct {
		method, path   string
		status         int
		content, cache string
	}{
		{"GET", "/", 200, "<html>QPix</html>", "no-cache"},
		{"GET", "/s/share-token", 200, "<html>QPix</html>", "no-cache"},
		{"GET", "/assets/app-test.js", 200, "console.log('qpix')", "public, max-age=31536000, immutable"},
		{"HEAD", "/assets/app-test.js", 200, "", "public, max-age=31536000, immutable"},
		{"GET", "/assets/missing.js", 404, "", ""},
		{"GET", "/assets/missing", 404, "", ""},
		{"GET", "/api/v1/missing", 404, "", "no-store"},
		{"GET", "/i/missing", 404, "", ""},
		{"GET", "/si/missing", 404, "", ""},
		{"GET", "/.env", 404, "", ""},
		{"GET", "/assets/", 404, "", ""},
		{"POST", "/s/share-token", 405, "", ""},
	} {
		r := httptest.NewRequest(test.method, test.path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("%s %s: %d != %d", test.method, test.path, w.Code, test.status)
		}
		if test.status == 200 && w.Body.String() != test.content {
			t.Fatalf("%s: 内容错误", test.path)
		}
		if test.cache != "" && w.Header().Get("Cache-Control") != test.cache {
			t.Fatal("缓存策略错误", test.path)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatal("缺少页面安全策略")
		}
	}
	if _, err := newFrontend(filepath.Join(root, "missing")); err == nil {
		t.Fatal("缺失前端目录不应成功启动")
	}
	if handler, err := newFrontend(""); err != nil || (handler == nil) != (webui.Files() == nil) {
		t.Fatal("未指定外置目录时应按构建标签选择内嵌页面或独立 API")
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "assets", "escape.txt")); err == nil {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "/assets/escape.txt", nil))
		if w.Code != 404 || strings.Contains(w.Body.String(), "secret") {
			t.Fatal("符号链接越界")
		}
	} else {
		t.Log("当前环境不允许创建符号链接，越界链接用例未执行")
	}
}
