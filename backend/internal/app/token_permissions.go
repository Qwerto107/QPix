package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
	"qpix/internal/database"
)

const (
	tokenRead   = "read"
	tokenUpload = "upload"
	tokenEdit   = "edit"
	tokenDelete = "delete"
	tokenAlbums = "albums"
	tokenShare  = "share"
)

var tokenPermissions = []string{tokenRead, tokenUpload, tokenEdit, tokenDelete, tokenAlbums, tokenShare}

type albumGrant struct {
	AlbumID     string   `json:"album_id"`
	Permissions []string `json:"permissions"`
}
type tokenAuthorization struct {
	AllAlbums   bool         `json:"all_albums"`
	Permissions []string     `json:"permissions"`
	Albums      []albumGrant `json:"albums"`
}

func defaultTokenAuthorization() tokenAuthorization {
	return tokenAuthorization{AllAlbums: true, Permissions: []string{tokenUpload}, Albums: []albumGrant{}}
}
func includesPermission(permissions []string, op string) bool {
	for _, value := range permissions {
		if value == op {
			return true
		}
	}
	return false
}
func (policy tokenAuthorization) allows(id, op string) bool {
	if policy.AllAlbums {
		return includesPermission(policy.Permissions, op)
	}
	for _, grant := range policy.Albums {
		if grant.AlbumID == id {
			return includesPermission(grant.Permissions, op)
		}
	}
	return false
}
func (policy tokenAuthorization) any(op string) bool {
	if policy.AllAlbums {
		return policy.allows("", op)
	}
	for _, grant := range policy.Albums {
		if includesPermission(grant.Permissions, op) {
			return true
		}
	}
	return false
}
func normalizePermissions(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > len(tokenPermissions) {
		return nil, errors.New("至少选择一种操作权限")
	}
	for _, value := range values {
		if !includesPermission(tokenPermissions, value) {
			return nil, errors.New("包含未知的 Token 权限")
		}
	}
	result := []string{}
	for _, value := range tokenPermissions {
		if includesPermission(values, value) {
			result = append(result, value)
		}
	}
	return result, nil
}
func validateTokenAuthorization(db *gorm.DB, userID string, input tokenAuthorization) (tokenAuthorization, error) {
	if input.Permissions == nil {
		input.Permissions = []string{}
	}
	if input.Albums == nil {
		input.Albums = []albumGrant{}
	}
	if input.AllAlbums {
		if len(input.Albums) != 0 {
			return input, errors.New("全部相册模式不能同时设置单独相册")
		}
		permissions, err := normalizePermissions(input.Permissions)
		input.Permissions = permissions
		return input, err
	}
	if len(input.Permissions) != 0 || len(input.Albums) < 1 || len(input.Albums) > 200 {
		return input, errors.New("指定相册模式请选择 1–200 个相册，并分别设置权限")
	}
	ids := []string{}
	seen := map[string]bool{}
	for i, grant := range input.Albums {
		if grant.AlbumID == "" || seen[grant.AlbumID] {
			return input, errors.New("相册不能为空或重复")
		}
		seen[grant.AlbumID] = true
		ids = append(ids, grant.AlbumID)
		permissions, err := normalizePermissions(grant.Permissions)
		if err != nil {
			return input, err
		}
		input.Albums[i].Permissions = permissions
	}
	var count int64
	if err := db.Model(&database.Album{}).Where("user_id = ? AND id IN ?", userID, ids).Count(&count).Error; err != nil {
		return input, err
	}
	if count != int64(len(ids)) {
		return input, errors.New("只能授权该用户拥有的相册")
	}
	return input, nil
}
func (p principal) allowsToken(albumID *string, op string) bool {
	if p.Token == nil {
		return true
	}
	return albumID != nil && p.Token.allows(*albumID, op)
}
func requireTokenPermission(w http.ResponseWriter, p principal, id *string, op string) bool {
	if !p.allowsToken(id, op) {
		fail(w, 403, "Token 未获得该相册的操作权限")
		return false
	}
	return true
}

