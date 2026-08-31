package encrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

type EncryptKey struct {
	key []byte // Must be exactly 32 bytes for AES-256
}

func NewEncryptKey(secretKey string) (*EncryptKey, error) {
	key, err := normalizeAES256Key(secretKey)
	if err != nil {
		return nil, err
	}
	return &EncryptKey{key: key}, nil
}

// normalizeAES256Key accepts a raw 32-byte string or a 64-char hex encoding of 32 bytes
// (as produced by scripts/generate-dev-secrets.sh).
func normalizeAES256Key(secretKey string) ([]byte, error) {
	if len(secretKey) == 64 {
		if decoded, err := hex.DecodeString(secretKey); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	key := []byte(secretKey)
	if len(key) != 32 {
		return nil, errors.New("INVALID_KEY_LENGTH: Secret key must be exactly 32 bytes for AES-256")
	}
	return key, nil
}

// Encryptor is the historical name used by engine packages.
type Encryptor = EncryptKey

// NewEncryptor is an alias for NewEncryptKey.
func NewEncryptor(secretKey string) (*EncryptKey, error) {
	return NewEncryptKey(secretKey)
}

// Encrypt string payload to Base64 AES-GCM string
func (e *EncryptKey) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher block: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM mode: %w", err)
	}

	// Create random nonce (never reuse nonces!)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate random nonce: %w", err)
	}

	// Encrypt payload and append nonce at the beginning
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt Base64 AES-GCM string back to plaintext
func (e *EncryptKey) Decrypt(cipherTextBase64 string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(cipherTextBase64)
	if err != nil {
		return "", fmt.Errorf("invalid base64 ciphertext: %w", err)
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher block: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM mode: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	// Extract nonce and encrypted ciphertext
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("DECRYPTION_FAILED: Authentication tag mismatch or bad key: %w", err)
	}

	return string(plaintext), nil
}

// DecryptStored decrypts a value read from SQLite. If decryption fails (legacy or
// plaintext rows), the original string is returned unchanged.
func (e *EncryptKey) DecryptStored(value string) string {
	if e == nil || value == "" {
		return value
	}
	plain, err := e.Decrypt(value)
	if err != nil {
		return value
	}
	return plain
}
