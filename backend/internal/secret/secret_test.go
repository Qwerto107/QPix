package secret

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestKeyAndAuthenticatedEncryption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	key, err := LoadKey(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadKey(path)
	if err != nil || !bytes.Equal(key, loaded) {
		t.Fatal("主密钥不能重复读取", err)
	}
	sealed, err := Seal(key, "test-secret", "profile-a")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Open(key, sealed, "profile-a")
	if err != nil || plain != "test-secret" {
		t.Fatal("解密失败", err)
	}
	for _, test := range []struct {
		key       []byte
		value, id string
	}{{key, sealed, "profile-b"}, {bytes.Repeat([]byte{1}, 32), sealed, "profile-a"}, {key, sealed[:len(sealed)-5] + "AAAAA", "profile-a"}} {
		if _, err = Open(test.key, test.value, test.id); err == nil {
			t.Fatal("接受被替换的密钥、密文或配置 ID")
		}
	}
	if err = os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadKey(path); err == nil {
		t.Fatal("不应覆盖损坏的主密钥")
	}
	content, _ := os.ReadFile(path)
	if string(content) != "broken" {
		t.Fatal("损坏密钥被意外覆盖")
	}
}
