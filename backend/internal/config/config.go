package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Address, DBDriver, DSN, StorageRoot, PublicURL, AppURL string
	AdminUsername, AdminPassword                           string
	MaxUploadBytes, MaxPixels                              int64
	SecureCookie                                           bool
	MasterKeyFile                                          string
	WebRoot                                                string
	TrustedProxies                                         []string
	DisableAccessRules                                     bool
}

func Load() (Config, error) {
	c := Config{
		Address: env("QPIX_ADDR", "127.0.0.1:8080"), DBDriver: env("QPIX_DB_DRIVER", "sqlite"),
		DSN: os.Getenv("QPIX_DB_DSN"), StorageRoot: env("QPIX_STORAGE_ROOT", "data/uploads"),
		PublicURL: strings.TrimRight(os.Getenv("QPIX_PUBLIC_URL"), "/"), AppURL: strings.TrimRight(os.Getenv("QPIX_APP_URL"), "/"),
		AdminUsername: env("QPIX_ADMIN_USERNAME", "admin"), AdminPassword: os.Getenv("QPIX_ADMIN_PASSWORD"),
		MaxUploadBytes: 20 << 20, MaxPixels: 25_000_000,
		MasterKeyFile: env("QPIX_MASTER_KEY_FILE", "data/master.key"),
		WebRoot:       os.Getenv("QPIX_WEB_ROOT"),
	}
	if c.DBDriver == "sqlite" && c.DSN == "" {
		c.DSN = "data/qpix.db"
	}
	if c.DBDriver != "sqlite" && c.DBDriver != "postgres" && c.DBDriver != "mysql" {
		return c, fmt.Errorf("QPIX_DB_DRIVER 必须是 sqlite、postgres 或 mysql")
	}
	if c.DSN == "" {
		return c, fmt.Errorf("请设置 QPIX_DB_DSN")
	}
	var err error
	if value := os.Getenv("QPIX_TRUSTED_PROXIES"); value != "" {
		for _, item := range strings.Split(value, ",") {
			c.TrustedProxies = append(c.TrustedProxies, strings.TrimSpace(item))
		}
	}
	if value := os.Getenv("QPIX_DISABLE_ACCESS_RULES"); value != "" {
		c.DisableAccessRules, err = strconv.ParseBool(value)
		if err != nil {
			return c, fmt.Errorf("QPIX_DISABLE_ACCESS_RULES 必须是布尔值")
		}
	}
	if raw := os.Getenv("QPIX_MAX_UPLOAD_MB"); raw != "" {
		var n int64
		n, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 || n > 100 {
			return c, fmt.Errorf("QPIX_MAX_UPLOAD_MB 必须在 1 到 100 之间")
		}
		c.MaxUploadBytes = n << 20
	}
	if raw := os.Getenv("QPIX_SECURE_COOKIE"); raw != "" {
		c.SecureCookie, err = strconv.ParseBool(raw)
		if err != nil {
			return c, fmt.Errorf("QPIX_SECURE_COOKIE 必须是布尔值")
		}
	}
	for _, value := range []string{c.PublicURL, c.AppURL} {
		if err = ValidateURL(value); err != nil {
			return c, err
		}
	}
	return c, nil
}

func ValidateURL(value string) error {
	if value == "" {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("域名必须是完整的 HTTP(S) 地址，不能包含路径、凭据或查询参数")
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
