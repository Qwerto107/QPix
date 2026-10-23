package app

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"qpix/internal/database"
)

func TestRateLimitSettings(t *testing.T) {
	db, err := database.Open("sqlite", filepath.Join(t.TempDir(), "rates.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	if err = db.AutoMigrate(&database.Setting{}); err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, rateLimits: defaultRateLimits()}
	a.loginLimits.retry("ip", 1, time.Minute)
	a.uploadLimits.retry("user", 1, time.Minute)
	save := func(rules map[string]rateLimitRule, status int) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"rules": rules})
		w := httptest.NewRecorder()
		a.updateRateLimits(w, httptest.NewRequest("PUT", "/", bytes.NewReader(body)), principal{})
		if w.Code != status {
			t.Fatalf("保存限频策略: %d %s", w.Code, w.Body.String())
		}
	}
	rules := defaultRateLimits()
	rules["upload"] = rateLimitRule{true, 1, 120}
	save(rules, 200)
	if len(a.uploadLimits.entries) != 0 || a.loginLimits.entries["ip"].count != 1 {
		t.Fatal("保存时未仅重置发生变化的规则")
	}
	var setting database.Setting
	if err = db.Where(map[string]any{"key": "rate_limits"}).First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	reloaded := &App{}
	if err = reloaded.loadRateLimits(setting.Value); err != nil || reloaded.rateLimits["upload"] != rules["upload"] {
		t.Fatal("持久化配置无法重新加载", err)
	}
	w := httptest.NewRecorder()
	if !a.allowRate(w, "upload", "user", "超限") {
		t.Fatal("更新后首次请求被拒绝")
	}
	// 用已消耗的窗口时间验证 Retry-After，而非返回完整的配置窗口。
	a.uploadLimits.entries["user"] = window{count: 1, until: time.Now().Add(30 * time.Second)}
	w = httptest.NewRecorder()
	if a.allowRate(w, "upload", "user", "超限") || w.Code != 429 || w.Header().Get("Retry-After") != "30" {
		t.Fatal("未按窗口剩余时间返回限频响应", w.Result().Header)
	}
	save(rules, 200)
	if a.uploadLimits.entries["user"].count != 1 {
		t.Fatal("未修改的规则不应清空计数")
	}
	rules["upload"] = rateLimitRule{false, 1, 120}
	save(rules, 200)
	for i := 0; i < 3; i++ {
		if !a.allowRate(httptest.NewRecorder(), "upload", "user", "超限") {
			t.Fatal("已停用规则仍在拒绝请求")
		}
	}
	rules["upload"] = rateLimitRule{true, 0, 120}
	save(rules, 400)
	if a.rateLimits["upload"].Enabled {
		t.Fatal("无效配置改变了运行策略")
	}
	delete(rules, "login")
	save(rules, 400)
}
