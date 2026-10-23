package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"qpix/internal/database"
	"qpix/internal/secret"
	"qpix/internal/storage"
)

type storageInput struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Enabled   bool   `json:"enabled"`
	Root      string `json:"root"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	PathStyle bool   `json:"path_style"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}
type storageCredentials struct {
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}
type profileView struct {
	database.StorageProfile
	Default        bool  `json:"is_default"`
	HasCredentials bool  `json:"has_credentials"`
	Images         int64 `json:"images"`
}

func (a *App) buildStore(profile database.StorageProfile) (storage.Store, error) {
	if profile.Kind == "local" {
		return storage.NewLocal(profile.Root)
	}
	if profile.Kind != "s3" {
		return nil, errors.New("不支持的存储类型")
	}
	plain, err := secret.Open(a.masterKey, profile.Credentials, profile.ID)
	if err != nil {
		return nil, err
	}
	var credentials storageCredentials
	if err = json.Unmarshal([]byte(plain), &credentials); err != nil {
		return nil, errors.New("存储凭据格式无效")
	}
	return storage.NewS3(storage.S3Config{Endpoint: profile.Endpoint, Region: profile.Region, Bucket: profile.Bucket, Prefix: profile.Prefix, PathStyle: profile.PathStyle, AccessKey: credentials.AccessKey, SecretKey: credentials.SecretKey}), nil
}

func (a *App) initProfiles() error {
	root, err := filepath.Abs(a.cfg.StorageRoot)
	if err != nil {
		return err
	}
	legacy := database.StorageProfile{ID: "local", Name: "本地存储", Kind: "local", Enabled: true, Root: root}
	if err = a.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&legacy).Error; err != nil {
		return err
	}
	var rows []database.StorageProfile
	if err = a.db.Find(&rows).Error; err != nil {
		return err
	}
	a.profiles = make(map[string]database.StorageProfile, len(rows))
	for _, profile := range rows {
		driver, exists := a.stores[profile.ID]
		if !exists || profile.ID != "local" || profile.Root != root {
			driver, err = a.buildStore(profile)
			if err != nil {
				return fmt.Errorf("存储配置 %s 加载失败：%w", profile.ID, err)
			}
		}
		a.stores[profile.ID] = driver
		a.profiles[profile.ID] = profile
	}
	return nil
}

func (a *App) parseProfile(input storageInput, row database.StorageProfile) (database.StorageProfile, error) {
	row.Name = strings.TrimSpace(input.Name)
	if row.Name == "" || utf8.RuneCountInString(row.Name) > 100 {
		return row, errors.New("存储名称长度必须在 1–100 个字符之间")
	}
	row.Kind, row.Enabled = input.Kind, input.Enabled
	if row.Kind == "local" {
		if strings.TrimSpace(input.Root) == "" || len(input.Root) > 2048 {
			return row, errors.New("请填写有效的本地目录")
		}
		root, err := filepath.Abs(strings.TrimSpace(input.Root))
		if err != nil {
			return row, errors.New("本地目录无效")
		}
		row.Root = root
		row.Endpoint, row.Region, row.Bucket, row.Prefix, row.Credentials = "", "", "", "", ""
		row.PathStyle = false
		return row, nil
	}
	if row.Kind != "s3" {
		return row, errors.New("存储类型必须是 local 或 s3")
	}
	row.Root = ""
	row.Endpoint = strings.TrimRight(strings.TrimSpace(input.Endpoint), "/")
	if row.Endpoint != "" {
		u, err := url.Parse(row.Endpoint)
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || len(row.Endpoint) > 2048 {
			return row, errors.New("Endpoint 必须是完整 HTTP(S) 地址，不包含路径、凭据或参数")
		}
	}
	row.Region, row.Bucket = strings.TrimSpace(input.Region), strings.TrimSpace(input.Bucket)
	row.Prefix = strings.Trim(strings.TrimSpace(input.Prefix), "/")
	row.PathStyle = input.PathStyle
	if row.Region == "" || len(row.Region) > 128 || row.Bucket == "" || len(row.Bucket) > 255 || strings.ContainsAny(row.Bucket, "/\\\r\n") || len(row.Prefix) > 512 || strings.ContainsAny(row.Prefix, "\\\x00\r\n") {
		return row, errors.New("请填写有效的 Region、Bucket 和对象前缀")
	}
	if input.AccessKey != "" || input.SecretKey != "" {
		if input.AccessKey == "" || input.SecretKey == "" || len(input.AccessKey) > 1024 || len(input.SecretKey) > 4096 {
			return row, errors.New("Access Key 和 Secret Key 必须一起填写，留空表示保留原凭据")
		}
		plain, err := json.Marshal(storageCredentials{AccessKey: input.AccessKey, SecretKey: input.SecretKey})
		if err != nil {
			return row, err
		}
		row.Credentials, err = secret.Seal(a.masterKey, string(plain), row.ID)
		if err != nil {
			return row, err
		}
	}
	if row.Credentials == "" {
		return row, errors.New("S3 存储需要访问凭据")
	}
	return row, nil
}

