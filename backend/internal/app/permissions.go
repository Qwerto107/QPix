package app

import (
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"
	"qpix/internal/database"
)

// 列表默认只展示本人资源；全站范围必须显式选择，并再次校验管理员身份。
func allScope(w http.ResponseWriter, r *http.Request, p principal) (bool, bool) {
	all := r.URL.Query().Get("scope") == "all"
	if all && (p.User.Role != "admin" || p.Token != nil) {
		fail(w, 403, "仅管理员可访问全站资源")
		return false, false
	}
	return all, true
}
func ownerQuery(db *gorm.DB, p principal, all bool) *gorm.DB {
	if !all {
		return db.Where("user_id = ?", p.User.ID)
	}
	return db
}
func (a *App) visibleAlbums(db *gorm.DB, p principal) *gorm.DB {
	q := db.Model(&database.Album{})
	// 管理员的全站权限仅适用于登录会话，个人 Token 始终受本人相册授权约束。
	if p.User.Role == "admin" && p.Token == nil {
		return q
	}
	return tokenAlbumQuery(q.Where("user_id = ?", p.User.ID), p, "id", p.TokenOperation)
}
func imageReadable(row database.Image, p principal) bool {
	if p.User.Role == "admin" && p.Token == nil {
		return true
	}
	if p.Token != nil {
		return row.UserID == p.User.ID && row.AlbumRef != nil && row.AlbumRef.UserID == p.User.ID && p.allowsToken(row.AlbumID, tokenRead)
	}
	if p.User.ID != "" && row.UserID == p.User.ID {
		return true
	}
	return row.AlbumRef != nil && row.AlbumRef.GuestVisible
}
func (a *App) manageableImages(db *gorm.DB, p principal) *gorm.DB {
	if p.User.Role == "admin" && p.Token == nil {
		return db
	}
	return tokenAlbumQuery(db.Where("user_id = ?", p.User.ID), p, "album_id", p.TokenOperation)
}
func (a *App) albumWritable(r *http.Request, p principal, id string) bool {
	q := a.db.WithContext(r.Context()).Model(&database.Album{}).Where("id = ?", id)
	q = q.Where("user_id = ?", p.User.ID)
	var n int64
	return p.allowsToken(&id, tokenUpload) && q.Count(&n).Error == nil && n == 1
}

// 列表已按资源归属和 Token 相册权限筛选，此处只组装响应，不再逐张查询权限和上传人。
func (a *App) viewFor(row database.Image, p principal, owner database.User) imageView {
	view := a.imageView(row)
	view.CanManage = p.User.Role == "admin" || p.User.ID == row.UserID
	if p.Token != nil {
		view.CanManage = p.User.ID == row.UserID && (p.allowsToken(row.AlbumID, tokenEdit) || p.allowsToken(row.AlbumID, tokenDelete))
	}
	if p.User.Role == "admin" || p.User.ID == row.UserID {
		view.OwnerID = row.UserID
		view.OwnerName = owner.Username
		view.OwnerRole = owner.Role
	}
	return view
}

func usersByID(db *gorm.DB, ids []string) (map[string]database.User, error) {
	owners := make(map[string]database.User)
	unique := make([]string, 0, len(ids))
	seen := make(map[string]bool)
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return owners, nil
	}
	var rows []database.User
	if err := db.Select("id", "username", "role", "default_album_id").Where("id IN ?", unique).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		owners[row.ID] = row
	}
	return owners, nil
}

func (a *App) sessionPrincipal(r *http.Request) (principal, error) {
	var p principal
	cookie, err := r.Cookie("qpix_session")
	if err != nil {
		return p, nil
	}
	var session database.Session
	db := a.db.WithContext(r.Context())
	if err = db.First(&session, "id = ? AND expires_at > ?", digest(cookie.Value), time.Now().UTC()).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return p, nil
	} else if err != nil {
		return p, err
	}
	if err = db.First(&p.User, "id = ? AND enabled = ?", session.UserID, true).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return principal{}, nil
	} else if err != nil {
		return p, err
	}
	p.Session = &session
	return p, nil
}