// 查询范围在数据库中收紧；禁止先读取完整列表再由客户端隐藏。
func tokenAlbumQuery(db *gorm.DB, p principal, column, op string) *gorm.DB {
	if p.Token == nil {
		return db
	}
	// 同时验证相册所有权，不能把历史上传到他人相册的图片视为“全部本人相册”。
	owned := db.Session(&gorm.Session{NewDB: true}).Model(&database.Album{}).Select("id").Where("user_id = ?", p.User.ID)
	if p.Token.AllAlbums {
		if !p.Token.any(op) {
			return db.Where("1 = 0")
		}
		return db.Where(column+" IN (?)", owned)
	}
	ids := []string{}
	for _, grant := range p.Token.Albums {
		if includesPermission(grant.Permissions, op) {
			ids = append(ids, grant.AlbumID)
		}
	}
	if len(ids) == 0 {
		return db.Where("1 = 0")
	}
	return db.Where(column+" IN (?)", owned.Where("id IN ?", ids))
}
func (a *App) authenticateToken(w http.ResponseWriter, r *http.Request, p *principal) bool {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		fail(w, 401, "Token 凭据格式无效")
		return false
	}
	var row database.APIToken
	db := a.db.WithContext(r.Context())
	if err := db.First(&row, "hash = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", digest(strings.TrimPrefix(header, "Bearer ")), time.Now().UTC()).Error; err != nil {
		authError(w, err, "Token 无效、已撤销或已到期")
		return false
	}
	if err := db.First(&p.User, "id = ?", row.UserID).Error; err != nil {
		authError(w, err, "账号不存在")
		return false
	}
	if p.User.Role != "user" && p.User.Role != "admin" {
		fail(w, 403, "此账号不能使用个人 Token")
		return false
	}
	var policy tokenAuthorization
	if err := json.Unmarshal([]byte(row.Authorization), &policy); err != nil {
		fail(w, 401, "Token 权限配置无效")
		return false
	}
	p.Token = &policy
	p.TokenID = row.ID
	return true
}
func authorizeTokenRoute(w http.ResponseWriter, r *http.Request, p *principal) bool {
	op := map[string]string{
		"GET /api/v1/images":                tokenRead,
		"POST /api/v1/images":               tokenUpload,
		"PATCH /api/v1/images/{id}":         tokenEdit,
		"POST /api/v1/images/batch-delete":  tokenDelete,
		"POST /api/v1/images/batch-move":    tokenEdit,
		"GET /api/v1/albums":                tokenRead,
		"POST /api/v1/albums":               tokenAlbums,
		"PATCH /api/v1/albums/{id}":         tokenAlbums,
		"DELETE /api/v1/albums/{id}":        tokenAlbums,
		"PUT /api/v1/albums/{id}/access":    tokenAlbums,
		"PUT /api/v1/albums/{id}/default":   tokenAlbums,
		"GET /api/v1/stats":                 tokenRead,
		"GET /api/v1/upload-options":        tokenUpload,
		"GET /api/v1/shares":                tokenShare,
		"POST /api/v1/shares":               tokenShare,
		"DELETE /api/v1/shares/{id}":        tokenShare,
		"DELETE /api/v1/shares/{id}/record": tokenShare,
		"GET /i/{file}":                     tokenRead,
	}[r.Pattern]
	if op == "" || !p.Token.any(op) {
		fail(w, 403, "Token 未获得此操作权限")
		return false
	}
	if r.Pattern == "POST /api/v1/albums" && !p.Token.AllAlbums {
		fail(w, 403, "指定相册的 Token 不能创建新相册")
		return false
	}
	p.TokenOperation = op
	return true
}

type tokenView struct {
	database.APIToken
	Authorization tokenAuthorization `json:"authorization"`
	OwnerID       string             `json:"owner_id"`
	OwnerName     string             `json:"owner_name"`
	OwnerRole     string             `json:"owner_role"`
}

func (a *App) tokenView(row database.APIToken, owner database.User) (tokenView, error) {
	var policy tokenAuthorization
	err := json.Unmarshal([]byte(row.Authorization), &policy)
	return tokenView{APIToken: row, Authorization: policy, OwnerID: row.UserID, OwnerName: owner.Username, OwnerRole: owner.Role}, err
}

type tokenInput struct {
	Name          string              `json:"name"`
	ExpiresAt     *time.Time          `json:"expires_at"`
	Authorization *tokenAuthorization `json:"authorization"`
}

func (a *App) updateToken(w http.ResponseWriter, r *http.Request, p principal) {
	var input tokenInput
	if !decode(w, r, &input) {
		return
	}
	db := a.db.WithContext(r.Context())
	q := db.Model(&database.APIToken{}).Where("id = ?", r.PathValue("id"))
	if p.User.Role != "admin" {
		q = q.Where("user_id = ?", p.User.ID)
	}
	var row database.APIToken
	if err := q.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, 404, "Token 不存在")
		} else {
			internal(w, err)
		}
		return
	}
	if row.RevokedAt != nil {
		fail(w, 409, "已撤销的 Token 不能修改")
		return
	}
	var owner database.User
	if err := db.First(&owner, "id = ?", row.UserID).Error; err != nil {
		internal(w, err)
		return
	}
	if input.Authorization == nil {
		fail(w, 400, "请提供 Token 权限配置")
		return
	}
	policy, ok := a.validateTokenInput(w, r, owner.ID, &input)
	if !ok {
		return
	}
	encoded, _ := json.Marshal(policy)
	if err := db.Model(&row).Updates(map[string]any{"name": input.Name, "expires_at": input.ExpiresAt, "authorization": string(encoded)}).Error; err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (a *App) validateTokenInput(w http.ResponseWriter, r *http.Request, userID string, input *tokenInput) (tokenAuthorization, bool) {
	policy := defaultTokenAuthorization()
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len([]rune(input.Name)) > 64 {
		fail(w, 400, "Token 名称长度必须在 1–64 个字符之间")
		return policy, false
	}
	if input.ExpiresAt != nil {
		utc := input.ExpiresAt.UTC()
		if !utc.After(time.Now()) || utc.Year() > 9999 {
			fail(w, 400, "到期时间必须晚于当前时间，且年份不能超过 9999")
			return policy, false
		}
		input.ExpiresAt = &utc
	}
	if input.Authorization != nil {
		policy = *input.Authorization
	}
	policy, err := validateTokenAuthorization(a.db.WithContext(r.Context()), userID, policy)
	if err != nil {
		fail(w, 400, err.Error())
		return policy, false
	}
	return policy, true
}
