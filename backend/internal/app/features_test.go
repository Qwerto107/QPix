package app

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"qpix/internal/database"
	"qpix/internal/storage"
)

// 与原有生命周期共用数据库矩阵，CI 中覆盖 SQLite、PostgreSQL、MySQL。
func testFeatures(t *testing.T, a *App, c *testClient, root string, fixture []byte) {
	t.Helper()
	var profile profileView
	input := storageInput{Name: "第二存储", Kind: "local", Enabled: true, Root: filepath.Join(root, "second")}
	if err := json.Unmarshal(c.request("POST", "/api/v1/storages", input, 201), &profile); err != nil {
		t.Fatal(err)
	}
	c.request("POST", "/api/v1/storages/"+profile.ID+"/test", nil, 200)
	c.request("POST", "/api/v1/storages/"+profile.ID+"/default", nil, 200)
	var uploaded imageView
	if err := json.Unmarshal(c.upload(fixture, "multi.png", "", "", 201), &uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.StorageID != profile.ID || uploaded.OriginalSize != int64(len(fixture)) {
		t.Fatal("上传未采用默认存储或原始大小错误")
	}
	c.request("DELETE", "/api/v1/storages/"+profile.ID, nil, 409)
	input.Root = filepath.Join(root, "other")
	c.request("PUT", "/api/v1/storages/"+profile.ID, input, 409)
	input.Root = profile.Root
	input.Enabled = false
	c.request("PUT", "/api/v1/storages/"+profile.ID, input, 409)
	c.request("POST", "/api/v1/storages/local/default", nil, 200)
	c.request("PUT", "/api/v1/storages/"+profile.ID, input, 200)
	c.upload(fixture, "disabled.png", "", "", 400, map[string]string{"storage_id": profile.ID})
	if !bytes.Equal(c.request("GET", uploaded.URL, nil, 200), fixture) {
		t.Fatal("停用存储影响了已有图片")
	}
	c.request("PUT", "/api/v1/processing", processingSettings{Mode: "webp", Quality: 82}, 200)
	c.upload(fixture, "bad.png", "", "", 400, map[string]string{"compression": "invalid"})
	var compressed imageView
	if err := json.Unmarshal(c.upload(fixture, "compressed.png", "", "", 201), &compressed); err != nil {
		t.Fatal(err)
	}
	if compressed.CompressionStatus == "original" || compressed.OriginalSize != int64(len(fixture)) || compressed.Size > compressed.OriginalSize {
		t.Fatal("默认处理状态错误")
	}
	var explicit imageView
	if err := json.Unmarshal(c.upload(fixture, "original.png", "", "", 201, map[string]string{"compression": "none", "quality": "20"}), &explicit); err != nil {
		t.Fatal(err)
	}
	if explicit.CompressionStatus != "original" || !bytes.Equal(c.request("GET", explicit.URL, nil, 200), fixture) {
		t.Fatal("显式原文件选项被默认设置覆盖")
	}
	c.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{explicit.ID}}, 200)
	for _, quality := range []string{"", "0", "101", "1.5", "oops"} {
		c.upload(fixture, "bad-quality.png", "", "", 400, map[string]string{"compression": "preserve", "quality": quality})
	}
	c.request("PUT", "/api/v1/processing", processingSettings{Mode: "none", Quality: 82}, 200)
	for _, mode := range []string{"preserve", "webp"} {
		if err := json.Unmarshal(c.upload(fixture, "override.png", "", "", 201, map[string]string{"compression": mode, "quality": "20"}), &explicit); err != nil {
			t.Fatal(err)
		}
		if explicit.CompressionStatus == "original" {
			t.Fatal("显式压缩方式被全局关闭覆盖")
		}
		c.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{explicit.ID}}, 200)
	}
	// 经真实上传链路检查画质覆盖默认值，避免表单参数被接收但未应用。
	qualitySource := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			qualitySource.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 30), G: uint8(y * 30), B: uint8(x * y * 5), A: uint8((x + y) * 15)})
		}
	}
	var qualityInput bytes.Buffer
	qualityEncoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := qualityEncoder.Encode(&qualityInput, qualitySource); err != nil {
		t.Fatal(err)
	}
	// 生命周期测试限制为 100 像素；填充尾部使微型样本也有足够空间观察高画质编码。
	qualityData := append(bytes.Clone(qualityInput.Bytes()), make([]byte, 1024)...)
	for _, mode := range []string{"preserve", "webp"} {
		var outputs [][]byte
		for _, quality := range []string{"20", "95"} {
			if err := json.Unmarshal(c.upload(qualityData, "quality.png", "", "", 201, map[string]string{"compression": mode, "quality": quality}), &explicit); err != nil {
				t.Fatal(err)
			}
			if !explicit.Optimized {
				t.Fatal("合成样本应得到压缩结果", explicit.CompressionStatus)
			}
			outputs = append(outputs, c.request("GET", explicit.URL, nil, 200))
			c.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{explicit.ID}}, 200)
		}
		if bytes.Equal(outputs[0], outputs[1]) {
			t.Fatal("上传画质未覆盖默认值", mode)
		}
	}
	c.request("PUT", "/api/v1/processing", processingSettings{Mode: "webp", Quality: 0}, 400)
	blocked := ruleInput{Scope: "admin", IPEnabled: true, IPs: []string{"192.0.2.1"}}
	c.request("PUT", "/api/v1/access/admin", blocked, 400)
	blocked.Scope = "upload"
	c.request("PUT", "/api/v1/access/upload", blocked, 200)
	c.upload(fixture, "blocked.png", "", "", 403)
	c.request("GET", "/api/v1/images", nil, 200)
	c.request("GET", uploaded.URL, nil, 200)
	c.request("PUT", "/api/v1/access/upload", ruleInput{Scope: "upload"}, 200)
	imageRule := ruleInput{Scope: "images", RefererEnabled: true, Referers: []string{"*.example.com"}}
	c.request("PUT", "/api/v1/access/images", imageRule, 200)
	c.request("GET", uploaded.URL, nil, 403)
	req, _ := http.NewRequest("HEAD", c.server.URL+uploaded.URL, nil)
	req.Header.Set("Referer", "https://images.example.com/view")
	response, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatal("受限图片缓存或 HEAD 响应错误")
	}
	req, _ = http.NewRequest("GET", c.server.URL+uploaded.URL, nil)
	req.Header.Set("If-None-Match", `"`+uploaded.ID+"."+uploaded.Format+`"`)
	c.send(req, 403)
	c.request("PUT", "/api/v1/access/images", ruleInput{Scope: "images"}, 200)
	// 凭据不通过 API 回显，并在保存后保持加密；空字段更新不得清除凭据。
	var cloudMu sync.Mutex
	objects := map[string][]byte{}
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudMu.Lock()
		defer cloudMu.Unlock()
		switch r.Method {
		case "PUT":
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			objects[r.URL.Path] = data
		case "GET":
			data, exists := objects[r.URL.Path]
			if !exists {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(data)
		case "DELETE":
			delete(objects, r.URL.Path)
			w.WriteHeader(204)
		}
	}))
	defer cloud.Close()
	s3input := storageInput{Name: "S3", Kind: "s3", Enabled: true, Endpoint: cloud.URL, Region: "us-east-1", Bucket: "test", Prefix: "qpix", PathStyle: true, AccessKey: "fixture-access", SecretKey: "fixture-secret"}
	body := c.request("POST", "/api/v1/storages", s3input, 201)
	if strings.Contains(string(body), s3input.AccessKey) || strings.Contains(string(body), s3input.SecretKey) || strings.Contains(string(c.request("GET", "/api/v1/storages", nil, 200)), "fixture-") {
		t.Fatal("API 泄漏凭据")
	}
	var s3profile profileView
	if err = json.Unmarshal(body, &s3profile); err != nil {
		t.Fatal(err)
	}
	var stored database.StorageProfile
	if err = a.db.First(&stored, "id = ?", s3profile.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Credentials == "" || strings.Contains(stored.Credentials, "fixture-") {
		t.Fatal("凭据未加密")
	}
	s3input.AccessKey, s3input.SecretKey = "", ""
	c.request("PUT", "/api/v1/storages/"+s3profile.ID, s3input, 200)
	var updated database.StorageProfile
	if err = a.db.First(&updated, "id = ?", s3profile.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Credentials != stored.Credentials {
		t.Fatal("留空更新更改了已有凭据")
	}
	c.request("POST", "/api/v1/storages/"+s3profile.ID+"/test", nil, 200)
	var cloudImage imageView
	if err = json.Unmarshal(c.upload(fixture, "s3.png", "", "", 201, map[string]string{"storage_id": s3profile.ID, "compression": "none"}), &cloudImage); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c.request("GET", cloudImage.URL, nil, 200), fixture) {
		t.Fatal("S3 网关读取内容不一致")
	}
	c.request("GET", cloudImage.ThumbnailURL, nil, 200)
	c.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{cloudImage.ID}}, 200)
	cloudMu.Lock()
	remainingObjects := len(objects)
	cloudMu.Unlock()
	if remainingObjects != 0 {
		t.Fatal("S3 删除留下对象")
	}
	// 重建应用验证持久化配置与主密钥均可恢复，随后继续使用原测试会话。
	reloaded, err := New(a.cfg, a.db, map[string]storage.Store{"local": a.getStore("local")})
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.profiles[profile.ID].Enabled || reloaded.accessRules["images"].input.RefererEnabled {
		t.Fatal("配置恢复错误")
	}
	c.request("DELETE", "/api/v1/storages/"+s3profile.ID, nil, 200)
	c.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{uploaded.ID, compressed.ID}}, 200)
	c.request("DELETE", "/api/v1/storages/"+profile.ID, nil, 200)
}

