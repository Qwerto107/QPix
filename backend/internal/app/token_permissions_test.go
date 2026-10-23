package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qpix/internal/config"
	"qpix/internal/database"
	"qpix/internal/storage"
)

func TestTokenAlbumPermissions(t *testing.T) {
	root := t.TempDir()
	db, err := database.Open("sqlite", filepath.Join(root, "tokens.db"))
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
	cfg := config.Config{DBDriver: "sqlite", StorageRoot: filepath.Join(root, "uploads"), AdminUsername: "admin", AdminPassword: "admin-test", MaxUploadBytes: 4096, MaxPixels: 100}
	a, err := New(cfg, db, map[string]storage.Store{"local": local})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	admin := &testClient{t: t, server: server, client: &http.Client{Jar: jar}}
	var auth struct {
		CSRF string `json:"csrf_token"`
		User database.User
	}
	json.Unmarshal(admin.request("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": cfg.AdminPassword}, 200), &auth)
	admin.csrf = auth.CSRF
	createUser := func(name string) *testClient {
		admin.request("POST", "/api/v1/users", userInput{Username: name, Enabled: true, Password: "1", ConfirmPassword: "1"}, 200)
		return memberClient(t, server, name, "1")
	}
	alice, bob := createUser("token-alice"), createUser("token-bob")
	var owner, other database.User
	if err = db.First(&owner, "username = ?", "token-alice").Error; err != nil {
		t.Fatal(err)
	}
	if err = db.First(&other, "username = ?", "token-bob").Error; err != nil {
		t.Fatal(err)
	}
	createAlbum := func(name string) database.Album {
		var album database.Album
		json.Unmarshal(alice.request("POST", "/api/v1/albums", map[string]string{"name": name}, 201), &album)
		alice.request("PUT", "/api/v1/albums/"+album.ID+"/access", map[string]any{"guest_visible": false, "referer_mode": "inherit"}, 200)
		return album
	}
	albumA, albumB := createAlbum("可读相册"), createAlbum("只可上传相册")
	alice.request("PUT", "/api/v1/albums/"+albumA.ID+"/default", map[string]string{}, 200)
	fixture := testPNG(t, 3, 2)
	upload := func(name, id string) imageView {
		var image imageView
		json.Unmarshal(alice.upload(fixture, name, id, "", 201), &image)
		return image
	}
	imageA, imageB := upload("a.png", albumA.ID), upload("b.png", albumB.ID)
	authorization := tokenAuthorization{Permissions: []string{}, Albums: []albumGrant{
		{AlbumID: albumA.ID, Permissions: []string{tokenRead, tokenEdit, tokenDelete, tokenAlbums, tokenShare}},
		{AlbumID: albumB.ID, Permissions: []string{tokenUpload}},
	}}
	var issued struct {
		Token string
		Item  tokenView
	}
	json.Unmarshal(alice.request("POST", "/api/v1/tokens", tokenInput{Name: "分相册权限", Authorization: &authorization}, 201), &issued)
	if issued.Token == "" || issued.Item.Authorization.AllAlbums || len(issued.Item.Authorization.Albums) != 2 {
		t.Fatal("权限未随 Token 创建保存")
	}
	var stored database.APIToken
	if err = db.First(&stored, "id = ?", issued.Item.ID).Error; err != nil || stored.Hash == issued.Token || stored.Hash != digest(issued.Token) {
		t.Fatal("Token 哈希保存失败", err)
	}
	bearer := &testClient{t: t, server: server, client: &http.Client{}}
	request := func(method, path string, body any, status int) []byte {
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		req, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer "+issued.Token)
		req.Header.Set("Content-Type", "application/json")
		return bearer.send(req, status)
	}
	var images struct {
		Items []imageView
		Total int
	}
	json.Unmarshal(request("GET", "/api/v1/images", nil, 200), &images)
	if images.Total != 1 || images.Items[0].ID != imageA.ID {
		t.Fatal("列表未按读取权限过滤")
	}
	var albums []database.Album
	json.Unmarshal(request("GET", "/api/v1/albums", nil, 200), &albums)
	if len(albums) != 1 || albums[0].ID != albumA.ID {
		t.Fatal("相册列表越过授权范围")
	}
	var stats struct{ Images int }
	json.Unmarshal(request("GET", "/api/v1/stats", nil, 200), &stats)
	if stats.Images != 1 {
		t.Fatal("统计泄露未授权相册数量")
	}
	request("GET", imageA.URL, nil, 200)
	request("HEAD", imageA.ThumbnailURL, nil, 200)
	request("GET", imageB.URL, nil, 404)
	request("GET", "/api/v1/images?scope=all", nil, 403)
	for _, path := range []string{"/api/v1/tokens", "/api/v1/users", "/api/v1/settings", "/api/v1/auth/me"} {
		request("GET", path, nil, 401)
	}
	request("POST", "/api/v1/tokens", map[string]string{"name": "不能派生凭据"}, 401)
	request("PUT", "/api/v1/auth/password", map[string]string{"current_password": "1", "new_password": "2"}, 401)
	request("POST", "/api/v1/albums", map[string]string{"name": "不能创建"}, 403)
	// 默认相册未获上传授权时拒绝，显式目标及后续默认相册都必须再次校验。
	bearer.upload(fixture, "blocked-default.png", "", issued.Token, 403)
	bearer.upload(fixture, "allowed-b.png", albumB.ID, issued.Token, 201)
	alice.request("PUT", "/api/v1/albums/"+albumB.ID+"/default", map[string]string{}, 200)
	var defaultImage imageView
	json.Unmarshal(bearer.upload(fixture, "new-default.png", "", issued.Token, 201), &defaultImage)
	if defaultImage.AlbumID == nil || *defaultImage.AlbumID != albumB.ID {
		t.Fatal("默认相册变更未即时生效")
	}
	alice.request("PUT", "/api/v1/albums/"+albumA.ID+"/default", map[string]string{}, 200)
	var reservations int64
	db.Model(&database.UploadReservation{}).Count(&reservations)
	if reservations != 0 {
		t.Fatal("Token 失败上传遗留容量预占")
	}
	request("PATCH", "/api/v1/images/"+imageA.ID, map[string]string{"name": "new-a.png"}, 200)
	request("PATCH", "/api/v1/images/"+imageB.ID, map[string]string{"name": "禁止修改"}, 404)
	// 混合批量删除须整体拒绝，不能删除其中有权的图片。
	request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{imageA.ID, imageB.ID}}, 403)
	request("GET", imageA.URL, nil, 200)
	var share shareView
	json.Unmarshal(request("POST", "/api/v1/shares", map[string]string{"kind": "image", "target_id": imageA.ID}, 201), &share)
	request("DELETE", "/api/v1/shares/"+share.ID+"/record", nil, 409)
	request("POST", "/api/v1/shares", map[string]string{"kind": "image", "target_id": imageB.ID}, 404)
	request("POST", "/api/v1/images/batch-move", map[string]any{"ids": []string{imageA.ID}, "album_id": albumB.ID}, 200)
	request("GET", imageA.URL, nil, 404)
	request("DELETE", "/api/v1/shares/"+share.ID, nil, 404)
	alice.request("DELETE", "/api/v1/shares/"+share.ID, nil, 200)
	request("DELETE", "/api/v1/shares/"+share.ID+"/record", nil, 404)
	alice.request("DELETE", "/api/v1/shares/"+share.ID+"/record", nil, 200)
	var shares struct{ Total int }
	json.Unmarshal(request("GET", "/api/v1/shares", nil, 200), &shares)
	if shares.Total != 0 {
		t.Fatal("移动后分享列表仍泄露未授权图片")
	}
	// 旧协作数据即使上传者是本人，外部相册也不包含在“全部本人相册”中。
	legacy := database.Image{ID: randomID(), UserID: owner.ID, AlbumID: &other.DefaultAlbumID, Name: "历史外部相册图片", StorageID: "local", Key: imageB.Key, Format: "png", State: "ready", Size: int64(len(fixture)), Width: 3, Height: 2}
	if err = db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	// 管理员能在界面调整用户 Token，但其他用户不能编辑。
	readOnly := tokenAuthorization{AllAlbums: true, Permissions: []string{tokenRead}, Albums: []albumGrant{}}
	input := tokenInput{Name: "只读", Authorization: &readOnly}
	bob.request("PUT", "/api/v1/tokens/"+issued.Item.ID, input, 404)
	admin.request("PUT", "/api/v1/tokens/"+issued.Item.ID, input, 200)
	request("PATCH", "/api/v1/images/"+imageB.ID, map[string]string{"name": "降权后"}, 403)
	bearer.upload(fixture, "downgraded.png", albumB.ID, issued.Token, 403)
	json.Unmarshal(request("GET", "/api/v1/images", nil, 200), &images)
	for _, image := range images.Items {
		if image.ID == legacy.ID {
			t.Fatal("全部本人相册越过实际相册归属")
		}
		if image.CanManage {
			t.Fatal("只读 Token 标记为可编辑")
		}
	}
	// 全部相册授权包括未来新建相册，且不能逃逸用户所有权。
	all := tokenAuthorization{AllAlbums: true, Permissions: tokenPermissions, Albums: []albumGrant{}}
	input.Authorization = &all
	alice.request("PUT", "/api/v1/tokens/"+issued.Item.ID, input, 200)
	var future database.Album
	json.Unmarshal(request("POST", "/api/v1/albums", map[string]string{"name": "未来相册"}, 201), &future)
	bearer.upload(fixture, "future.png", future.ID, issued.Token, 201)
	request("PATCH", "/api/v1/albums/"+other.DefaultAlbumID, map[string]string{"name": "越权"}, 404)
	bearer.upload(fixture, "other.png", other.DefaultAlbumID, issued.Token, 400)
	// 删除相册并删除图片需要额外的图片删除权限。
	deleteAlbum := createAlbum("可管理但不能删除图片")
	restricted := tokenAuthorization{Albums: []albumGrant{{AlbumID: deleteAlbum.ID, Permissions: []string{tokenAlbums}}}, Permissions: []string{}}
	input.Authorization = &restricted
	alice.request("PUT", "/api/v1/tokens/"+issued.Item.ID, input, 200)
	request("DELETE", "/api/v1/albums/"+deleteAlbum.ID, map[string]string{"action": "delete"}, 403)
	restricted.Albums[0].Permissions = append(restricted.Albums[0].Permissions, tokenDelete)
	alice.request("PUT", "/api/v1/tokens/"+issued.Item.ID, input, 200)
	request("DELETE", "/api/v1/albums/"+deleteAlbum.ID, map[string]string{"action": "delete"}, 200)
	// 未知权限、空授权、重复相册及他人相册不能保存。
	for _, policy := range []tokenAuthorization{
		{AllAlbums: true, Permissions: []string{"admin"}},
		{AllAlbums: true, Permissions: []string{}},
		{Albums: []albumGrant{{AlbumID: other.DefaultAlbumID, Permissions: []string{tokenRead}}}},
		{Albums: []albumGrant{{AlbumID: albumA.ID, Permissions: []string{tokenRead}}, {AlbumID: albumA.ID, Permissions: []string{tokenUpload}}}},
		{Albums: []albumGrant{}},
	} {
		alice.request("POST", "/api/v1/tokens", tokenInput{Name: "非法权限", Authorization: &policy}, 400)
	}
	// 管理员也可创建个人 Token，但所有权与相册授权不能继承网页登录的全站权限。
	var adminToken struct {
		Token string
		Item  tokenView
	}
	json.Unmarshal(admin.request("POST", "/api/v1/tokens", tokenInput{Name: "管理员个人 Token", Authorization: &all}, 201), &adminToken)
	adminRequest := func(method, path string, body any, status int) []byte {
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer "+adminToken.Token)
		req.Header.Set("Content-Type", "application/json")
		return bearer.send(req, status)
	}
	var adminImage imageView
	json.Unmarshal(bearer.upload(fixture, "admin-personal.png", "", adminToken.Token, 201), &adminImage)
	admin.request("PUT", "/api/v1/albums/"+auth.User.DefaultAlbumID+"/access", map[string]any{"guest_visible": false}, 200)
	adminRequest("GET", adminImage.URL, nil, 200)
	adminRequest("HEAD", adminImage.ThumbnailURL, nil, 200)
	adminRequest("GET", imageB.URL, nil, 404)
	adminRequest("PATCH", "/api/v1/images/"+imageB.ID, map[string]string{"name": "越权"}, 404)
	adminRequest("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{adminImage.ID, imageB.ID}}, 403)
	adminRequest("POST", "/api/v1/images/batch-move", map[string]any{"ids": []string{imageB.ID}, "album_id": auth.User.DefaultAlbumID}, 404)
	bearer.upload(fixture, "admin-other-album.png", albumB.ID, adminToken.Token, 400)
	adminRequest("PATCH", "/api/v1/albums/"+other.DefaultAlbumID, map[string]string{"name": "越权"}, 404)
	adminRequest("PUT", "/api/v1/albums/"+other.DefaultAlbumID+"/default", nil, 404)
	adminRequest("PUT", "/api/v1/albums/"+other.DefaultAlbumID+"/access", map[string]any{"guest_visible": true}, 404)
	adminRequest("DELETE", "/api/v1/albums/"+other.DefaultAlbumID, map[string]string{"action": "delete"}, 404)
	adminRequest("POST", "/api/v1/shares", map[string]string{"kind": "image", "target_id": imageB.ID}, 404)
	adminRequest("DELETE", "/api/v1/shares/"+share.ID, nil, 404)
	json.Unmarshal(adminRequest("GET", "/api/v1/images", nil, 200), &images)
	if images.Total != 1 || images.Items[0].ID != adminImage.ID {
		t.Fatal("管理员个人 Token 读取了他人的图片")
	}
	json.Unmarshal(adminRequest("GET", "/api/v1/stats", nil, 200), &stats)
	if stats.Images != 1 {
		t.Fatal("管理员个人 Token 统计包含他人的图片")
	}
	for _, path := range []string{"/api/v1/images", "/api/v1/albums", "/api/v1/shares", "/api/v1/stats"} {
		adminRequest("GET", path+"?scope=all", nil, 403)
	}
	for _, path := range []string{"/api/v1/tokens", "/api/v1/users", "/api/v1/settings", "/api/v1/storages", "/api/v1/processing", "/api/v1/access", "/api/v1/rate-limits", "/api/v1/auth/me"} {
		adminRequest("GET", path, nil, 401)
	}
	adminRequest("POST", "/api/v1/tokens", map[string]string{"name": "禁止派生"}, 401)
	adminRequest("PUT", "/api/v1/rate-limits", map[string]any{"rules": defaultRateLimits()}, 401)
	adminRequest("PUT", "/api/v1/tokens/"+issued.Item.ID, input, 401)
	adminRequest("DELETE", "/api/v1/tokens/"+issued.Item.ID, nil, 401)
	adminRequest("DELETE", "/api/v1/tokens/"+issued.Item.ID+"/record", nil, 401)
	adminRequest("PUT", "/api/v1/auth/password", map[string]string{"new_password": "2"}, 401)
	// 即使图片属于管理员，也不能撤销其他用户为该图片创建的历史分享。
	foreignShare := database.Share{ID: randomID(), UserID: owner.ID, Kind: "image", TargetID: adminImage.ID, TokenHash: digest(randomHex(32))}
	if err = db.Create(&foreignShare).Error; err != nil {
		t.Fatal(err)
	}
	adminRequest("DELETE", "/api/v1/shares/"+foreignShare.ID, nil, 404)
	var adminAlbum database.Album
	json.Unmarshal(adminRequest("POST", "/api/v1/albums", map[string]string{"name": "管理员另一个相册"}, 201), &adminAlbum)
	var outsideImage imageView
	json.Unmarshal(admin.upload(fixture, "outside-grant.png", adminAlbum.ID, "", 201), &outsideImage)
	selected := tokenAuthorization{Albums: []albumGrant{{AlbumID: auth.User.DefaultAlbumID, Permissions: tokenPermissions}}}
	adminInput := tokenInput{Name: "管理员指定相册", Authorization: &selected}
	alice.request("PUT", "/api/v1/tokens/"+adminToken.Item.ID, adminInput, 404)
	admin.request("PUT", "/api/v1/tokens/"+adminToken.Item.ID, adminInput, 200)
	adminRequest("GET", outsideImage.URL, nil, 404)
	adminRequest("PATCH", "/api/v1/images/"+outsideImage.ID, map[string]string{"name": "未授权"}, 404)
	adminRequest("PATCH", "/api/v1/albums/"+adminAlbum.ID, map[string]string{"name": "未授权"}, 404)
	bearer.upload(fixture, "outside-grant.png", adminAlbum.ID, adminToken.Token, 403)
	adminRequest("PATCH", "/api/v1/images/"+adminImage.ID, map[string]string{"name": "可修改"}, 200)
	adminInput.Authorization = &readOnly
	admin.request("PUT", "/api/v1/tokens/"+adminToken.Item.ID, adminInput, 200)
	adminRequest("PATCH", "/api/v1/images/"+adminImage.ID, map[string]string{"name": "只读"}, 403)
	// 个人列表只返回本人 Token，管理员全站列表继续可管理其他账号。
	var tokens struct{ Items []tokenView }
	json.Unmarshal(admin.request("GET", "/api/v1/tokens", nil, 200), &tokens)
	if len(tokens.Items) != 1 || tokens.Items[0].ID != adminToken.Item.ID {
		t.Fatal("管理员个人 Token 列表混入其他用户")
	}
	json.Unmarshal(admin.request("GET", "/api/v1/tokens?scope=all", nil, 200), &tokens)
	if len(tokens.Items) != 2 {
		t.Fatal("管理员全站 Token 列表缺少用户记录")
	}
	var beforeImages, beforeShares int64
	db.Model(&database.Image{}).Count(&beforeImages)
	db.Model(&database.Share{}).Count(&beforeShares)
	admin.request("DELETE", "/api/v1/tokens/"+adminToken.Item.ID+"/record", nil, 409)
	admin.request("DELETE", "/api/v1/tokens/"+adminToken.Item.ID, nil, 200)
	adminRequest("GET", "/api/v1/images", nil, 401)
	alice.request("DELETE", "/api/v1/tokens/"+adminToken.Item.ID+"/record", nil, 404)
	admin.request("DELETE", "/api/v1/tokens/"+adminToken.Item.ID+"/record", nil, 200)
	admin.request("DELETE", "/api/v1/tokens/"+adminToken.Item.ID+"/record", nil, 404)
	// 过期、撤销和停用保持有效，即使请求同时携带合法会话也不能回退。
	until := time.Now().UTC().Add(-time.Minute)
	db.Model(&database.APIToken{}).Where("id = ?", issued.Item.ID).Update("expires_at", until)
	alice.request("DELETE", "/api/v1/tokens/"+issued.Item.ID+"/record", nil, 409)
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/images", nil)
	req.Header.Set("Authorization", "Bearer "+issued.Token)
	alice.send(req, 401)
	db.Model(&database.APIToken{}).Where("id = ?", issued.Item.ID).Update("expires_at", nil)
	admin.request("DELETE", "/api/v1/tokens/"+issued.Item.ID, nil, 200)
	request("GET", "/api/v1/images", nil, 401)
	// 已撤销记录可由本人删除；其他用户及缺少 CSRF 的会话不能删除。
	bob.request("DELETE", "/api/v1/tokens/"+issued.Item.ID+"/record", nil, 404)
	csrfRequest, _ := http.NewRequest("DELETE", server.URL+"/api/v1/tokens/"+issued.Item.ID+"/record", nil)
	alice.send(csrfRequest, 403)
	alice.request("DELETE", "/api/v1/tokens/"+issued.Item.ID+"/record", nil, 200)
	request("GET", "/api/v1/images", nil, 401)
	json.Unmarshal(alice.request("GET", "/api/v1/tokens?status=revoked", nil, 200), &tokens)
	if len(tokens.Items) != 0 {
		t.Fatal("删除后列表仍包含已撤销的 Token")
	}
	var permanent struct {
		Token string
		Item  tokenView
	}
	json.Unmarshal(alice.request("POST", "/api/v1/tokens", map[string]string{"name": "停用账号"}, 201), &permanent)
	alice.request("DELETE", "/api/v1/tokens/"+permanent.Item.ID+"/record", nil, 409)
	admin.request("PUT", "/api/v1/users/"+owner.ID, userInput{Enabled: false}, 200)
	bearer.upload(fixture, "disabled.png", "", permanent.Token, 401)
	admin.request("DELETE", "/api/v1/tokens/"+permanent.Item.ID+"/record", nil, 200)
	var remainingTokens, remainingImages, remainingShares int64
	db.Model(&database.APIToken{}).Count(&remainingTokens)
	db.Model(&database.Image{}).Count(&remainingImages)
	db.Model(&database.Share{}).Count(&remainingShares)
	if remainingTokens != 0 || remainingImages != beforeImages || remainingShares != beforeShares {
		t.Fatal("Token 记录未删除或误删了其他资源")
	}
	if strings.Contains(stored.Authorization, "admin") {
		t.Fatal("存在管理级授权")
	}
}
