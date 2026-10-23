package app

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"qpix/internal/config"
	"qpix/internal/database"
)

func (a *App) listAlbums(w http.ResponseWriter, r *http.Request, p principal) {
	all, ok := allScope(w, r, p)
	if !ok {
		return
	}
	db := a.db.WithContext(r.Context())
	var albums []database.Album
	q := tokenAlbumQuery(ownerQuery(db.Model(&database.Album{}), p, all), p, "id", tokenRead)
	if owner := r.URL.Query().Get("user_id"); all && owner != "" {
		q = q.Where("user_id = ?", owner)
	}
	if err := q.Order("created_at ASC").Find(&albums).Error; err != nil {
		internal(w, err)
		return
	}
	items := make([]map[string]any, 0, len(albums))
	ownerIDs := make([]string, 0, len(albums))
	albumIDs := make([]string, 0, len(albums))
	for _, album := range albums {
		ownerIDs = append(ownerIDs, album.UserID)
		albumIDs = append(albumIDs, album.ID)
	}
	owners, err := usersByID(db, ownerIDs)
	if err != nil {
		internal(w, err)
		return
	}
	var totals []struct {
		AlbumID string
		Count   int64
	}
	if len(albumIDs) > 0 {
		if err := ownerQuery(db.Model(&database.Image{}), p, all).Select("album_id, COUNT(*) AS count").Where("album_id IN ? AND state = ?", albumIDs, "ready").Group("album_id").Scan(&totals).Error; err != nil {
			internal(w, err)
			return
		}
	}
	counts := make(map[string]int64, len(totals))
	for _, total := range totals {
		counts[total.AlbumID] = total.Count
	}
	for _, album := range albums {
		owner, exists := owners[album.UserID]
		if !exists {
			internal(w, gorm.ErrRecordNotFound)
			return
		}
		refs := []string{}
		if album.Referers != "" {
			if err := json.Unmarshal([]byte(album.Referers), &refs); err != nil {
				internal(w, err)
				return
			}
		}
		items = append(items, map[string]any{"id": album.ID, "name": album.Name, "count": counts[album.ID], "created_at": album.CreatedAt, "guest_visible": album.GuestVisible, "can_upload": p.allowsToken(&album.ID, tokenUpload), "owner_id": album.UserID, "owner_name": owner.Username, "owner_role": owner.Role, "is_default": owner.DefaultAlbumID == album.ID, "referer_mode": album.RefererMode, "referers": refs, "allow_empty_referer": album.AllowEmptyReferer})
	}
	respond(w, 200, items)
}

func albumName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var input struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &input) {
		return "", false
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		fail(w, 400, "相册名称长度必须在 1–100 个字符之间")
		return "", false
	}
	return name, true
}

func (a *App) createAlbum(w http.ResponseWriter, r *http.Request, p principal) {
	name, ok := albumName(w, r)
	if !ok {
		return
	}
	album := database.Album{ID: randomID(), UserID: p.User.ID, Name: name, GuestVisible: true, RefererMode: "inherit", AllowEmptyReferer: true}
	if err := a.db.WithContext(r.Context()).Create(&album).Error; err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, album)
}

func (a *App) renameAlbum(w http.ResponseWriter, r *http.Request, p principal) {
	name, ok := albumName(w, r)
	if !ok {
		return
	}
	db := a.db.WithContext(r.Context())
	var album database.Album
	if err := a.visibleAlbums(db, p).First(&album, "id = ?", r.PathValue("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, 404, "相册不存在")
		} else {
			internal(w, err)
		}
		return
	}
	if err := db.Model(&album).Update("name", name).Error; err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) listTokens(w http.ResponseWriter, r *http.Request, p principal) {
	all, ok := allScope(w, r, p)
	if !ok {
		return
	}
	db := a.db.WithContext(r.Context())
	q := ownerQuery(db.Model(&database.APIToken{}), p, all)
	q = filterRecords(q, r, "name", true)
	_, limit := listPage(r)
	rows, pagination, err := paginate[database.APIToken](q, r, limit, recordOrder(r))
	if err != nil {
		paginationError(w, err)
		return
	}
	items := make([]tokenView, 0, len(rows))
	ownerIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		ownerIDs = append(ownerIDs, row.UserID)
	}
	owners, err := usersByID(db, ownerIDs)
	if err != nil {
		internal(w, err)
		return
	}
	for _, row := range rows {
		owner, exists := owners[row.UserID]
		if !exists {
			internal(w, gorm.ErrRecordNotFound)
			return
		}
		item, err := a.tokenView(row, owner)
		if err != nil {
			internal(w, err)
			return
		}
		items = append(items, item)
	}
	respond(w, 200, pagination.response(items))
}

