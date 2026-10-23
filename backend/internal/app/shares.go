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
	OwnerName   string `json:"owner_name"`
	OwnerRole   string `json:"owner_role"`
}

func (a *App) shareTitle(db *gorm.DB, row database.Share) (string, error) {
	var image database.Image
	err := db.First(&image, "id = ? AND state = ?", row.TargetID, "ready").Error
	return image.Name, err
}

func (a *App) shareView(row database.Share, owner database.User, title string) (shareView, error) {
	token, err := secret.Open(a.masterKey, row.TokenCipher, "share:"+row.ID)
	if err != nil {
		return shareView{}, err
	}
	return shareView{Share: row, URL: "/s/" + token, HasPassword: row.PasswordHash != "", Title: title, OwnerName: owner.Username, OwnerRole: owner.Role}, nil
}

func (a *App) listShares(w http.ResponseWriter, r *http.Request, p principal) {
	all, ok := allScope(w, r, p)
	if !ok {
		return
	}
	db := a.db.WithContext(r.Context())
	q := ownerQuery(db.Model(&database.Share{}), p, all).Where("kind = ?", "image")
	if p.Token != nil {
		images := tokenAlbumQuery(db.Model(&database.Image{}).Where("user_id = ?", p.User.ID), p, "album_id", tokenShare).Select("id")
		q = q.Where("target_id IN (?)", images)
	}
	if owner := r.URL.Query().Get("user_id"); owner != "" {
		q = q.Where("user_id = ?", owner)
	}
	if search := strings.TrimSpace(r.URL.Query().Get("q")); search != "" {
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(search))
		q = q.Where("target_id IN (?)", db.Model(&database.Image{}).Select("id").Where("LOWER(name) LIKE ? ESCAPE '!'", "%"+escaped+"%"))
	}
	switch r.URL.Query().Get("status") {
	case "active":
		q = q.Where("revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", time.Now().UTC())
	case "expired":
		q = q.Where("revoked_at IS NULL AND expires_at <= ?", time.Now().UTC())
	case "revoked":
		q = q.Where("revoked_at IS NOT NULL")
	}
	_, limit := listPage(r)
	order := "created_at DESC"
	if r.URL.Query().Get("order") == "asc" {
		order = "created_at ASC"
	}
	rows, pagination, err := paginate[database.Share](q, r, limit, order+", id DESC")
	if err != nil {
		paginationError(w, err)
		return
	}
	items := make([]shareView, 0, len(rows))
	ownerIDs := make([]string, 0, len(rows))
	targetIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		ownerIDs = append(ownerIDs, row.UserID)
		targetIDs = append(targetIDs, row.TargetID)
	}
	owners, err := usersByID(db, ownerIDs)
	if err != nil {
		internal(w, err)
		return
	}
	var images []database.Image
	if len(targetIDs) > 0 {
		if err := db.Select("id", "name").Where("id IN ? AND state = ?", targetIDs, "ready").Find(&images).Error; err != nil {
			internal(w, err)
			return
		}
	}
	titles := make(map[string]string, len(images))
	for _, image := range images {
		titles[image.ID] = image.Name
	}
	for _, row := range rows {
		owner, exists := owners[row.UserID]
		if !exists {
			internal(w, gorm.ErrRecordNotFound)
			return
		}
		title, exists := titles[row.TargetID]
		if !exists {
			title = "目标已删除"
		}
		item, err := a.shareView(row, owner, title)
		if err != nil {
			internal(w, err)
			return
		}
		items = append(items, item)
	}
	respond(w, 200, pagination.response(items))
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
	if !a.shareCreateLimits.allow(p.User.ID, 30, time.Minute) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "创建分享过于频繁，请稍后重试")
		return
	}
	if input.Kind != "image" || input.TargetID == "" {
		fail(w, 400, "请选择单张图片")
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
	row := database.Share{ID: randomID(), UserID: p.User.ID, Kind: input.Kind, TargetID: input.TargetID, TokenHash: digest(token), ExpiresAt: input.ExpiresAt}
	var target database.Image
	if err := a.manageableImages(a.db.WithContext(r.Context()), p).First(&target, "id = ? AND state = ?", input.TargetID, "ready").Error; err != nil {
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
	locked, fresh, ok := a.lockWrite(w, r, true, "write")
	if !ok {
		return
	}
	defer locked.release()
	w, p = locked, fresh
	if p.User.ID != row.UserID {
		fail(w, 401, "登录身份已变化，请重新登录后重试")
		return
	}
	// 密码计算期间目标可能被删除、移动，Token 授权也可能变化，不能沿用旧查询结果。
	if err := a.manageableImages(a.db.WithContext(r.Context()), p).First(&target, "id = ? AND state = ?", input.TargetID, "ready").Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, 404, "分享目标不存在")
		} else {
			internal(w, err)
		}
		return
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		fail(w, 400, "到期时间必须晚于当前时间")
		return
	}
	if err = a.db.WithContext(r.Context()).Create(&row).Error; err != nil {
		internal(w, err)
		return
	}
	view, err := a.shareView(row, p.User, target.Name)
	if err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, view)
}