func (a *App) listProfiles(w http.ResponseWriter, r *http.Request, p principal) {
	a.settingsMu.RLock()
	defaultStorage := a.defaultStorage
	a.settingsMu.RUnlock()
	var rows []database.StorageProfile
	db := a.db.WithContext(r.Context())
	if err := db.Order("created_at ASC").Find(&rows).Error; err != nil {
		internal(w, err)
		return
	}
	items := make([]profileView, 0, len(rows))
	var totals []struct {
		StorageID string
		Count     int64
	}
	if err := db.Model(&database.Image{}).Select("storage_id, COUNT(*) AS count").Group("storage_id").Scan(&totals).Error; err != nil {
		internal(w, err)
		return
	}
	counts := make(map[string]int64, len(totals))
	for _, total := range totals {
		counts[total.StorageID] = total.Count
	}
	for _, row := range rows {
		items = append(items, profileView{StorageProfile: row, Default: defaultStorage == row.ID, HasCredentials: row.Credentials != "", Images: counts[row.ID]})
	}
	respond(w, 200, items)
}

func (a *App) createProfile(w http.ResponseWriter, r *http.Request, p principal) {
	var input storageInput
	if !decode(w, r, &input) {
		return
	}
	row, err := a.parseProfile(input, database.StorageProfile{ID: randomID()})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	driver, err := a.buildStore(row)
	if err != nil {
		fail(w, 400, "无法初始化存储，请检查目录、凭据或主密钥")
		return
	}
	if err = a.db.WithContext(r.Context()).Create(&row).Error; err != nil {
		internal(w, err)
		return
	}
	a.settingsMu.Lock()
	a.profiles[row.ID], a.stores[row.ID] = row, driver
	a.settingsMu.Unlock()
	respond(w, 201, profileView{StorageProfile: row, HasCredentials: row.Credentials != ""})
}

func (a *App) updateProfile(w http.ResponseWriter, r *http.Request, p principal) {
	var input storageInput
	if !decode(w, r, &input) {
		return
	}
	id := r.PathValue("id")
	a.settingsMu.RLock()
	old, exists := a.profiles[id]
	defaultStorage := a.defaultStorage
	active := a.storageUploads[id]
	a.settingsMu.RUnlock()
	if !exists {
		fail(w, 404, "存储配置不存在")
		return
	}
	row, err := a.parseProfile(input, old)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if id == defaultStorage && !row.Enabled {
		fail(w, 409, "请先切换默认存储，再停用此配置")
		return
	}
	var references int64
	db := a.db.WithContext(r.Context())
	if err = db.Model(&database.Image{}).Where("storage_id = ?", id).Count(&references).Error; err != nil {
		internal(w, err)
		return
	}
	locationChanged := row.Kind != old.Kind || row.Root != old.Root || row.Endpoint != old.Endpoint || row.Region != old.Region || row.Bucket != old.Bucket || row.Prefix != old.Prefix || row.PathStyle != old.PathStyle
	if (references > 0 || active > 0) && locationChanged {
		fail(w, 409, "此存储已有图片或在途上传，不能修改类型、目录、Endpoint、Region、Bucket、前缀或寻址方式，请新建配置")
		return
	}
	driver, err := a.buildStore(row)
	if err != nil {
		fail(w, 400, "无法初始化存储，请检查目录、凭据或主密钥")
		return
	}
	// 构建驱动时不持设置锁；保存位置变更前再次检查期间新增的在途上传。
	a.settingsMu.Lock()
	if locationChanged && a.storageUploads[id] > 0 {
		a.settingsMu.Unlock()
		fail(w, 409, "此存储有在途上传，暂不能修改存储位置")
		return
	}
	if err = db.Save(&row).Error; err != nil {
		a.settingsMu.Unlock()
		internal(w, err)
		return
	}
	a.profiles[id], a.stores[id] = row, driver
	a.settingsMu.Unlock()
	respond(w, 200, profileView{StorageProfile: row, Default: id == defaultStorage, HasCredentials: row.Credentials != "", Images: references})
}

