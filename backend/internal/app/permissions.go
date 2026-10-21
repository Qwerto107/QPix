package app

import (
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"
	"qpix/internal/database"
)

func (a *App) memberAlbums(db *gorm.DB, userID string) *gorm.DB {
	return db.Model(&database.AlbumMember{}).Select("album_id").Where("user_id = ?", userID)
}

func (a *App) visibleAlbums(db *gorm.DB, p principal) *gorm.DB {
	query := db.Model(&database.Album{})
	if p.User.Role == "admin" {
		return query
	}
	if p.User.ID == "" {
		return query.Where("guest_visible = ?", true)
	}
	return query.Where("guest_visible = ? OR id IN (?)", true, a.memberAlbums(db, p.User.ID))
}

func (a *App) visibleImages(db *gorm.DB, p principal) *gorm.DB {
	if p.User.Role == "admin" {
		return db
	}
	albums := a.visibleAlbums(a.db.WithContext(db.Statement.Context), p).Select("id")
	if p.User.ID == "" {
		return db.Where("album_id IN (?)", albums)
	}
	return db.Where("(album_id IN (?) OR (album_id IS NULL AND user_id = ?))", albums, p.User.ID)
}

func (a *App) manageableImages(db *gorm.DB, p principal) *gorm.DB {
	if p.User.Role == "admin" {
		return db
	}
	return db.Where("user_id = ? AND (album_id IS NULL OR album_id IN (?))", p.User.ID, a.memberAlbums(a.db.WithContext(db.Statement.Context), p.User.ID))
}

func (a *App) albumWritable(r *http.Request, p principal, id string) bool {
	if id == "" {
		return p.User.ID != ""
	}
	db := a.db.WithContext(r.Context())
	query := db.Model(&database.Album{}).Where("id = ?", id)
	if p.User.Role != "admin" {
		query = query.Where("id IN (?)", a.memberAlbums(db, p.User.ID))
	}
	var count int64
	return query.Count(&count).Error == nil && count == 1
}

func (a *App) viewFor(row database.Image, p principal) imageView {
	view := a.imageView(row)
	var count int64
	view.CanManage = a.manageableImages(a.db.Model(&database.Image{}), p).Where("id = ?", row.ID).Count(&count).Error == nil && count == 1
	if p.User.Role == "admin" || p.User.ID == row.UserID {
		view.OwnerID = row.UserID
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
