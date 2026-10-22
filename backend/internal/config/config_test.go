package config

import "testing"

func TestRuntimeEnvironmentIgnored(t *testing.T) {
	for _, key := range []string{"QPIX_PUBLIC_URL", "QPIX_APP_URL", "QPIX_MAX_UPLOAD_MB", "QPIX_SECURE_COOKIE", "QPIX_TRUSTED_PROXIES"} {
		t.Setenv(key, "invalid-legacy-value")
	}
	t.Setenv("QPIX_DB_DRIVER", "sqlite")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicURL != "" || cfg.AppURL != "" || cfg.MaxUploadBytes != 20<<20 || cfg.SecureCookie || len(cfg.TrustedProxies) != 0 {
		t.Fatal("已移除的环境变量仍影响运行设置")
	}
}

func TestPublicURLValidation(t *testing.T) {
	for _, value := range []string{"", "http://127.0.0.1:5173", "https://img.example.com"} {
		if err := ValidateURL(value); err != nil {
			t.Errorf("合法域名被拒绝 %q: %v", value, err)
		}
	}
	for _, value := range []string{"javascript:alert(1)", "//img.example.com", "https://user:password@img.example.com", "https://img.example.com/a", "https://img.example.com?token=secret", "https://img.example.com#fragment"} {
		if err := ValidateURL(value); err == nil {
			t.Errorf("不应接受域名 %q", value)
		}
	}
}