func (a *App) deleteProfile(w http.ResponseWriter, r *http.Request, p principal) {
	a.settingsMu.Lock()
	id := r.PathValue("id")
	if _, exists := a.profiles[id]; !exists {
		a.settingsMu.Unlock()
		fail(w, 404, "存储配置不存在")
		return
	}
	if id == "local" || id == a.defaultStorage {
		a.settingsMu.Unlock()
		fail(w, 409, "默认配置和初始本地配置不能删除")
		return
	}
	var count int64
	db := a.db.WithContext(r.Context())
	if err := db.Model(&database.Image{}).Where("storage_id = ?", id).Count(&count).Error; err != nil {
		a.settingsMu.Unlock()
		internal(w, err)
		return
	}
	if count > 0 || a.storageUploads[id] > 0 {
		a.settingsMu.Unlock()
		fail(w, 409, "此存储仍有图片引用或在途上传，不能删除")
		return
	}
	if err := db.Delete(&database.StorageProfile{}, "id = ?", id).Error; err != nil {
		a.settingsMu.Unlock()
		internal(w, err)
		return
	}
	delete(a.profiles, id)
	delete(a.stores, id)
	a.settingsMu.Unlock()
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) defaultProfile(w http.ResponseWriter, r *http.Request, p principal) {
	a.settingsMu.Lock()
	id := r.PathValue("id")
	row, exists := a.profiles[id]
	if !exists || !row.Enabled {
		a.settingsMu.Unlock()
		fail(w, 400, "请选择已启用的存储")
		return
	}
	if err := saveSetting(a.db.WithContext(r.Context()), "default_storage", id); err != nil {
		a.settingsMu.Unlock()
		internal(w, err)
		return
	}
	a.defaultStorage = id
	a.settingsMu.Unlock()
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) testProfile(w http.ResponseWriter, r *http.Request, p principal) {
	a.settingsMu.RLock()
	driver, exists := a.stores[r.PathValue("id")]
	a.settingsMu.RUnlock()
	if !exists {
		fail(w, 404, "存储配置不存在")
		return
	}
	releasePolicy(w)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	key := ".qpix-probe/" + randomID() + ".txt"
	content := []byte("QPix storage connection test")
	if err := driver.Put(ctx, key, bytes.NewReader(content)); err != nil {
		fail(w, 502, "写入测试失败，请检查连接、凭据和写权限")
		return
	}
	defer func() {
		cleanup, finish := context.WithTimeout(context.Background(), 10*time.Second)
		defer finish()
		_ = driver.Delete(cleanup, key)
	}()
	reader, err := driver.Open(ctx, key)
	if err != nil {
		fail(w, 502, "读取测试失败，请检查读权限")
		return
	}
	data, err := io.ReadAll(io.LimitReader(reader, 1024))
	closeErr := reader.Close()
	if err != nil || closeErr != nil || !bytes.Equal(data, content) {
		fail(w, 502, "存储读取校验失败")
		return
	}
	if err = driver.Delete(ctx, key); err != nil {
		fail(w, 502, "删除测试失败，请检查删除权限")
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func saveSetting(db *gorm.DB, key, value string) error {
	return db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&database.Setting{Key: key, Value: value}).Error
}

func (a *App) getStore(id string) storage.Store {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return a.stores[id]
}
