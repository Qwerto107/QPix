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
func (a *App) visibleImages(db *gorm.DB, p principal) *gorm.DB {
	if p.User.Role == "admin" && p.Token == nil {
		return db
	}
	if p.Token != nil {
		return tokenAlbumQuery(db.Where("user_id = ?", p.User.ID), p, "album_id", tokenRead)
	}
	public := a.db.WithContext(db.Statement.Context).Model(&database.Album{}).Select("id").Where("guest_visible = ?", true)
	if p.User.ID == "" {
		return db.Where("album_id IN (?)", public)
	}
	return db.Where("(user_id = ? OR album_id IN (?))", p.User.ID, public)
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

func (a *App) viewFor(row database.Image, p principal) imageView {
	view := a.imageView(row)
	var count int64
	view.CanManage = a.manageableImages(a.db.Model(&database.Image{}), p).Where("id = ?", row.ID).Count(&count).Error == nil && count == 1
	if p.Token != nil {
		view.CanManage = p.allowsToken(row.AlbumID, tokenEdit) || p.allowsToken(row.AlbumID, tokenDelete)
	}
	if p.User.Role == "admin" || p.User.ID == row.UserID {
		view.OwnerID = row.UserID
		var owner database.User
		if a.db.Select("username", "role").First(&owner, "id = ?", row.UserID).Error == nil {
			view.OwnerName = owner.Username
			view.OwnerRole = owner.Role
		}
	}
	return view
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
