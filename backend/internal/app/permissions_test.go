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
	for _, path := range []string{"images", "albums", "shares", "tokens", "stats"} {
		alice.request("GET", "/api/v1/"+path+"?scope=all", nil, 403)
	}
	var aliceUser, bobUser database.User
	if err := a.db.First(&aliceUser, "id = ?", aliceID).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.First(&bobUser, "id = ?", bobID).Error; err != nil {
		t.Fatal(err)
	}
	if aliceUser.DefaultAlbumID == "" || bobUser.DefaultAlbumID == "" || aliceUser.DefaultAlbumID == bobUser.DefaultAlbumID {
		t.Fatal("默认相册未按账号独立创建")
	}
	alice.request("DELETE", "/api/v1/albums/"+aliceUser.DefaultAlbumID, map[string]string{"action": "delete"}, 409)
	var album database.Album
	if err := json.Unmarshal(alice.request("POST", "/api/v1/albums", map[string]string{"name": "个人相册"}, 201), &album); err != nil {
		t.Fatal(err)
	}
	bob.request("PATCH", "/api/v1/albums/"+album.ID, map[string]string{"name": "越权"}, 404)
	bob.request("PUT", "/api/v1/albums/"+album.ID+"/default", map[string]string{}, 404)
	bob.request("PUT", "/api/v1/albums/"+album.ID+"/access", map[string]any{"guest_visible": true}, 404)
	bob.request("DELETE", "/api/v1/albums/"+album.ID, map[string]string{"action": "delete"}, 404)
	alice.request("PUT", "/api/v1/albums/"+album.ID+"/default", map[string]string{}, 200)
	aliceImage := decodeImage(alice.upload(fixture, "alice.png", "", "", 201))
	bobImage := decodeImage(bob.upload(fixture, "bob.png", "", "", 201))
	var personal, globalPage struct {
		Items []imageView
		Total int
	}
	if err := json.Unmarshal(admin.request("GET", "/api/v1/images", nil, 200), &personal); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(admin.request("GET", "/api/v1/images?scope=all&limit=100", nil, 200), &globalPage); err != nil {
		t.Fatal(err)
	}
	for _, image := range personal.Items {
		if image.OwnerID == aliceID || image.OwnerID == bobID {
			t.Fatal("管理员个人图库包含他人图片")
		}
	}
	if globalPage.Total < personal.Total+2 {
		t.Fatal("全站图库未包含其他用户图片")
	}
	foundOwner := false
	for _, image := range globalPage.Items {
		if image.OwnerID == aliceID {
			foundOwner = image.OwnerName == "alice" && image.OwnerRole == "user"
		}
	}
	if !foundOwner {
		t.Fatal("全站管理缺少上传人和身份")
	}
	admin.request("POST", "/api/v1/images/batch-move", map[string]any{"ids": []string{aliceImage.ID}, "album_id": bobUser.DefaultAlbumID}, 400)
	admin.request("PATCH", "/api/v1/images/"+aliceImage.ID, map[string]string{"album_id": bobUser.DefaultAlbumID}, 400)

	if aliceImage.AlbumID == nil || *aliceImage.AlbumID != album.ID {
		t.Fatal("网页上传未使用最新默认相册")
	}
	bob.upload(fixture, "越权.png", album.ID, "", 400)
	for _, path := range []string{"/api/v1/browse/albums", "/api/v1/browse/images"} {
		guest.request("GET", path, nil, 404)
	}
	guest.request("GET", "/api/v1/albums", nil, 401)
	var page struct {
		Items []imageView
		Total int
	}
	if err := json.Unmarshal(bob.request("GET", "/api/v1/images?album_id="+album.ID, nil, 200), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatal("公开直链不应授予图库发现权限")
	}
	var listed []database.Album
	if err := json.Unmarshal(bob.request("GET", "/api/v1/albums", nil, 200), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != bobUser.DefaultAlbumID {
		t.Fatal("相册列表包含其他账号的相册")
	}
	bob.request("PATCH", "/api/v1/images/"+aliceImage.ID, map[string]string{"name": "越权.png"}, 404)
	bob.request("POST", "/api/v1/images/batch-move", map[string]any{"ids": []string{aliceImage.ID}, "album_id": bobUser.DefaultAlbumID}, 404)
	alice.request("POST", "/api/v1/images/batch-move", map[string]any{"ids": []string{aliceImage.ID}, "album_id": bobUser.DefaultAlbumID}, 404)
	guest.request("GET", aliceImage.URL, nil, 200)
	setPolicy := func(mode string, refs []string, empty bool) {
		alice.request("PUT", "/api/v1/albums/"+album.ID+"/access", map[string]any{"guest_visible": false, "referer_mode": mode, "referers": refs, "allow_empty_referer": empty}, 200)
	}
	setPolicy("inherit", nil, true)
	guest.request("GET", aliceImage.URL, nil, 404)
	alice.request("GET", aliceImage.URL, nil, 200)
	alice.request("POST", "/api/v1/shares", map[string]string{"kind": "album", "target_id": album.ID}, 400)
	bob.request("POST", "/api/v1/shares", map[string]string{"kind": "image", "target_id": aliceImage.ID}, 404)
	var share shareView
	if err := json.Unmarshal(alice.request("POST", "/api/v1/shares", map[string]any{"kind": "image", "target_id": aliceImage.ID, "password": "share-secret", "expires_at": time.Now().Add(time.Hour)}, 201), &share); err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(share.URL, "/s/")
	path := "/api/v1/public-shares/" + token
	var shared struct {
		Locked bool
		Items  []imageView
	}
	if err := json.Unmarshal(guest.request("GET", path, nil, 200), &shared); err != nil {
		t.Fatal(err)
	}
	if !shared.Locked || len(shared.Items) != 0 {
		t.Fatal("密码未解锁已泄露图片")
	}
	shareURL := "/si/" + token + "/" + aliceImage.ID + "." + aliceImage.Format
	guest.request("GET", shareURL, nil, 404)
	guest.request("POST", path+"/unlock", map[string]string{"password": "wrong"}, 403)
	guest.request("POST", path+"/unlock", map[string]string{"password": "share-secret"}, 200)
	if err := json.Unmarshal(guest.request("GET", path, nil, 200), &shared); err != nil {
		t.Fatal(err)
	}
	if shared.Locked || len(shared.Items) != 1 || !strings.HasPrefix(shared.Items[0].URL, "/si/") {
		t.Fatal("单图分享范围错误")
	}
	if !bytes.Equal(guest.request("GET", shareURL, nil, 200), fixture) {
		t.Fatal("分享图片内容错误")
	}
	guest.request("GET", "/si/"+token+"/"+bobImage.ID+"."+bobImage.Format, nil, 404)
	bob.request("DELETE", "/api/v1/shares/"+share.ID, nil, 404)
	var sharePage struct {
		Items []shareView
		Total int
	}
	if err := json.Unmarshal(bob.request("GET", "/api/v1/shares", nil, 200), &sharePage); err != nil {
		t.Fatal(err)
	}
	if sharePage.Total != 0 {
		t.Fatal("分享列表泄露其他账号数据")
	}
	admin.request("GET", "/api/v1/shares?scope=all&limit=1", nil, 200)
	// 直链应用相册规则；分享及普通账号操作保持独立。
	setPolicy("custom", []string{"*.allowed.example"}, false)
	guest.request("GET", shareURL, nil, 200)
	guest.request("GET", path, nil, 200)
	alice.request("GET", aliceImage.URL, nil, 403)
	alice.request("GET", "/api/v1/albums", nil, 200)
	guest.request("POST", path+"/unlock", map[string]string{"password": "share-secret"}, 200)
	request, _ := http.NewRequest("HEAD", admin.server.URL+shareURL, nil)
	request.Header.Set("Referer", "https://a.allowed.example/view")
	guest.send(request, 200)
	request, _ = http.NewRequest("HEAD", admin.server.URL+aliceImage.ThumbnailURL, nil)
	request.Header.Set("Referer", "https://a.allowed.example/view")
	alice.send(request, 200)
	request, _ = http.NewRequest("HEAD", admin.server.URL+shareURL, nil)
	request.Header.Set("Referer", "https://allowed.example/view")
	guest.send(request, 200)
	setPolicy("custom", []string{"*.allowed.example"}, true)
	guest.request("GET", shareURL, nil, 200)
	a.settingsMu.Lock()
	originalImages := a.accessRules["images"]
	global, _ := compileRule(ruleInput{Scope: "images", RefererEnabled: true, Referers: []string{"global.example"}})
	a.accessRules["images"] = global
	a.settingsMu.Unlock()
	guest.request("GET", shareURL, nil, 200)
	// 管理会话使用管理员规则，以便验证用户相册可以覆盖全局 Referer。
	admin.request("PUT", "/api/v1/albums/"+album.ID+"/access", map[string]any{"guest_visible": false, "referer_mode": "unrestricted"}, 200)
	guest.request("GET", shareURL, nil, 200)
	a.settingsMu.Lock()
	blockedIP, _ := compileRule(ruleInput{Scope: "images", IPEnabled: true, IPs: []string{"192.0.2.1"}})
	a.accessRules["images"] = blockedIP
	a.settingsMu.Unlock()
	guest.request("GET", shareURL, nil, 200)
	guest.request("GET", path, nil, 200)
	alice.request("GET", aliceImage.URL, nil, 403)
	alice.request("GET", "/api/v1/images", nil, 200)
	a.settingsMu.Lock()
	a.accessRules["images"] = originalImages
	a.settingsMu.Unlock()

	expired := time.Now().UTC().Add(-time.Minute)
	if err := a.db.Model(&database.Share{}).Where("id = ?", share.ID).Update("expires_at", expired).Error; err != nil {
		t.Fatal(err)
	}
	guest.request("GET", shareURL, nil, 404)
	if err := a.db.Model(&database.Share{}).Where("id = ?", share.ID).Update("expires_at", time.Now().UTC().Add(time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	// 移动到其他相册立即使用目标策略，图片归属和容量不改变。
	alice.request("POST", "/api/v1/images/batch-move", map[string]any{"ids": []string{aliceImage.ID}, "album_id": aliceUser.DefaultAlbumID}, 200)
	guest.request("GET", aliceImage.URL, nil, 200)
	alice.request("DELETE", "/api/v1/albums/"+album.ID, map[string]string{"action": "delete"}, 409)
	alice.request("PUT", "/api/v1/albums/"+aliceUser.DefaultAlbumID+"/default", map[string]string{}, 200)
	alice.request("DELETE", "/api/v1/albums/"+album.ID, map[string]string{"action": "move", "target_id": aliceUser.DefaultAlbumID}, 200)
	// Token 列表和撤销按创建者隔离，记录保留，使用时间由认证上传更新。
	var tokenData struct {
		Token string
		Item  database.APIToken
	}
	if err := json.Unmarshal(alice.request("POST", "/api/v1/tokens", map[string]string{"name": "个人上传"}, 201), &tokenData); err != nil {
		t.Fatal(err)
	}
	var tokenPage struct {
		Items []database.APIToken
		Total int
	}
	if err := json.Unmarshal(bob.request("GET", "/api/v1/tokens", nil, 200), &tokenPage); err != nil {
		t.Fatal(err)
	}
	if tokenPage.Total != 0 {
		t.Fatal("Token 列表泄露其他账号数据")
	}
	bob.request("DELETE", "/api/v1/tokens/"+tokenData.Item.ID, nil, 404)
	tokenImage := decodeImage(guest.upload(fixture, "token-default.png", "", tokenData.Token, 201))
	if tokenImage.AlbumID == nil || *tokenImage.AlbumID != aliceUser.DefaultAlbumID {
		t.Fatal("Token 上传未使用默认相册")
	}
	var storedToken database.APIToken
	if err := a.db.First(&storedToken, "id = ?", tokenData.Item.ID).Error; err != nil || storedToken.LastUsedAt == nil {
		t.Fatal("未记录 Token 使用时间", err)
	}
	admin.request("DELETE", "/api/v1/tokens/"+tokenData.Item.ID, nil, 200)
	guest.upload(fixture, "revoked.png", "", tokenData.Token, 401)
	if err := a.db.First(&storedToken, "id = ?", tokenData.Item.ID).Error; err != nil || storedToken.RevokedAt == nil {
		t.Fatal("撤销记录未保留", err)
	}
	admin.request("DELETE", "/api/v1/shares/"+share.ID, nil, 200)
	request, _ = http.NewRequest("GET", admin.server.URL+shareURL, nil)
	request.Header.Set("If-None-Match", `"`+aliceImage.ID+"."+aliceImage.Format+`"`)
	guest.send(request, 404)
	admin.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{aliceImage.ID, bobImage.ID, tokenImage.ID}}, 200)
	// 连同图片删除必须立即让直链与分享失效，并清理原图、缩略图。
	var disposable database.Album
	json.Unmarshal(alice.request("POST", "/api/v1/albums", map[string]string{"name": "待删除"}, 201), &disposable)
	image := decodeImage(alice.upload(fixture, "delete.png", disposable.ID, "", 201))
	alice.request("DELETE", "/api/v1/albums/"+disposable.ID, map[string]string{"action": "delete"}, 200)
	guest.request("GET", image.URL, nil, 404)
	var imageCount int64
	a.db.Model(&database.Image{}).Where("id = ?", image.ID).Count(&imageCount)
	if imageCount != 0 {
		t.Fatal("删除相册未清理图片")
	}
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
	originalImages = a.accessRules["images"]
	blockedImages, _ := compileRule(ruleInput{Scope: "images", RefererEnabled: true, Referers: []string{"allowed.example"}})
	a.accessRules["images"] = blockedImages
	a.settingsMu.Unlock()
	bob.request("POST", "/api/v1/member/auth/login", map[string]string{"username": "admin", "password": a.cfg.AdminPassword}, 404)
	bob.request("GET", "/api/v1/auth/me", nil, 200)
	guest.request("POST", "/api/v1/auth/login", map[string]string{"username": "bob", "password": "test-user-password"}, 200)
	guest.request("GET", "/api/v1/auth/me", nil, 200)
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
