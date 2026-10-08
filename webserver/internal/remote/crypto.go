package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// ErrSecretUnavailable 表示凭据加密密钥未配置（或未派生成功）。
var ErrSecretUnavailable = errors.New("remote secret key is not configured")

// DeriveKey 把任意长度的密钥文本派生为 AES-256-GCM 所需的 32 字节密钥。
//
// 用 SHA-256 而非截断：允许配置文件里写任意可读的密钥短语，避免「必须正好 32 字节」的
// 约束；同时也把 jwt.secret 这类既有密钥安全地复用到凭据加密场景（SHA-256 单向隔离）。
//
// 参数 Parameters:
//   - secretText (string): 密钥文本（配置或环境变量提供）。
//
// 返回 Returns:
//   - key ([]byte): 32 字节派生密钥。
func DeriveKey(secretText string) []byte {
	sum := sha256.Sum256([]byte(secretText))
	return sum[:]
}

// EncryptSecret 用 AES-256-GCM 加密明文，输出 base64(nonce || ciphertext)。
//
// 参数 Parameters:
//   - key ([]byte): 32 字节 AES 密钥。
//   - plaintext (string): 待加密明文。
//
// 返回 Returns:
//   - encoded (string): base64 编码的密文（含 nonce 前缀）。
//   - err (error): 加密失败时返回非 nil。
func EncryptSecret(key []byte, plaintext string) (string, error) {
	if len(key) != 32 {
		return "", ErrSecretUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// DecryptSecret 解密 EncryptSecret 的输出，返回明文。
//
// 参数 Parameters:
//   - key ([]byte): 32 字节 AES 密钥。
//   - encoded (string): base64 编码的密文；为空时返回空明文（视为「未设置」）。
//
// 返回 Returns:
//   - plaintext (string): 明文。
//   - err (error): 解密失败时返回非 nil。
func DecryptSecret(key []byte, encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	if len(key) != 32 {
		return "", ErrSecretUnavailable
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
