package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"qpix/internal/database"
	"qpix/internal/secret"
)

type shareView struct {
	database.Share
	URL         string `json:"url"`
	HasPassword bool   `json:"has_password"`
	Title       string `json:"title"`
}

func (a *App) shareTitle(db *gorm.DB, row database.Share) (string, error) {
	if row.Kind == "album" {
		var album database.Album
		err := db.First(&album, "id = ?", row.TargetID).Error
		return album.Name, err
	}
	var image database.Image
	err := db.First(&image, "id = ? AND state = ?", row.TargetID, "ready").Error
	return image.Name, err
}

func (a *App) shareView(row database.Share) (shareView, error) {
	token, err := secret.Open(a.masterKey, row.TokenCipher, "share:"+row.ID)
	if err != nil {
		return shareView{}, err
	}
	title, err := a.shareTitle(a.db, row)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		title = "目标已删除"
		err = nil
	}
	return shareView{Share: row, URL: "/s/" + token, HasPassword: row.PasswordHash != "", Title: title}, err
}

func (a *App) listShares(w http.ResponseWriter, r *http.Request, p principal) {
	var rows []database.Share
	if err := a.db.WithContext(r.Context()).Order("created_at DESC").Find(&rows).Error; err != nil {
		internal(w, err)
		return
	}
	items := make([]shareView, 0, len(rows))
	for _, row := range rows {
		item, err := a.shareView(row)
		if err != nil {
			internal(w, err)
			return
		}
		items = append(items, item)
	}
	respond(w, 200, items)
}

