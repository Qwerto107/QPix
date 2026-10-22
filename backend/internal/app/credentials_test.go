package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"qpix/internal/database"
)

func testPasswordPolicyAndTokenExpiry(t *testing.T, a *App, admin *testClient, fixture []byte) {
	t.Helper()
	input := userInput{Username: "short-password", Enabled: true, Password: "1", ConfirmPassword: "2"}
	admin.request("POST", "/api/v1/users", input, 400)
	input.Password, input.ConfirmPassword = "", ""
	admin.request("POST", "/api/v1/users", input, 400)
	input.Password, input.ConfirmPassword = strings.Repeat("汉", 25), strings.Repeat("汉", 25)
	admin.request("POST", "/api/v1/users", input, 400)
	input.Password, input.ConfirmPassword = "1", "1"
	var user struct{ ID string }
	if err := json.Unmarshal(admin.request("POST", "/api/v1/users", input, 200), &user); err != nil {
		t.Fatal(err)
	}
	member := memberClient(t, admin.server, input.Username, "1")
	input.Password, input.ConfirmPassword = "好", "好"
	admin.request("PUT", "/api/v1/users/"+user.ID, input, 200)
	member.request("GET", "/api/v1/auth/me", nil, 401)
	member = memberClient(t, admin.server, input.Username, "好")
	for _, password := range []string{"", strings.Repeat("汉", 25)} {
		member.request("PUT", "/api/v1/auth/password", map[string]string{"current_password": "好", "new_password": password}, 400)
	}
	member.request("PUT", "/api/v1/auth/password", map[string]string{"current_password": "好", "new_password": "2"}, 200)
	member = memberClient(t, admin.server, input.Username, "2")
	var permanent struct {
		Token string
		Item  database.APIToken
	}
	if err := json.Unmarshal(member.request("POST", "/api/v1/tokens", map[string]any{"name": "永久上传", "expires_at": nil}, 201), &permanent); err != nil {
		t.Fatal(err)
	}
	if permanent.Item.ExpiresAt != nil {
		t.Fatal("永久 Token 应以 NULL 保存到期时间")
	}
	// 自定义时刻统一保存 UTC，过期和撤销都必须影响后续上传。
	expires := time.Now().In(time.FixedZone("UTC+8", 8*3600)).Add(2 * time.Hour).Truncate(time.Second)
	var custom struct {
		Token string
		Item  database.APIToken
	}
	if err := json.Unmarshal(member.request("POST", "/api/v1/tokens", map[string]any{"name": "自定义上传", "expires_at": expires}, 201), &custom); err != nil {
		t.Fatal(err)
	}
	if custom.Item.ExpiresAt == nil || !custom.Item.ExpiresAt.Equal(expires) || custom.Item.ExpiresAt.Location() != time.UTC {
		t.Fatal("Token 自定义时刻或时区转换错误")
	}
	member.request("POST", "/api/v1/tokens", map[string]any{"name": "过期", "expires_at": time.Now().Add(-time.Minute)}, 400)
	member.request("POST", "/api/v1/tokens", map[string]any{"name": "无效", "expires_at": "invalid"}, 400)
	anonymous := &testClient{t: t, server: admin.server, client: &http.Client{}}
	a.Maintain(context.Background())
	var image imageView
	if err := json.Unmarshal(anonymous.upload(fixture, "permanent.png", "", permanent.Token, 201), &image); err != nil {
		t.Fatal(err)
	}
	admin.request("POST", "/api/v1/shares", map[string]any{"kind": "image", "target_id": image.ID, "password": strings.Repeat("汉", 25)}, 400)
	var share shareView
	if err := json.Unmarshal(admin.request("POST", "/api/v1/shares", map[string]any{"kind": "image", "target_id": image.ID, "password": "1"}, 201), &share); err != nil {
		t.Fatal(err)
	}
	anonymous.request("POST", "/api/v1/public-shares/"+strings.TrimPrefix(share.URL, "/s/")+"/unlock", map[string]string{"password": "1"}, 200)
	if err := a.db.Model(&database.APIToken{}).Where("id = ?", custom.Item.ID).Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	anonymous.upload(fixture, "expired.png", "", custom.Token, 401)
	// 修改本人密码只撤销会话，用户 Token 继续保持原权限。
	member.request("PUT", "/api/v1/auth/password", map[string]string{"current_password": "2", "new_password": "3"}, 200)
	member.request("GET", "/api/v1/auth/me", nil, 401)
	var afterPassword imageView
	if err := json.Unmarshal(anonymous.upload(fixture, "after-password.png", "", permanent.Token, 201), &afterPassword); err != nil {
		t.Fatal(err)
	}
	admin.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{afterPassword.ID}}, 200)
	admin.request("DELETE", "/api/v1/tokens/"+permanent.Item.ID, nil, 200)
	anonymous.upload(fixture, "revoked.png", "", permanent.Token, 401)
	admin.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{image.ID}}, 200)
}
