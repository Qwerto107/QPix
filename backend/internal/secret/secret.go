package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// LoadKey 首次启动生成独立主密钥，已有密钥损坏时直接报错，绝不静默替换。
func LoadKey(path string) ([]byte, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		key := make([]byte, 32)
		if _, err = io.ReadFull(rand.Reader, key); err == nil {
			_, err = file.WriteString(base64.StdEncoding.EncodeToString(key))
		}
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return key, nil
	}
	if !os.IsExist(err) {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil || len(key) != 32 {
		return nil, errors.New("主密钥格式无效，请恢复原 master.key，不要重新生成")
	}
	return key, nil
}

func Seal(key []byte, plain, identity string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	// 将存储配置 ID 绑定为附加数据，防止密文被复制到另一条存储配置后使用。
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), []byte(identity))), nil
}

func Open(key []byte, encrypted, identity string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil || len(data) < gcm.NonceSize() {
		return "", errors.New("凭据密文无效")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], []byte(identity))
	if err != nil {
		return "", errors.New("无法解密存储凭据，请检查主密钥")
	}
	return string(plain), nil
}
