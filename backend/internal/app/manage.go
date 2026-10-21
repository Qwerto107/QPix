package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"qpix/internal/config"
	"qpix/internal/database"
)

func (a *App) listAlbums(w http.ResponseWriter, r *http.Request, p principal) {
	var albums []database.Album
	db := a.db.WithContext(r.Context())
	if err := a.visibleAlbums(db, p).Order("created_at DESC").Find(&albums).Error; err != nil {
		internal(w, err)
		return
	}
	var counts []struct {
		AlbumID string
		Total   int64
	}
	if err := a.visibleImages(db.Model(&database.Image{}), p).Select("album_id, COUNT(*) AS total").Where("state = ? AND album_id IS NOT NULL", "ready").Group("album_id").Scan(&counts).Error; err != nil {
		internal(w, err)
		return
	}
	byID := map[string]int64{}
	for _, count := range counts {
		byID[count.AlbumID] = count.Total
	}
	items := make([]map[string]any, 0, len(albums))
	for _, album := range albums {
		members := []string{}
		if p.User.Role == "admin" {
			if err := db.Model(&database.AlbumMember{}).Where("album_id = ?", album.ID).Pluck("user_id", &members).Error; err != nil {
				internal(w, err)
				return
			}
		}
		items = append(items, map[string]any{"id": album.ID, "name": album.Name, "count": byID[album.ID], "created_at": album.CreatedAt, "guest_visible": album.GuestVisible, "member_ids": members, "can_upload": p.User.ID != "" && a.albumWritable(r, p, album.ID)})
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
	album := database.Album{ID: randomID(), UserID: p.User.ID, Name: name}
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
	if err := db.First(&album, "id = ?", r.PathValue("id")).Error; err != nil {
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

func (a *App) deleteAlbum(w http.ResponseWriter, r *http.Request, p principal) {
	id := r.PathValue("id")
	err := a.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		var album database.Album
		if err := tx.First(&album, "id = ?", id).Error; err != nil {
			return err
		}
		// 图片回到上传者的私有未分类空间，避免删除相册意外公开图片。
		if err := tx.Model(&database.Image{}).Where("album_id = ?", id).Update("album_id", nil).Error; err != nil {
			return err
		}
		if err := tx.Where("album_id = ?", id).Delete(&database.AlbumMember{}).Error; err != nil {
			return err
		}
		return tx.Delete(&album).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		fail(w, 404, "相册不存在")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) listTokens(w http.ResponseWriter, r *http.Request, p principal) {
	rows := []database.APIToken{}
	if err := a.db.WithContext(r.Context()).Where("user_id = ?", p.User.ID).Order("created_at DESC").Find(&rows).Error; err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, rows)
}

func (a *App) createToken(w http.ResponseWriter, r *http.Request, p principal) {
	var input struct {
		Name      string     `json:"name"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 64 {
		fail(w, 400, "Token 名称长度必须在 1–64 个字符之间")
		return
	}
	if input.ExpiresAt != nil {
		utc := input.ExpiresAt.UTC()
		if !utc.After(time.Now()) || utc.Year() > 9999 {
			fail(w, 400, "到期时间必须晚于当前时间，且年份不能超过 9999")
			return
		}
		input.ExpiresAt = &utc
	}
	raw := "qpix_" + randomHex(32)
	row := database.APIToken{ID: randomID(), UserID: p.User.ID, Name: input.Name, Hash: digest(raw), Prefix: raw[:13], ExpiresAt: input.ExpiresAt}
	if err := a.db.WithContext(r.Context()).Create(&row).Error; err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, map[string]any{"token": raw, "item": row})
}

func (a *App) revokeToken(w http.ResponseWriter, r *http.Request, p principal) {
	result := a.db.WithContext(r.Context()).Where("id = ? AND user_id = ?", r.PathValue("id"), p.User.ID).Delete(&database.APIToken{})
	if result.Error != nil {
		internal(w, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		fail(w, 404, "Token 不存在")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) stats(w http.ResponseWriter, r *http.Request, p principal) {
	db := a.db.WithContext(r.Context())
	var totals struct {
		Images int64 `json:"images"`
		Bytes  int64 `json:"bytes"`
	}
	if err := a.visibleImages(db.Model(&database.Image{}), p).Select("COUNT(*) AS images, COALESCE(SUM(size), 0) AS bytes").Where("state = ?", "ready").Scan(&totals).Error; err != nil {
		internal(w, err)
		return
	}
	var albums, unfiled, today, deleting int64
	for _, query := range []struct {
		q      *gorm.DB
		target *int64
	}{
		{a.visibleAlbums(db, p), &albums},
		{a.visibleImages(db.Model(&database.Image{}), p).Where("state = ? AND album_id IS NULL", "ready"), &unfiled},
		{a.visibleImages(db.Model(&database.Image{}), p).Where("state = ? AND created_at >= ?", "ready", time.Now().UTC().Truncate(24*time.Hour)), &today},
		{a.manageableImages(db.Model(&database.Image{}), p).Where("state = ?", "deleting"), &deleting},
	} {
		if err := query.q.Count(query.target).Error; err != nil {
			internal(w, err)
			return
		}
	}
	respond(w, 200, map[string]any{"images": totals.Images, "bytes": totals.Bytes, "albums": albums, "unfiled": unfiled, "today": today, "pending_deletions": deleting})
}

func (a *App) updateAlbumAccess(w http.ResponseWriter, r *http.Request, p principal) {
	var input struct {
		GuestVisible bool     `json:"guest_visible"`
		MemberIDs    []string `json:"member_ids"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.MemberIDs) > 200 {
		fail(w, 400, "每个相册最多授权 200 个账号")
		return
	}
	db := a.db.WithContext(r.Context())
	id := r.PathValue("id")
	var album database.Album
	if err := db.First(&album, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, 404, "相册不存在")
		} else {
			internal(w, err)
		}
		return
	}
	seen := map[string]bool{}
	members := []database.AlbumMember{}
	for _, uid := range input.MemberIDs {
		if seen[uid] {
			continue
		}
		seen[uid] = true
		var count int64
		if err := db.Model(&database.User{}).Where("id = ? AND role = ?", uid, "user").Count(&count).Error; err != nil {
			internal(w, err)
			return
		}
		if count != 1 {
			fail(w, 400, "授权账号不存在或不是普通用户")
			return
		}
		members = append(members, database.AlbumMember{AlbumID: id, UserID: uid})
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&album).Update("guest_visible", input.GuestVisible).Error; err != nil {
			return err
		}
		if err := tx.Where("album_id = ?", id).Delete(&database.AlbumMember{}).Error; err != nil {
			return err
		}
		if len(members) > 0 {
			return tx.Create(&members).Error
		}
		return nil
	}); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) settings(w http.ResponseWriter, r *http.Request, p principal) {
	a.settingsMu.RLock()
	base := a.publicURL
	profile := a.profiles[a.defaultStorage]
	processing := a.processingSettings
	limit := a.maxUploadBytes
	a.settingsMu.RUnlock()
	respond(w, 200, map[string]any{"public_url": base, "max_upload_bytes": limit, "max_pixels": a.cfg.MaxPixels, "database": a.cfg.DBDriver, "storage": profile.Kind, "default_storage": profile.ID, "storage_name": profile.Name, "processing": processing, "formats": []string{"jpg", "png", "gif", "webp"}, "version": "0.3.0"})
}

func (a *App) updateSettings(w http.ResponseWriter, r *http.Request, p principal) {
	var input struct {
		PublicURL      *string `json:"public_url"`
		MaxUploadBytes *int64  `json:"max_upload_bytes"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.PublicURL == nil && input.MaxUploadBytes == nil {
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
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	// 先完成持久化再更新内存；部分字段保存不会覆盖其他设置。
	err := a.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if input.PublicURL != nil {
			if err := saveSetting(tx, "public_url", *input.PublicURL); err != nil {
				return err
			}
		}
		if input.MaxUploadBytes != nil {
			return saveSetting(tx, "max_upload_bytes", strconv.FormatInt(*input.MaxUploadBytes, 10))
		}
		return nil
	})
	if err != nil {
		internal(w, err)
		return
	}
	if input.PublicURL != nil {
		a.publicURL = *input.PublicURL
	}
	if input.MaxUploadBytes != nil {
		a.maxUploadBytes = *input.MaxUploadBytes
	}
	respond(w, 200, map[string]bool{"ok": true})
}
