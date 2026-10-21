package config

import "testing"

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
