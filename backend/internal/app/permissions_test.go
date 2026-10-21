package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"qpix/internal/database"
)

func memberClient(t *testing.T, server *httptest.Server, username, password string) *testClient {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &testClient{t: t, server: server, client: &http.Client{Jar: jar}}
	var auth struct {
		CSRF string        `json:"csrf_token"`
		User database.User `json:"user"`
	}
	if err := json.Unmarshal(c.request("POST", "/api/v1/auth/login", map[string]string{"username": username, "password": password}, 200), &auth); err != nil {
		t.Fatal(err)
	}
	if auth.User.Role != "user" {
		t.Fatal("统一登录未识别普通用户角色")
	}
	c.csrf = auth.CSRF
	c.request("GET", "/api/v1/auth/me", nil, 200)
	return c
}

func testPermissionsAndShares(t *testing.T, a *App, admin *testClient, fixture []byte) {
	t.Helper()
	decodeImage := func(body []byte) imageView {
		var row imageView
		if err := json.Unmarshal(body, &row); err != nil {
			t.Fatal(err)
		}
		return row
	}
	createUser := func(name string) string {
		var row struct{ ID string }
		input := userInput{Username: name, Enabled: true, QuotaBytes: 10000, MaxFileBytes: 4096, Password: "test-user-password", ConfirmPassword: "test-user-password"}
		if err := json.Unmarshal(admin.request("POST", "/api/v1/users", input, 200), &row); err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	aliceID := createUser("alice")
	admin.request("POST", "/api/v1/users", userInput{Username: "alice", Enabled: true, Password: "test-user-password", ConfirmPassword: "test-user-password"}, 409)
	bobID := createUser("bob")
	alice := memberClient(t, admin.server, "alice", "test-user-password")
	bob := memberClient(t, admin.server, "bob", "test-user-password")
	jar, _ := cookiejar.New(nil)
	guest := &testClient{t: t, server: admin.server, client: &http.Client{Jar: jar}}
	alice.request("GET", "/api/v1/users", nil, 403)
	alice.request("GET", "/api/v1/storages", nil, 403)
	alice.request("POST", "/api/v1/albums", map[string]string{"name": "越权"}, 403)
	alice.request("POST", "/api/v1/shares", map[string]string{"kind": "album", "target_id": "x"}, 403)
	var opts map[string]any
	if err := json.Unmarshal(alice.request("GET", "/api/v1/upload-options", nil, 200), &opts); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(opts)
	if strings.Contains(string(encoded), "root") || strings.Contains(string(encoded), "endpoint") || strings.Contains(string(encoded), "credentials") {
		t.Fatal("上传选项泄露存储配置")
	}
	var album database.Album
	if err := json.Unmarshal(admin.request("POST", "/api/v1/albums", map[string]string{"name": "权限相册"}, 201), &album); err != nil {
		t.Fatal(err)
	}
	admin.request("PUT", "/api/v1/albums/"+album.ID+"/access", map[string]any{"guest_visible": false, "member_ids": []string{aliceID}}, 200)
	adminImage := decodeImage(admin.upload(fixture, "admin-private.png", album.ID, "", 201))
	// 访客只能凭图片或分享链接访问，不提供公开相册与图片发现接口。
	for _, path := range []string{"/api/v1/browse/albums", "/api/v1/browse/images"} {
		guest.request("GET", path, nil, 404)
		admin.request("GET", path, nil, 404)
	}
	guest.request("GET", "/api/v1/albums", nil, 401)
	guest.request("GET", "/api/v1/images", nil, 401)
	aliceImage := decodeImage(alice.upload(fixture, "alice-private.png", album.ID, "", 201))
	bobImage := decodeImage(bob.upload(fixture, "bob-unfiled.png", "", "", 201))
	guest.request("GET", adminImage.URL, nil, 404)
	guest.request("HEAD", adminImage.ThumbnailURL, nil, 404)
	alice.request("GET", adminImage.URL, nil, 200)
	bob.request("GET", adminImage.URL, nil, 404)
	alice.request("GET", bobImage.URL, nil, 404)
	admin.request("GET", bobImage.URL, nil, 200)
	bob.upload(fixture, "not-member.png", album.ID, "", 400)
	alice.request("PATCH", "/api/v1/images/"+adminImage.ID, map[string]string{"name": "越权.png"}, 404)
	alice.request("PATCH", "/api/v1/images/"+aliceImage.ID, map[string]string{"name": "本人图片.png"}, 200)
	admin.request("PATCH", "/api/v1/images/"+bobImage.ID, map[string]string{"name": "管理员可修改.png"}, 200)
	var batch struct {
		Deleted int
		Failed  []string
	}
	if err := json.Unmarshal(bob.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{aliceImage.ID}}, 200), &batch); err != nil {
		t.Fatal(err)
	}
	if batch.Deleted != 0 || len(batch.Failed) != 1 {
		t.Fatal("批量删除未拒绝越权 ID")
	}
	var page struct {
		Items []imageView
		Total int
	}
	if err := json.Unmarshal(bob.request("GET", "/api/v1/images?album_id="+album.ID, nil, 200), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 || len(page.Items) != 0 {
		t.Fatal("未授权列表泄露数量或记录")
	}
	admin.request("PUT", "/api/v1/albums/"+album.ID+"/access", map[string]any{"guest_visible": true, "member_ids": []string{aliceID}}, 200)
	if !bytes.Equal(guest.request("GET", adminImage.URL, nil, 200), fixture) {
		t.Fatal("访客公开图片内容错误")
	}
	guest.request("HEAD", adminImage.ThumbnailURL, nil, 200)
	guest.request("GET", "/api/v1/browse/images?album_id="+album.ID, nil, 404)
	if err := json.Unmarshal(bob.request("GET", "/api/v1/images?album_id="+album.ID, nil, 200), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 || page.Items[0].CanManage {
		t.Fatal("公开相册向未授权用户授予了编辑权限或列表错误")
	}
	bob.upload(fixture, "public-not-writable.png", album.ID, "", 400)
	alice.request("PATCH", "/api/v1/images/"+aliceImage.ID, map[string]string{"album_id": "missing"}, 400)
	admin.request("PUT", "/api/v1/albums/"+album.ID+"/access", map[string]any{"guest_visible": false, "member_ids": []string{aliceID}}, 200)
	// 分享是一项独立的只读授权，私有相册的裸链接仍然不可访问。
	var share shareView
	if err := json.Unmarshal(admin.request("POST", "/api/v1/shares", map[string]any{"kind": "album", "target_id": album.ID, "password": "share-secret", "expires_at": time.Now().Add(time.Hour)}, 201), &share); err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(share.URL, "/s/")
	path := "/api/v1/public-shares/" + token
	var locked struct {
		Locked bool
		Items  []imageView
	}
	if err := json.Unmarshal(guest.request("GET", path, nil, 200), &locked); err != nil {
		t.Fatal(err)
	}
	if !locked.Locked || len(locked.Items) != 0 {
		t.Fatal("口令未解锁即泄露图片")
	}
	shareURL := "/si/" + token + "/" + adminImage.ID + "." + adminImage.Format
	guest.request("GET", shareURL, nil, 404)
	guest.request("POST", path+"/unlock", map[string]string{"password": "wrong"}, 403)
	guest.request("POST", path+"/unlock", map[string]string{"password": "share-secret"}, 200)
	if err := json.Unmarshal(guest.request("GET", path, nil, 200), &locked); err != nil {
		t.Fatal(err)
	}
	if locked.Locked || len(locked.Items) != 2 || !strings.HasPrefix(locked.Items[0].URL, "/si/") {
		t.Fatal("分享内容或网关错误")
	}
	guest.request("GET", shareURL, nil, 200)
	guest.request("GET", "/si/"+token+"/"+bobImage.ID+"."+bobImage.Format, nil, 404)
	guest.request("GET", adminImage.URL, nil, 404)
	guest.request("HEAD", "/si/"+token+"/"+adminImage.ID+"-thumb.png", nil, 200)
	admin.request("DELETE", "/api/v1/shares/"+share.ID, nil, 200)
	request, _ := http.NewRequest("GET", admin.server.URL+shareURL, nil)
	request.Header.Set("If-None-Match", `"`+adminImage.ID+"."+adminImage.Format+`"`)
	guest.send(request, 404)
	guest.request("GET", path, nil, 404)
	var imageShare shareView
	offsetExpiry := time.Now().Add(time.Hour).In(time.FixedZone("test+08", 8*60*60))
	if err := json.Unmarshal(admin.request("POST", "/api/v1/shares", map[string]any{"kind": "image", "target_id": bobImage.ID, "expires_at": offsetExpiry}, 201), &imageShare); err != nil {
		t.Fatal(err)
	}
	var timedShare database.Share
	if err := a.db.First(&timedShare, "id = ?", imageShare.ID).Error; err != nil {
		t.Fatal(err)
	}
	_, offset := timedShare.ExpiresAt.Zone()
	if offset != 0 {
		t.Fatal("分享到期时间没有归一化为 UTC")
	}
	imageToken := strings.TrimPrefix(imageShare.URL, "/s/")
	guest.request("GET", "/si/"+imageToken+"/"+bobImage.ID+"."+bobImage.Format, nil, 200)
	if err := a.db.Model(&database.Share{}).Where("id = ?", imageShare.ID).Update("expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	guest.request("GET", "/si/"+imageToken+"/"+bobImage.ID+"."+bobImage.Format, nil, 404)
	var tokenData struct{ Token string }
	if err := json.Unmarshal(alice.request("POST", "/api/v1/tokens", map[string]string{"name": "普通用户上传"}, 201), &tokenData); err != nil {
		t.Fatal(err)
	}
	tokenImage := decodeImage(guest.upload(fixture, "member-token.png", "", tokenData.Token, 201))
	alice.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{tokenImage.ID}}, 200)
	admin.request("PUT", "/api/v1/albums/"+album.ID+"/access", map[string]any{"guest_visible": false, "member_ids": []string{}}, 200)
	alice.request("GET", aliceImage.URL, nil, 404)
	alice.request("PATCH", "/api/v1/images/"+aliceImage.ID, map[string]string{"name": "撤销后"}, 404)
	guest.upload(fixture, "revoked-album-token.png", album.ID, tokenData.Token, 400)
	// 网络应急开关不能跳过相册权限。
	a.cfg.DisableAccessRules = true
	bob.request("GET", adminImage.URL, nil, 404)
	a.cfg.DisableAccessRules = false
	admin.request("DELETE", "/api/v1/albums/"+album.ID, nil, 200)
	alice.request("GET", aliceImage.URL, nil, 200)
	guest.request("GET", aliceImage.URL, nil, 404)
	admin.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{adminImage.ID, aliceImage.ID, bobImage.ID}}, 200)
	// 容量边界按实际保存大小累计；失败上传释放预占，单张限制检查原文件大小。
	quotaInput := userInput{Enabled: true, QuotaBytes: int64(len(fixture)), MaxFileBytes: int64(len(fixture))}
	admin.request("PUT", "/api/v1/users/"+aliceID, quotaInput, 200)
	alice.upload([]byte("broken"), "bad.png", "", "", 415)
	var reservations int64
	if err := a.db.Model(&database.UploadReservation{}).Where("user_id = ?", aliceID).Count(&reservations).Error; err != nil || reservations != 0 {
		t.Fatal("失败上传未释放预占", err)
	}
	var uploaded imageView
	if err := json.Unmarshal(alice.upload(fixture, "quota.png", "", "", 201), &uploaded); err != nil {
		t.Fatal(err)
	}
	alice.upload(fixture, "full.png", "", "", 409)
	alice.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{uploaded.ID}}, 200)
	quotaInput.MaxFileBytes = int64(len(fixture) - 1)
	admin.request("PUT", "/api/v1/users/"+aliceID, quotaInput, 200)
	alice.upload(fixture, "too-big.png", "", "", 413)
	quotaInput.MaxFileBytes = int64(len(fixture))
	admin.request("PUT", "/api/v1/users/"+aliceID, quotaInput, 200)
	// 与上传接口共用同一预占函数，两个并发请求只允许一个取得最后一份容量。
	var wg sync.WaitGroup
	ids := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/api/v1/images", nil)
			id, err := a.reserveUpload(req, aliceID, int64(len(fixture)))
			if err == nil {
				ids <- id
			}
		}()
	}
	wg.Wait()
	close(ids)
	var reservedIDs []string
	for id := range ids {
		reservedIDs = append(reservedIDs, id)
	}
	if len(reservedIDs) != 1 {
		t.Fatal("并发容量预占发生超额")
	}
	a.db.Where("id IN ?", reservedIDs).Delete(&database.UploadReservation{})
	quotaInput.Enabled = false
	admin.request("PUT", "/api/v1/users/"+aliceID, quotaInput, 200)
	alice.request("GET", "/api/v1/auth/me", nil, 401)
	guest.request("POST", "/api/v1/auth/login", map[string]string{"username": "alice", "password": "test-user-password"}, 401)
	alice.upload(fixture, "disabled.png", "", "", 401)
	guest.upload(fixture, "disabled-token.png", "", tokenData.Token, 401)
	// 统一入口按数据库中的角色应用规则：管理限制不能误伤用户，也不能被登录绕过。
	a.settingsMu.Lock()
	original := a.accessRules["admin"]
	blocked, _ := compileRule(ruleInput{Scope: "admin", IPEnabled: true, IPs: []string{"192.0.2.1"}})
	a.accessRules["admin"] = blocked
	a.settingsMu.Unlock()
	admin.request("GET", "/api/v1/auth/me", nil, 403)
	bob.request("GET", "/api/v1/auth/me", nil, 200)
	guest.request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": a.cfg.AdminPassword}, 403)
	guest.request("GET", "/api/v1/auth/me", nil, 401)
	bob = memberClient(t, admin.server, "bob", "test-user-password")
	bob.request("GET", "/api/v1/users", nil, 403)
	a.settingsMu.Lock()
	a.accessRules["admin"] = original
	originalImages := a.accessRules["images"]
	blockedImages, _ := compileRule(ruleInput{Scope: "images", RefererEnabled: true, Referers: []string{"allowed.example"}})
	a.accessRules["images"] = blockedImages
	a.settingsMu.Unlock()
	bob.request("POST", "/api/v1/member/auth/login", map[string]string{"username": "admin", "password": a.cfg.AdminPassword}, 404)
	bob.request("GET", "/api/v1/auth/me", nil, 403)
	guest.request("POST", "/api/v1/auth/login", map[string]string{"username": "bob", "password": "test-user-password"}, 403)
	guest.request("GET", "/api/v1/auth/me", nil, 401)
	guest.request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": a.cfg.AdminPassword}, 200)
	admin.request("GET", "/api/v1/auth/me", nil, 200)
	a.settingsMu.Lock()
	a.accessRules["images"] = originalImages
	a.settingsMu.Unlock()
	// 普通用户的改密、会话恢复和退出同样使用统一接口，并保持 CSRF 与会话撤销。
	bob.request("PUT", "/api/v1/auth/password", map[string]string{"current_password": "test-user-password", "new_password": "changed-user-password"}, 200)
	bob.request("GET", "/api/v1/auth/me", nil, 401)
	bob.request("POST", "/api/v1/auth/login", map[string]string{"username": "bob", "password": "test-user-password"}, 401)
	bob = memberClient(t, admin.server, "bob", "changed-user-password")
	csrf := bob.csrf
	bob.csrf = ""
	bob.request("POST", "/api/v1/auth/logout", nil, 403)
	bob.csrf = csrf
	bob.request("POST", "/api/v1/auth/logout", nil, 200)
	bob.request("GET", "/api/v1/auth/me", nil, 401)
	// 密码和分享令牌只以哈希或密文落库。
	var stored database.Share
	if err := a.db.First(&stored, "id = ?", share.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TokenHash == token || strings.Contains(stored.TokenCipher, token) || stored.PasswordHash == "share-secret" {
		t.Fatal("分享凭据未安全保存")
	}
	_ = bobID
}
