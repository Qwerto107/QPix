package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
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
	if value := os.Getenv("QPIX_DISABLE_ACCESS_RULES"); value != "" {
		c.DisableAccessRules, err = strconv.ParseBool(value)
		if err != nil {
			return c, fmt.Errorf("QPIX_DISABLE_ACCESS_RULES 必须是布尔值")
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
