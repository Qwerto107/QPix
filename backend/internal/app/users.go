package app

import (
	"errors"
	"net/http"
	"regexp"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"qpix/internal/database"
)

type userInput struct {
	Username        string `json:"username"`
	Enabled         bool   `json:"enabled"`
	QuotaBytes      int64  `json:"quota_bytes"`
	MaxFileBytes    int64  `json:"max_file_bytes"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	Role            string `json:"role"`
}

type userView struct {
	database.User
	UsedBytes int64 `json:"used_bytes"`
}

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

func (a *App) usedBytes(db *gorm.DB, userID string) (int64, error) {
	var used int64
	err := db.Model(&database.Image{}).Select("COALESCE(SUM(size), 0)").Where("user_id = ?", userID).Scan(&used).Error
	return used, err
}

func (a *App) listUsers(w http.ResponseWriter, r *http.Request, p principal) {
	var rows []database.User
	if err := a.db.WithContext(r.Context()).Order("created_at ASC").Find(&rows).Error; err != nil {
		internal(w, err)
		return
	}
	items := make([]userView, 0, len(rows))
	for _, row := range rows {
		used, err := a.usedBytes(a.db.WithContext(r.Context()), row.ID)
		if err != nil {
			internal(w, err)
			return
		}
		items = append(items, userView{User: row, UsedBytes: used})
	}
	respond(w, 200, items)
}

func (a *App) saveUser(w http.ResponseWriter, r *http.Request, p principal) {
	var input userInput
	if !decode(w, r, &input) {
		return
	}
	if input.Role != "" && input.Role != "user" && input.Role != "admin" {
		fail(w, 400, "角色必须为普通用户或管理员")
		return
	}
	if input.QuotaBytes < 0 || input.QuotaBytes > 1<<50 || input.MaxFileBytes < 0 || input.MaxFileBytes > a.uploadLimit() {
		fail(w, 400, "容量配额无效，单张上限不能超过服务器限制；0 表示不限容量或跟随服务器单张限制")
		return
	}
	id := r.PathValue("id")
	creating := id == ""
	if creating && !usernamePattern.MatchString(input.Username) {
		fail(w, 400, "用户名为 1–64 位字母、数字、下划线、点或短横线，首位为字母或数字")
		return
	}
	if creating {
		// 写权限锁已串行化账号创建；显式查询避免不同数据库驱动的重复键错误翻译差异。
		var count int64
		if err := a.db.WithContext(r.Context()).Model(&database.User{}).Where("username = ?", input.Username).Count(&count).Error; err != nil {
			internal(w, err)
			return
		}
		if count > 0 {
			fail(w, 409, "用户名已存在")
			return
		}
	}
	if creating && input.Password == "" || len(input.Password) > 72 || input.Password != input.ConfirmPassword {
		fail(w, 400, "密码不能为空，最多 72 字节，且两次输入须一致；编辑时均留空可保留原密码")
		return
	}
	var hash []byte
	var err error
	if input.Password != "" {
		hash, err = bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			internal(w, err)
			return
		}
	}
	db := a.db.WithContext(r.Context())
	row := database.User{ID: id}
	if creating {
		row = database.User{ID: randomID(), Username: input.Username, PasswordHash: string(hash), Role: "user", Enabled: true, QuotaBytes: input.QuotaBytes, MaxFileBytes: input.MaxFileBytes}
	} else {
		if err = db.First(&row, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				fail(w, 404, "账号不存在")
			} else {
				internal(w, err)
			}
			return
		}
		if row.Role == "admin" && input.Password != "" {
			fail(w, 400, "管理员请在本人账号设置修改密码")
			return
		}
	}
	role := row.Role
	if input.Role != "" {
		role = input.Role
	}
	if !creating && row.ID == p.User.ID && (role != row.Role || !input.Enabled) {
		fail(w, 409, "不能更改自己的角色或停用当前账号")
		return
	}
	if row.Role == "admin" && row.Enabled && (role != "admin" || !input.Enabled) {
		if !a.hasOtherAdmin(w, db, row.ID) {
			return
		}
	}
	roleChanged := role != row.Role
	err = db.Transaction(func(tx *gorm.DB) error {
		if creating {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		changes := map[string]any{"role": role, "enabled": input.Enabled, "quota_bytes": input.QuotaBytes, "max_file_bytes": input.MaxFileBytes}
		if len(hash) > 0 {
			changes["password_hash"] = string(hash)
		}
		if err := tx.Model(&row).Updates(changes).Error; err != nil {
			return err
		}
		if creating {
			if err := ensureDefaultAlbum(tx, &row); err != nil {
				return err
			}
		}
		if !input.Enabled || len(hash) > 0 || roleChanged {
			if err := tx.Where("user_id = ?", row.ID).Delete(&database.Session{}).Error; err != nil {
				return err
			}
			if !input.Enabled {
				return tx.Model(&database.APIToken{}).Where("user_id = ? AND revoked_at IS NULL", row.ID).Update("revoked_at", time.Now().UTC()).Error
			}
			return nil
		}
		return nil
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		fail(w, 409, "用户名已存在")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]string{"id": row.ID})
}

func (a *App) hasOtherAdmin(w http.ResponseWriter, db *gorm.DB, id string) bool {
	var count int64
	if err := db.Model(&database.User{}).Where("role = ? AND enabled = ? AND id <> ?", "admin", true, id).Count(&count).Error; err != nil {
		internal(w, err)
		return false
	}
	if count == 0 {
		fail(w, 409, "必须保留至少一个启用的管理员")
		return false
	}
	return true
}

func (a *App) deleteUser(w http.ResponseWriter, r *http.Request, p principal) {
	id := r.PathValue("id")
	if id == p.User.ID {
		fail(w, 409, "不能删除当前登录账号")
		return
	}
	db := a.db.WithContext(r.Context())
	var row database.User
	if err := db.First(&row, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, 404, "账号不存在")
		} else {
			internal(w, err)
		}
		return
	}
	if row.Role == "admin" && row.Enabled && !a.hasOtherAdmin(w, db, id) {
		return
	}
	owned := db.Model(&database.Album{}).Select("id").Where("user_id = ?", id)
	var images, shares int64
	if err := db.Model(&database.Image{}).Where("user_id = ? OR album_id IN (?)", id, owned).Count(&images).Error; err != nil {
		internal(w, err)
		return
	}
	if err := db.Model(&database.Share{}).Where("user_id = ?", id).Count(&shares).Error; err != nil {
		internal(w, err)
		return
	}
	if images > 0 || shares > 0 {
		fail(w, 409, "该账号仍有图片或分享记录，请先手动清理后再删除")
		return
	}
	// 仅清理空账号及其附属凭据、空相册；含待清理图片的账号也不能提前删除。
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, model := range []any{&database.Session{}, &database.APIToken{}, &database.UploadReservation{}} {
			if err := tx.Where("user_id = ?", id).Delete(model).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("user_id = ? OR album_id IN (?)", id, tx.Model(&database.Album{}).Select("id").Where("user_id = ?", id)).Delete(&database.AlbumMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&database.Album{}).Error; err != nil {
			return err
		}
		return tx.Delete(&row).Error
	})
	if err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) reserveUpload(r *http.Request, userID string, size int64) (string, error) {
	a.quotaMu.Lock()
	defer a.quotaMu.Unlock()
	id := randomID()
	err := a.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		var user database.User
		if err := tx.First(&user, "id = ? AND enabled = ?", userID, true).Error; err != nil {
			return err
		}
		if user.MaxFileBytes > 0 && size > user.MaxFileBytes {
			return errFileLimit
		}
		used, err := a.usedBytes(tx, userID)
		if err != nil {
			return err
		}
		var reserved int64
		if err = tx.Model(&database.UploadReservation{}).Select("COALESCE(SUM(bytes),0)").Where("user_id = ?", userID).Scan(&reserved).Error; err != nil {
			return err
		}
		if user.QuotaBytes > 0 && size > user.QuotaBytes-used-reserved {
			return errQuota
		}
		return tx.Create(&database.UploadReservation{ID: id, UserID: userID, Bytes: size}).Error
	})
	return id, err
}

var errQuota = errors.New("可用容量不足，请清理图片或联系管理员调整配额")
var errFileLimit = errors.New("图片超过此账号的单张大小限制")

func (a *App) uploadOptions(w http.ResponseWriter, r *http.Request, p principal) {
	used, err := a.usedBytes(a.db.WithContext(r.Context()), p.User.ID)
	if err != nil {
		internal(w, err)
		return
	}
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	stores := []map[string]string{}
	for _, profile := range a.profiles {
		if profile.Enabled {
			stores = append(stores, map[string]string{"id": profile.ID, "name": profile.Name})
		}
	}
	limit := a.maxUploadBytes
	if p.User.MaxFileBytes > 0 {
		limit = min(limit, p.User.MaxFileBytes)
	}
	respond(w, 200, map[string]any{"max_upload_bytes": limit, "max_pixels": a.maxPixels, "default_storage": a.defaultStorage, "processing": a.processingSettings, "storages": stores, "used_bytes": used, "quota_bytes": p.User.QuotaBytes})
}