func TestAccessMatchingAndProxyTrust(t *testing.T) {
	rule, err := compileRule(ruleInput{Scope: "images", IPEnabled: true, IPs: []string{"192.0.2.0/24", "2001:db8::/32"}, RefererEnabled: true, Referers: []string{"*.example.com"}, AllowEmptyReferer: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		ip, referer string
		want        bool
	}{
		{"192.0.2.10", "https://a.example.com/x", true}, {"2001:db8::1", "", true},
		{"192.0.2.1", "https://example.com", false}, {"192.0.2.1", "https://badexample.com", false},
		{"192.0.2.1", "null", false}, {"192.0.2.1", "https://a.example.com@evil.test", false}, {"198.51.100.1", "https://a.example.com", false},
	} {
		if got := rule.allows(netip.MustParseAddr(test.ip), test.referer); got != test.want {
			t.Errorf("%+v: %v", test, got)
		}
	}
	for _, input := range []ruleInput{{Scope: "bad"}, {Scope: "admin", IPEnabled: true}, {Scope: "images", Referers: []string{"https://example.com"}}, {Scope: "upload", IPs: []string{"bad"}}} {
		if _, err = compileRule(input); err == nil {
			t.Errorf("接受无效规则: %+v", input)
		}
	}
	a := &App{trustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	for _, test := range []struct{ peer, xff, want string }{
		{"192.0.2.1:5000", "198.51.100.1", "192.0.2.1"},
		{"10.0.0.1:5000", "198.51.100.99, 192.0.2.1, 10.0.0.2", "192.0.2.1"},
		{"10.0.0.1:5000", "2001:db8::1, 10.0.0.2", "2001:db8::1"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = test.peer
		r.Header.Set("X-Forwarded-For", test.xff)
		if got := a.requesterIP(r).String(); got != test.want {
			t.Errorf("%+v: %s", test, got)
		}
	}
	a.cfg.DisableAccessRules = true
	a.accessRules = map[string]accessRule{"admin": rule}
	if !a.enforceAccess(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/users", nil)) {
		t.Fatal("应急开关未生效")
	}
}
