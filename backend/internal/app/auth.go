package app

import (
	"errors"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"qpix/internal/database"
)

// 固定的默认成本哈希仅用于不存在的账号，保证失败登录也执行同等成本的密码校验。
const dummyPasswordHash = "$2a$10$XajjQvNhvvRt5GSeFk1xFeyqRrsxkhBkUiQeg0dt.wU1qD4aFDcga"

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate(w, "login", a.limiterIP(r), "登录尝试过多，请稍后再试") {
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	var user database.User
	db := a.db.WithContext(r.Context())
	err := db.First(&user, "username = ?", input.Username).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		internal(w, err)
		return
	}
	hash := user.PasswordHash
	if err != nil {
		hash = dummyPasswordHash
	}
	validPassword := len(input.Password) <= 72 && bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.Password)) == nil
	if err != nil || !validPassword || !user.Enabled || (user.Role != "admin" && user.Role != "user") {
		fail(w, 401, "用户名或密码错误")
		return
	}
	locked := a.lockPolicy(w)
	defer locked.release()
	w = locked
	// 密码计算期间允许管理操作；创建会话前重新读取，防止密码重置或停用后恢复旧会话。
	if err = db.First(&user, "id = ?", user.ID).Error; err != nil {
		authError(w, err, "用户名或密码错误")
		return
	}
	if user.PasswordHash != hash || !user.Enabled || (user.Role != "admin" && user.Role != "user") {
		fail(w, 401, "用户名或密码错误")
		return
	}
	if !a.sameOrigin(r) {
		fail(w, 403, "请求来源不被允许")
		return
	}
	// 角色来自数据库，验证密码后再应用对应网络规则，通过后才创建会话。
	if user.Role == "admin" && !a.enforceScope(w, r, "admin") {
		return
	}
	raw := randomHex(32)
	session := database.Session{ID: digest(raw), UserID: user.ID, CSRF: randomHex(32), ExpiresAt: time.Now().UTC().Add(7 * 24 * time.Hour)}
	if err := a.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at < ?", time.Now().UTC()).Delete(&database.Session{}).Error; err != nil {
			return err
		}
		// 重新登录替换浏览器已有会话，避免正常登录重复积累记录。
		if cookie, err := r.Cookie("qpix_session"); err == nil {
			if err = tx.Where("id = ?", digest(cookie.Value)).Delete(&database.Session{}).Error; err != nil {
				return err
			}
		}
		return tx.Create(&session).Error
	}); err != nil {
		internal(w, err)
		return
	}
	a.setCookie(w, raw, int((7 * 24 * time.Hour).Seconds()))
	respond(w, 200, map[string]any{"user": user, "csrf_token": session.CSRF})
}

func (a *App) setCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: "qpix_session", Value: value, Path: "/", HttpOnly: true, Secure: a.cookiesSecure(), SameSite: http.SameSiteStrictMode, MaxAge: maxAge})
}
func (a *App) me(w http.ResponseWriter, r *http.Request, p principal) {
	respond(w, 200, map[string]any{"user": p.User, "csrf_token": p.Session.CSRF})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request, p principal) {
	if err := a.db.WithContext(r.Context()).Delete(&database.Session{}, "id = ?", p.Session.ID).Error; err != nil {
		internal(w, err)
		return
	}
	a.setCookie(w, "", -1)
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) changePassword(w http.ResponseWriter, r *http.Request, p principal) {
	var input struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !a.allowRate(w, "password_change", p.User.ID, "尝试过多，请稍后再试") {
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(p.User.PasswordHash), []byte(input.Current)) != nil {
		fail(w, 400, "当前密码不正确")
		return
	}
	if input.New == "" || len(input.New) > 72 {
		fail(w, 400, "新密码不能为空，且不能超过 72 字节")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.New), bcrypt.DefaultCost)
	if err != nil {
		internal(w, err)
		return
	}
	locked, fresh, ok := a.lockWrite(w, r, false, "write")
	if !ok {
		return
	}
	defer locked.release()
	w = locked
	if fresh.User.ID != p.User.ID || fresh.User.PasswordHash != p.User.PasswordHash {
		fail(w, 409, "密码已变更，请重新登录后重试")
		return
	}
	p = fresh
	err = a.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&database.User{}).Where("id = ?", p.User.ID).Update("password_hash", string(hash)).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", p.User.ID).Delete(&database.Session{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		internal(w, err)
		return
	}
	a.setCookie(w, "", -1)
	respond(w, 200, map[string]bool{"ok": true})
}