func (a *App) createShare(w http.ResponseWriter, r *http.Request, p principal) {
	var input struct {
		Kind      string     `json:"kind"`
		TargetID  string     `json:"target_id"`
		Password  string     `json:"password"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Kind != "image" && input.Kind != "album" || input.TargetID == "" {
		fail(w, 400, "请选择图片或相册")
		return
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		fail(w, 400, "到期时间必须晚于当前时间")
		return
	}
	if input.ExpiresAt != nil {
		utc := input.ExpiresAt.UTC()
		input.ExpiresAt = &utc
	}
	if len(input.Password) > 72 {
		fail(w, 400, "分享密码不能超过 72 字节")
		return
	}
	token := randomHex(32)
	row := database.Share{ID: randomID(), Kind: input.Kind, TargetID: input.TargetID, TokenHash: digest(token), ExpiresAt: input.ExpiresAt}
	if _, err := a.shareTitle(a.db.WithContext(r.Context()), row); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, 404, "分享目标不存在")
		} else {
			internal(w, err)
		}
		return
	}
	var err error
	row.TokenCipher, err = secret.Seal(a.masterKey, token, "share:"+row.ID)
	if err != nil {
		internal(w, err)
		return
	}
	if input.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			internal(w, err)
			return
		}
		row.PasswordHash = string(hash)
	}
	if err = a.db.WithContext(r.Context()).Create(&row).Error; err != nil {
		internal(w, err)
		return
	}
	view, err := a.shareView(row)
	if err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, view)
}

func (a *App) revokeShare(w http.ResponseWriter, r *http.Request, p principal) {
	id := r.PathValue("id")
	result := a.db.WithContext(r.Context()).Model(&database.Share{}).Where("id = ?", id).Update("revoked_at", time.Now().UTC())
	if result.Error != nil {
		internal(w, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		fail(w, 404, "分享不存在")
		return
	}
	if err := a.db.WithContext(r.Context()).Where("share_id = ?", id).Delete(&database.ShareSession{}).Error; err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) findShare(r *http.Request) (database.Share, error) {
	var row database.Share
	token := r.PathValue("token")
	if len(token) != 64 {
		return row, gorm.ErrRecordNotFound
	}
	err := a.db.WithContext(r.Context()).First(&row, "token_hash = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", digest(token), time.Now().UTC()).Error
	if err != nil {
		return row, err
	}
	_, err = a.shareTitle(a.db.WithContext(r.Context()), row)
	return row, err
}

func shareCookie(id string) string { return "qpix_share_" + id }

func (a *App) shareUnlocked(r *http.Request, row database.Share) (bool, error) {
	if row.PasswordHash == "" {
		return true, nil
	}
	cookie, err := r.Cookie(shareCookie(row.ID))
	if err != nil {
		return false, nil
	}
	var count int64
	err = a.db.WithContext(r.Context()).Model(&database.ShareSession{}).Where("id = ? AND share_id = ? AND expires_at > ?", digest(cookie.Value), row.ID, time.Now().UTC()).Count(&count).Error
	return count == 1, err
}

func (a *App) publicShare(w http.ResponseWriter, r *http.Request) {
	a.policyMu.RLock()
	defer a.policyMu.RUnlock()
	row, err := a.findShare(r)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, 404, "分享不存在、已撤销或已到期")
		} else {
			internal(w, err)
		}
		return
	}
	unlocked, err := a.shareUnlocked(r, row)
	if err != nil {
		internal(w, err)
		return
	}
	if !unlocked {
		respond(w, 200, map[string]any{"locked": true, "expires_at": row.ExpiresAt})
		return
	}
	title, err := a.shareTitle(a.db.WithContext(r.Context()), row)
	if err != nil {
		internal(w, err)
		return
	}
	q := a.shareImages(a.db.WithContext(r.Context()).Model(&database.Image{}), row).Where("state = ?", "ready")
	var total int64
	if err = q.Count(&total).Error; err != nil {
		internal(w, err)
		return
	}
	page, limit := imagePage(r)
	var images []database.Image
	if err = q.Order("created_at DESC").Order("id DESC").Offset((page - 1) * limit).Limit(limit).Find(&images).Error; err != nil {
		internal(w, err)
		return
	}
	items := make([]imageView, 0, len(images))
	for _, image := range images {
		view := a.imageView(image)
		// 分享中的文件必须走分享网关，不能向访客暴露绕过口令的原始链接。
		view.URL = "/si/" + r.PathValue("token") + "/" + image.ID + "." + image.Format
		view.ThumbnailURL = view.URL
		if image.ThumbKey != "" {
			view.ThumbnailURL = "/si/" + r.PathValue("token") + "/" + image.ID + "-thumb.png"
		}
		items = append(items, view)
	}
	respond(w, 200, map[string]any{"locked": false, "title": title, "expires_at": row.ExpiresAt, "items": items, "total": total, "page": page, "limit": limit})
}

func (a *App) unlockShare(w http.ResponseWriter, r *http.Request) {
	if !a.loginLimits.allow("share:"+a.requesterIP(r).String(), 15, 15*time.Minute) {
		w.Header().Set("Retry-After", "900")
		fail(w, 429, "尝试过于频繁，请稍后重试")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	a.policyMu.RLock()
	defer a.policyMu.RUnlock()
	row, err := a.findShare(r)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, 404, "分享不存在、已撤销或已到期")
		} else {
			internal(w, err)
		}
		return
	}
	if row.PasswordHash != "" && (len(input.Password) > 72 || bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(input.Password)) != nil) {
		fail(w, 403, "分享密码错误")
		return
	}
	if row.PasswordHash != "" {
		raw := randomHex(32)
		expires := time.Now().UTC().Add(24 * time.Hour)
		if row.ExpiresAt != nil && row.ExpiresAt.Before(expires) {
			expires = *row.ExpiresAt
		}
		err = a.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
			if cookie, err := r.Cookie(shareCookie(row.ID)); err == nil {
				if err = tx.Where("id = ?", digest(cookie.Value)).Delete(&database.ShareSession{}).Error; err != nil {
					return err
				}
			}
			return tx.Create(&database.ShareSession{ID: digest(raw), ShareID: row.ID, ExpiresAt: expires}).Error
		})
		if err != nil {
			internal(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: shareCookie(row.ID), Value: raw, Path: "/", HttpOnly: true, Secure: a.cfg.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: max(1, int(time.Until(expires).Seconds()))})
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) shareImages(db *gorm.DB, row database.Share) *gorm.DB {
	if row.Kind == "album" {
		return db.Where("album_id = ?", row.TargetID)
	}
	return db.Where("id = ?", row.TargetID)
}

func (a *App) serveShareImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	a.policyMu.RLock()
	defer a.policyMu.RUnlock()
	row, err := a.findShare(r)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
		} else {
			internal(w, err)
		}
		return
	}
	ok, err := a.shareUnlocked(r, row)
	if err != nil {
		internal(w, err)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	a.serveStoredImage(w, r, func(db *gorm.DB) *gorm.DB { return a.shareImages(db, row) })
}

// 分享分页复用图库上限，不允许请求一次读取整个相册。
func imagePage(r *http.Request) (int, int) {
	page, limit := 1, 36
	if value := strings.TrimSpace(r.URL.Query().Get("page")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 1_000_000 {
			page = parsed
		}
	}
	return page, limit
}
