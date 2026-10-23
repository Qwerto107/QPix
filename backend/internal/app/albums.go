package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"qpix/internal/database"
)

// 每个账号都持有一个自己的默认相册，上传兜底与网页入口使用同一规则。
func ensureDefaultAlbum(db *gorm.DB, user *database.User) error {
	if user.DefaultAlbumID != "" {
		var n int64
		if err := db.Model(&database.Album{}).Where("id = ? AND user_id = ?", user.DefaultAlbumID, user.ID).Count(&n).Error; err != nil {
			return err
		}
		if n == 1 {
			return nil
		}
	}
	album := database.Album{ID: randomID(), UserID: user.ID, Name: "默认相册", GuestVisible: true, RefererMode: "inherit", AllowEmptyReferer: true}
	if err := db.Create(&album).Error; err != nil {
		return err
	}
	user.DefaultAlbumID = album.ID
	return db.Model(user).Update("default_album_id", album.ID).Error
}
func (a *App) initPersonalAlbums() error {
	var users []database.User
	if err := a.db.Find(&users).Error; err != nil {
		return err
	}
	return a.db.Transaction(func(tx *gorm.DB) error {
		for _, user := range users {
			if err := ensureDefaultAlbum(tx, &user); err != nil {
				return err
			}
			if err := tx.Model(&database.Image{}).Where("user_id = ? AND album_id IS NULL", user.ID).Update("album_id", user.DefaultAlbumID).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
func (a *App) defaultAlbum(r *http.Request, userID string) (string, error) {
	var user database.User
	err := a.db.WithContext(r.Context()).Select("default_album_id").First(&user, "id = ?", userID).Error
	return user.DefaultAlbumID, err
}
func (a *App) setDefaultAlbum(w http.ResponseWriter, r *http.Request, p principal) {
	var album database.Album
	if err := a.visibleAlbums(a.db.WithContext(r.Context()), p).First(&album, "id = ?", r.PathValue("id")).Error; err != nil {
		fail(w, 404, "相册不存在")
		return
	}
	if err := a.db.WithContext(r.Context()).Model(&database.User{}).Where("id = ?", album.UserID).Update("default_album_id", album.ID).Error; err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (a *App) updateAlbumAccess(w http.ResponseWriter, r *http.Request, p principal) {
	var input struct {
		GuestVisible      bool     `json:"guest_visible"`
		RefererMode       string   `json:"referer_mode"`
		Referers          []string `json:"referers"`
		AllowEmptyReferer bool     `json:"allow_empty_referer"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.RefererMode == "" {
		input.RefererMode = "inherit"
	}
	if input.RefererMode != "inherit" && input.RefererMode != "custom" && input.RefererMode != "unrestricted" {
		fail(w, 400, "无效的 Referer 策略")
		return
	}
	rule, err := compileRule(ruleInput{Scope: "images", RefererEnabled: input.RefererMode == "custom", Referers: input.Referers, AllowEmptyReferer: input.AllowEmptyReferer})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	var album database.Album
	db := a.db.WithContext(r.Context())
	if err = a.visibleAlbums(db, p).First(&album, "id = ?", r.PathValue("id")).Error; err != nil {
		fail(w, 404, "相册不存在")
		return
	}
	refs, _ := json.Marshal(rule.input.Referers)
	if err = db.Model(&album).Updates(map[string]any{"guest_visible": input.GuestVisible, "referer_mode": input.RefererMode, "referers": string(refs), "allow_empty_referer": input.AllowEmptyReferer}).Error; err != nil {
		internal(w, err)
		return
	}
	a.invalidateAlbumRule(album.ID)
	respond(w, 200, map[string]bool{"ok": true})
}
func (a *App) deleteAlbum(w http.ResponseWriter, r *http.Request, p principal) {
	var input struct {
		Action   string `json:"action"`
		TargetID string `json:"target_id"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Action != "move" && input.Action != "delete" {
		fail(w, 400, "请选择转移图片或连同图片删除")
		return
	}
	db := a.db.WithContext(r.Context())
	var album database.Album
	if err := a.visibleAlbums(db, p).First(&album, "id = ?", r.PathValue("id")).Error; err != nil {
		fail(w, 404, "相册不存在")
		return
	}
	if p.Token != nil {
		var foreign int64
		if err := db.Model(&database.Image{}).Where("album_id = ? AND user_id <> ?", album.ID, p.User.ID).Count(&foreign).Error; err != nil {
			internal(w, err)
			return
		}
		if foreign > 0 {
			fail(w, 403, "相册含有非本人图片，请通过界面处理")
			return
		}
	}
	var owner database.User
	if err := db.First(&owner, "id = ?", album.UserID).Error; err != nil {
		internal(w, err)
		return
	}
	if owner.DefaultAlbumID == album.ID {
		fail(w, 409, "请先指定另一个默认相册")
		return
	}
	if input.Action == "delete" && !requireTokenPermission(w, p, &album.ID, tokenDelete) {
		return
	}
	if input.Action == "move" {
		if !requireTokenPermission(w, p, &album.ID, tokenEdit) || !requireTokenPermission(w, p, &input.TargetID, tokenUpload) {
			return
		}
		var target database.Album
		if input.TargetID == album.ID || db.First(&target, "id = ? AND user_id = ?", input.TargetID, album.UserID).Error != nil {
			fail(w, 400, "请选择该账号的其他相册")
			return
		}
	}
	var deletedImages []database.Image
	err := db.Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&database.Image{}).Where("album_id = ?", album.ID)
		if input.Action == "delete" {
			if err := q.Session(&gorm.Session{}).Find(&deletedImages).Error; err != nil {
				return err
			}
			if err := q.Updates(map[string]any{"state": "deleting", "album_id": nil}).Error; err != nil {
				return err
			}
		} else {
			if err := q.Update("album_id", input.TargetID).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&album).Error
	})
	if err != nil {
		internal(w, err)
		return
	}
	// 先标记再清理，存储暂不可用时由后台重试，原图及分享网关立即停止读取。
	a.invalidateAlbumRule(album.ID)
	releasePolicy(w)
	if len(deletedImages) > 0 {
		a.cleanupImages(r.Context(), deletedImages)
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (a *App) moveImages(w http.ResponseWriter, r *http.Request, p principal) {
	var input struct {
		IDs     []string `json:"ids"`
		AlbumID string   `json:"album_id"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.IDs) < 1 || len(input.IDs) > 100 {
		fail(w, 400, "每次请选择 1–100 张图片")
		return
	}
	db := a.db.WithContext(r.Context())
	var target database.Album
	targetPrincipal := p
	targetPrincipal.TokenOperation = tokenUpload
	if err := a.visibleAlbums(db, targetPrincipal).First(&target, "id = ?", input.AlbumID).Error; err != nil {
		fail(w, 404, "目标相册不存在")
		return
	}
	if !requireTokenPermission(w, p, &target.ID, tokenUpload) {
		return
	}
	var rows []database.Image
	if err := a.manageableImages(db, p).Where("id IN ? AND state = ?", input.IDs, "ready").Find(&rows).Error; err != nil {
		internal(w, err)
		return
	}
	unique := map[string]bool{}
	for _, id := range input.IDs {
		unique[id] = true
	}
	if len(rows) != len(unique) {
		fail(w, 404, "部分图片不存在或没有管理权限")
		return
	}
	// 账号之间不转移资源归属，管理员也在上传者自己的相册内整理。
	for _, row := range rows {
		if row.UserID != target.UserID {
			fail(w, 400, "请选择同一上传者的图片，并转入该账号的相册")
			return
		}
	}
	if err := db.Model(&database.Image{}).Where("id IN ?", input.IDs).Update("album_id", target.ID).Error; err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

// 外链只覆盖 Referer，始终保留全局 IP 规则；规则错误时关闭访问。
func (a *App) enforceAlbumAccess(w http.ResponseWriter, r *http.Request, album *database.Album) bool {
	if a.cfg.DisableAccessRules {
		return true
	}
	a.settingsMu.RLock()
	rule := a.accessRules["images"]
	a.settingsMu.RUnlock()
	guestVisible := false
	if album != nil {
		guestVisible = album.GuestVisible
		if album.RefererMode == "unrestricted" {
			rule.input.RefererEnabled = false
		}
		if album.RefererMode == "custom" {
			custom, err := a.compiledAlbumRule(*album)
			if err != nil {
				internal(w, err)
				return false
			}
			rule.input.RefererEnabled = true
			rule.input.Referers = custom.input.Referers
			rule.input.AllowEmptyReferer = custom.input.AllowEmptyReferer
		}
	}
	if !rule.allows(a.requesterIP(r), r.Header.Get("Referer")) {
		fail(w, 403, "请求不满足相册访问规则")
		return false
	}
	// 只允许无网络限制的访客直链在浏览器保存，并要求每次回源校验；禁止共享缓存。
	if guestVisible && !rule.input.IPEnabled && !rule.input.RefererEnabled && r.Header.Get("Authorization") == "" {
		w.Header().Set("Cache-Control", "private, no-cache, must-revalidate")
	}
	return true
}

// 仅白名单字段可参与排序，避免将查询参数拼接为任意 SQL。
func recordOrder(r *http.Request) string {
	allowed := map[string]string{"name": "name", "created_at": "created_at"}
	if r.URL.Path == "/api/v1/images" {
		allowed["size"] = "size"
	} else {
		allowed["expires_at"] = "expires_at"
		allowed["last_used_at"] = "last_used_at"
	}
	col := allowed[r.URL.Query().Get("sort")]
	if col == "" {
		col = "created_at"
	}
	dir := "DESC"
	if r.URL.Query().Get("order") == "asc" {
		dir = "ASC"
	}
	return col + " " + dir + ", id DESC"
}
func listPage(r *http.Request) (int, int) {
	page, limit := 1, 20
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 && v <= 1000000 {
		page = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 100 {
		limit = v
	}
	return page, limit
}
func filterRecords(q *gorm.DB, r *http.Request, col string, expires bool) *gorm.DB {
	if owner := r.URL.Query().Get("user_id"); owner != "" {
		q = q.Where("user_id = ?", owner)
	}
	if search := strings.TrimSpace(r.URL.Query().Get("q")); search != "" {
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(search))
		q = q.Where("LOWER("+col+") LIKE ? ESCAPE '!'", "%"+escaped+"%")
	}
	if expires {
		switch r.URL.Query().Get("status") {
		case "active":
			q = q.Where("revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", time.Now().UTC())
		case "expired":
			q = q.Where("revoked_at IS NULL AND expires_at <= ?", time.Now().UTC())
		case "revoked":
			q = q.Where("revoked_at IS NOT NULL")
		}
	}
	return q
}
