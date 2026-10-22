package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"qpix/internal/config"
	"strconv"

	"qpix/internal/database"
	"qpix/internal/secret"
)

type processingSettings struct {
	Mode    string `json:"mode"`
	Quality int    `json:"quality"`
}

func (a *App) initRuntime() error {
	a.processingSettings = processingSettings{Mode: "none", Quality: 82}
	a.defaultStorage = "local"
	a.accessRules = map[string]accessRule{}
	for _, scope := range []string{"admin", "upload", "images"} {
		rule, _ := compileRule(ruleInput{Scope: scope})
		a.accessRules[scope] = rule
	}
	for _, raw := range a.cfg.TrustedProxies {
		prefix, err := parseNetwork(raw)
		if err != nil {
			return errors.New("可信代理包含无效 IP 或网段")
		}
		a.trustedProxies = append(a.trustedProxies, prefix)
	}
	if a.cfg.MasterKeyFile == "" {
		a.cfg.MasterKeyFile = filepath.Join(filepath.Dir(a.cfg.StorageRoot), "master.key")
	}
	var encryptedCount int64
	if err := a.db.Model(&database.StorageProfile{}).Where("credentials <> ?", "").Count(&encryptedCount).Error; err != nil {
		return err
	}
	var shareCount int64
	if err := a.db.Model(&database.Share{}).Count(&shareCount).Error; err != nil {
		return err
	}
	encryptedCount += shareCount
	if _, err := os.Stat(a.cfg.MasterKeyFile); os.IsNotExist(err) && encryptedCount > 0 {
		return errors.New("已有加密凭据或分享链接但主密钥缺失，请恢复原 master.key")
	}
	var err error
	a.masterKey, err = secret.LoadKey(a.cfg.MasterKeyFile)
	if err != nil {
		return err
	}
	if err = a.initProfiles(); err != nil {
		return err
	}
	if shareCount > 0 {
		var share database.Share
		if err = a.db.First(&share).Error; err != nil {
			return err
		}
		if _, err = secret.Open(a.masterKey, share.TokenCipher, "share:"+share.ID); err != nil {
			return err
		}
	}
	var settings []database.Setting
	if err = a.db.Find(&settings).Error; err != nil {
		return err
	}
	for _, setting := range settings {
		switch setting.Key {
		case "app_url":
			if err = config.ValidateURL(setting.Value); err != nil {
				return err
			}
			a.appURL = setting.Value
		case "secure_cookie":
			a.secureCookie, err = strconv.ParseBool(setting.Value)
			if err != nil {
				return err
			}
		case "max_pixels":
			a.maxPixels, err = strconv.ParseInt(setting.Value, 10, 64)
			if err != nil || !validMaxPixels(a.maxPixels) {
				return errors.New("图片像素上限无效")
			}
		case "trusted_proxies":
			var values []string
			if err = json.Unmarshal([]byte(setting.Value), &values); err != nil {
				return err
			}
			a.trustedProxies, err = parseTrustedProxies(values)
			if err != nil {
				return err
			}
		case "max_upload_bytes":
			limit, parseErr := strconv.ParseInt(setting.Value, 10, 64)
			if parseErr != nil || !validUploadLimit(limit) {
				return errors.New("服务器单张上传上限配置无效")
			}
			a.maxUploadBytes = limit
		case "default_storage":
			a.defaultStorage = setting.Value
		case "processing":
			if err = json.Unmarshal([]byte(setting.Value), &a.processingSettings); err != nil {
				return err
			}
		}
	}
	// 旧开发版本的智能压缩默认值对应新的 WebP 模式。
	if a.processingSettings.Mode == "smart" {
		a.processingSettings.Mode = "webp"
	}
	if !validProcessing(a.processingSettings) {
		return errors.New("图片压缩配置无效")
	}
	if profile, exists := a.profiles[a.defaultStorage]; !exists || !profile.Enabled {
		return errors.New("默认存储不存在或已停用")
	}
	var rules []database.AccessRule
	if err = a.db.Find(&rules).Error; err != nil {
		return err
	}
	for _, row := range rules {
		input := ruleInput{Scope: row.Scope, IPEnabled: row.IPEnabled, RefererEnabled: row.RefererEnabled, AllowEmptyReferer: row.AllowEmptyReferer}
		if err = json.Unmarshal([]byte(row.IPs), &input.IPs); err != nil {
			return err
		}
		if err = json.Unmarshal([]byte(row.Referers), &input.Referers); err != nil {
			return err
		}
		// 上传仅校验身份和 IP；忽略旧版本遗留的 Referer 设置。
		if input.Scope == "upload" {
			input.RefererEnabled, input.AllowEmptyReferer, input.Referers = false, false, nil
		}
		rule, err := compileRule(input)
		if err != nil {
			return err
		}
		a.accessRules[row.Scope] = rule
	}
	return nil
}

func validMaxPixels(value int64) bool { return value >= 1_000_000 && value <= 100_000_000 }

func parseTrustedProxies(values []string) ([]netip.Prefix, error) {
	if len(values) > 200 {
		return nil, errors.New("可信代理最多 200 项")
	}
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := parseNetwork(value)
		if err != nil {
			return nil, err
		}
		result = append(result, prefix)
	}
	normalized := make([]string, 0, len(result))
	for _, prefix := range result {
		normalized = append(normalized, prefix.String())
	}
	raw, _ := json.Marshal(normalized)
	if len(raw) > 2048 {
		return nil, errors.New("可信代理列表过长，JSON 内容最多 2048 字节")
	}
	return result, nil
}

func (a *App) cookiesSecure() bool {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return a.secureCookie
}

func (a *App) pixelLimit() int64 {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return a.maxPixels
}

func validUploadLimit(limit int64) bool {
	return limit >= 1<<20 && limit <= 100<<20
}

func (a *App) uploadLimit() int64 {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	return a.maxUploadBytes
}

func validProcessing(settings processingSettings) bool {
	return (settings.Mode == "none" || settings.Mode == "preserve" || settings.Mode == "webp") && settings.Quality >= 1 && settings.Quality <= 100
}
func (a *App) getProcessing(w http.ResponseWriter, r *http.Request, p principal) {
	a.settingsMu.RLock()
	settings := a.processingSettings
	a.settingsMu.RUnlock()
	respond(w, 200, settings)
}
func (a *App) updateProcessing(w http.ResponseWriter, r *http.Request, p principal) {
	var settings processingSettings
	if !decode(w, r, &settings) {
		return
	}
	if !validProcessing(settings) {
		fail(w, 400, "模式必须是 none、preserve 或 webp，画质必须在 1–100 之间")
		return
	}
	raw, _ := json.Marshal(settings)
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if err := saveSetting(a.db.WithContext(r.Context()), "processing", string(raw)); err != nil {
		internal(w, err)
		return
	}
	a.processingSettings = settings
	respond(w, 200, settings)
}