func (a *App) createToken(w http.ResponseWriter, r *http.Request, p principal) {
	var input tokenInput
	if !decode(w, r, &input) {
		return
	}
	policy, ok := a.validateTokenInput(w, r, p.User.ID, &input)
	if !ok {
		return
	}
	encoded, _ := json.Marshal(policy)
	raw := "qpix_" + randomHex(32)
	row := database.APIToken{ID: randomID(), UserID: p.User.ID, Name: input.Name, Hash: digest(raw), Prefix: raw[:13], ExpiresAt: input.ExpiresAt, Authorization: string(encoded)}
	if err := a.db.WithContext(r.Context()).Create(&row).Error; err != nil {
		internal(w, err)
		return
	}
	item, err := a.tokenView(row, p.User)
	if err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, map[string]any{"token": raw, "item": item})
}

func (a *App) revokeToken(w http.ResponseWriter, r *http.Request, p principal) {
	q := a.db.WithContext(r.Context()).Model(&database.APIToken{}).Where("id = ?", r.PathValue("id"))
	if p.User.Role != "admin" {
		q = q.Where("user_id = ?", p.User.ID)
	}
	result := q.Where("revoked_at IS NULL").Update("revoked_at", time.Now().UTC())
	if result.Error != nil {
		internal(w, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		fail(w, 404, "Token 不存在或已撤销")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) deleteTokenRecord(w http.ResponseWriter, r *http.Request, p principal) {
	db := a.db.WithContext(r.Context())
	q := db.Where("id = ?", r.PathValue("id"))
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
	if row.RevokedAt == nil {
		fail(w, 409, "请先撤销 Token，再删除记录")
		return
	}
	// 删除只清理已失效的凭据记录，不能影响图片、分享或其他 Token。
	result := db.Where("id = ? AND revoked_at IS NOT NULL", row.ID).Delete(&database.APIToken{})
	if result.Error != nil {
		internal(w, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		fail(w, 409, "Token 状态已变化，请刷新列表")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) stats(w http.ResponseWriter, r *http.Request, p principal) {
	all, ok := allScope(w, r, p)
	if !ok {
		return
	}
	db := a.db.WithContext(r.Context())
	var totals struct {
		Images int64 `json:"images"`
		Bytes  int64 `json:"bytes"`
	}
	if err := tokenAlbumQuery(ownerQuery(db.Model(&database.Image{}), p, all), p, "album_id", tokenRead).Where("state = ?", "ready").Select("COUNT(*) AS images, COALESCE(SUM(size),0) AS bytes").Scan(&totals).Error; err != nil {
		internal(w, err)
		return
	}
	var albums, today, deleting int64
	for _, item := range []struct {
		q *gorm.DB
		n *int64
	}{
		{tokenAlbumQuery(ownerQuery(db.Model(&database.Album{}), p, all), p, "id", tokenRead), &albums},
		{tokenAlbumQuery(ownerQuery(db.Model(&database.Image{}), p, all), p, "album_id", tokenRead).Where("state = ? AND created_at >= ?", "ready", time.Now().UTC().Truncate(24*time.Hour)), &today},
		{tokenAlbumQuery(ownerQuery(db.Model(&database.Image{}), p, all), p, "album_id", tokenRead).Where("state = ?", "deleting"), &deleting},
	} {
		if err := item.q.Count(item.n).Error; err != nil {
			internal(w, err)
			return
		}
	}
	respond(w, 200, map[string]any{"images": totals.Images, "bytes": totals.Bytes, "albums": albums, "unfiled": 0, "today": today, "pending_deletions": deleting})
}

func (a *App) settings(w http.ResponseWriter, r *http.Request, p principal) {
	a.settingsMu.RLock()
	base := a.publicURL
	profile := a.profiles[a.defaultStorage]
	processing := a.processingSettings
	limit := a.maxUploadBytes
	appURL, secureCookie, pixels := a.appURL, a.secureCookie, a.maxPixels
	proxies := make([]string, 0, len(a.trustedProxies))
	for _, prefix := range a.trustedProxies {
		proxies = append(proxies, prefix.String())
	}
	a.settingsMu.RUnlock()
	respond(w, 200, map[string]any{"public_url": base, "app_url": appURL, "secure_cookie": secureCookie, "trusted_proxies": proxies, "max_upload_bytes": limit, "max_pixels": pixels, "database": a.cfg.DBDriver, "storage": profile.Kind, "default_storage": profile.ID, "storage_name": profile.Name, "processing": processing, "formats": []string{"jpg", "png", "gif", "webp"}, "version": "0.2.0"})
}

func (a *App) updateSettings(w http.ResponseWriter, r *http.Request, p principal) {
	var input struct {
		PublicURL      *string   `json:"public_url"`
		MaxUploadBytes *int64    `json:"max_upload_bytes"`
		AppURL         *string   `json:"app_url"`
		SecureCookie   *bool     `json:"secure_cookie"`
		TrustedProxies *[]string `json:"trusted_proxies"`
		MaxPixels      *int64    `json:"max_pixels"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.PublicURL == nil && input.MaxUploadBytes == nil && input.AppURL == nil && input.SecureCookie == nil && input.TrustedProxies == nil && input.MaxPixels == nil {
		fail(w, 400, "请提供需要更新的设置")
		return
	}
	if input.PublicURL != nil {
		value := strings.TrimRight(strings.TrimSpace(*input.PublicURL), "/")
		if len(value) > 2048 || config.ValidateURL(value) != nil {
			fail(w, 400, "请输入完整的 HTTP(S) 域名，不包含路径和查询参数")
			return
		}
		input.PublicURL = &value
	}
	if input.MaxUploadBytes != nil && !validUploadLimit(*input.MaxUploadBytes) {
		fail(w, 400, "服务器单张上限必须在 1–100 MiB 之间")
		return
	}
	if input.AppURL != nil {
		value := strings.TrimRight(strings.TrimSpace(*input.AppURL), "/")
		if len(value) > 2048 || config.ValidateURL(value) != nil {
			fail(w, 400, "站点地址必须为完整的 HTTP(S) 源地址")
			return
		}
		input.AppURL = &value
	}
	if input.MaxPixels != nil && !validMaxPixels(*input.MaxPixels) {
		fail(w, 400, "像素上限必须在 100 万到 1 亿之间")
		return
	}
	var proxies []netip.Prefix
	if input.TrustedProxies != nil {
		var err error
		proxies, err = parseTrustedProxies(*input.TrustedProxies)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
	}
	if input.SecureCookie != nil && *input.SecureCookie && r.TLS == nil {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		peer, _ := netip.ParseAddr(host)
		trusted := a.trusted(peer.Unmap())
		if input.TrustedProxies != nil {
			trusted = false
			for _, prefix := range proxies {
				if prefix.Contains(peer.Unmap()) {
					trusted = true
					break
				}
			}
		}
		if !trusted || r.Header.Get("X-Forwarded-Proto") != "https" {
			fail(w, 400, "请通过 HTTPS 访问后启用安全 Cookie；反向代理需先配置可信代理并传递 X-Forwarded-Proto: https")
			return
		}
	}
	a.settingsMu.Lock()
	cookieChanged := input.SecureCookie != nil && *input.SecureCookie != a.secureCookie
	// 先完成持久化再更新内存；部分字段保存不会覆盖其他设置。
	err := a.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if input.PublicURL != nil {
			if err := saveSetting(tx, "public_url", *input.PublicURL); err != nil {
				return err
			}
		}
		if input.MaxUploadBytes != nil {
			if err := saveSetting(tx, "max_upload_bytes", strconv.FormatInt(*input.MaxUploadBytes, 10)); err != nil {
				return err
			}
		}
		values := map[string]string{}
		if input.AppURL != nil {
			values["app_url"] = *input.AppURL
		}
		if input.SecureCookie != nil {
			values["secure_cookie"] = strconv.FormatBool(*input.SecureCookie)
		}
		if input.MaxPixels != nil {
			values["max_pixels"] = strconv.FormatInt(*input.MaxPixels, 10)
		}
		if input.TrustedProxies != nil {
			normalized := make([]string, 0, len(proxies))
			for _, prefix := range proxies {
				normalized = append(normalized, prefix.String())
			}
			raw, _ := json.Marshal(normalized)
			values["trusted_proxies"] = string(raw)
		}
		for key, value := range values {
			if err := saveSetting(tx, key, value); err != nil {
				return err
			}
		}
		// Cookie 策略改变后清理旧登录和分享会话，避免旧凭据继续沿用原传输策略。
		if cookieChanged {
			for _, model := range []any{&database.Session{}, &database.ShareSession{}} {
				if err := tx.Where("1 = 1").Delete(model).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		a.settingsMu.Unlock()
		internal(w, err)
		return
	}
	if input.PublicURL != nil {
		a.publicURL = *input.PublicURL
	}
	if input.MaxUploadBytes != nil {
		a.maxUploadBytes = *input.MaxUploadBytes
	}
	if input.AppURL != nil {
		a.appURL = *input.AppURL
	}
	if input.SecureCookie != nil {
		a.secureCookie = *input.SecureCookie
	}
	if input.TrustedProxies != nil {
		a.trustedProxies = proxies
	}
	if input.MaxPixels != nil {
		a.maxPixels = *input.MaxPixels
	}
	a.settingsMu.Unlock()
	if cookieChanged {
		a.setCookie(w, "", -1)
	}
	respond(w, 200, map[string]bool{"ok": true, "reauthenticate": cookieChanged})
}