func (a *App) manageableShares(db *gorm.DB, p principal) *gorm.DB {
	q := db.Model(&database.Share{})
	if p.User.Role != "admin" || p.Token != nil {
		q = q.Where("user_id = ?", p.User.ID)
	}
	if p.Token != nil {
		images := tokenAlbumQuery(db.Session(&gorm.Session{NewDB: true}).Model(&database.Image{}).Where("user_id = ?", p.User.ID), p, "album_id", tokenShare).Select("id")
		q = q.Where("target_id IN (?)", images)
	}
	return q
}

func (a *App) revokeShare(w http.ResponseWriter, r *http.Request, p principal) {
	id := r.PathValue("id")
	q := a.manageableShares(a.db.WithContext(r.Context()), p).Where("id = ?", id)
	result := q.Update("revoked_at", time.Now().UTC())
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

func (a *App) deleteShareRecord(w http.ResponseWriter, r *http.Request, p principal) {
	db := a.db.WithContext(r.Context())
	var row database.Share
	if err := a.manageableShares(db, p).First(&row, "id = ?", r.PathValue("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, 404, "分享不存在")
		} else {
			internal(w, err)
		}
		return
	}
	if row.RevokedAt == nil {
		fail(w, 409, "请先撤销分享，再删除记录")
		return
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("share_id = ?", row.ID).Delete(&database.ShareSession{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND revoked_at IS NOT NULL", row.ID).Delete(&database.Share{}).Error
	})
	if err != nil {
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
	err := a.db.WithContext(r.Context()).First(&row, "token_hash = ? AND kind = 'image' AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", digest(token), time.Now().UTC()).Error
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
	row, err := a.findShare(r)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, 404, "分享不存在、已撤销或已到期")
		} else {
			internal(w, err)
		}
		return
	}
	var target database.Image
	if err := a.db.WithContext(r.Context()).First(&target, "id = ? AND state = ?", row.TargetID, "ready").Error; err != nil {
		http.NotFound(w, r)
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
		view.ThumbnailURL = "/si/" + r.PathValue("token") + "/" + thumbnailFile(image)
		items = append(items, view)
	}
	respond(w, 200, map[string]any{"locked": false, "title": title, "expires_at": row.ExpiresAt, "items": items, "total": total, "page": page, "limit": limit})
}

func (a *App) unlockShare(w http.ResponseWriter, r *http.Request) {
	if !a.shareLimits.allow(a.limiterIP(r), 15, 15*time.Minute) {
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
	row, err := a.findShare(r)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, 404, "分享不存在、已撤销或已到期")
		} else {
			internal(w, err)
		}
		return
	}
	var target database.Image
	if err := a.db.WithContext(r.Context()).First(&target, "id = ? AND state = ?", row.TargetID, "ready").Error; err != nil {
		http.NotFound(w, r)
		return
	}
	if row.PasswordHash != "" && (len(input.Password) > 72 || bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(input.Password)) != nil) {
		fail(w, 403, "分享密码错误")
		return
	}
	locked := a.lockPolicy(w)
	defer locked.release()
	w = locked
	// 分享可能在密码校验期间到期、撤销或删除，写入解锁会话前再次确认。
	if _, err := a.findShare(r); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.NotFound(w, r)
		} else {
			internal(w, err)
		}
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
		http.SetCookie(w, &http.Cookie{Name: shareCookie(row.ID), Value: raw, Path: "/", HttpOnly: true, Secure: a.cookiesSecure(), SameSite: http.SameSiteStrictMode, MaxAge: max(1, int(time.Until(expires).Seconds()))})
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) shareImages(db *gorm.DB, row database.Share) *gorm.DB {
	return db.Where("id = ?", row.TargetID)
}

func (a *App) serveShareImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
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
	a.serveStoredImage(w, r, func(image database.Image) bool { return image.ID == row.TargetID })
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
