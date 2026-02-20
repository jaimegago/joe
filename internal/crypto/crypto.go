// Package crypto provides AES-256-GCM encryption for sensitive data at rest.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// encPrefix is prepended to every encrypted value stored in the database.
// Values without this prefix are treated as plaintext for backward compatibility.
const encPrefix = "enc:"

// ErrInvalidKey is returned when the key has an unexpected length.
var ErrInvalidKey = errors.New("crypto: key must be 32 bytes (AES-256)")

// Encrypt encrypts plaintext using AES-256-GCM and returns a base64-encoded
// string with the encPrefix prepended. The output is safe to store as text.
// key must be exactly 32 bytes.
func Encrypt(key, plaintext []byte) (string, error) {
	if len(key) != 32 {
		return "", ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a value produced by Encrypt. If the value does not have
// the encPrefix it is returned as-is (plaintext passthrough for backward
// compatibility).  key must be exactly 32 bytes.
func Decrypt(key []byte, value string) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKey
	}
	if len(value) < len(encPrefix) || value[:len(encPrefix)] != encPrefix {
		// Not encrypted — return as plaintext bytes for backward compatibility.
		return []byte(value), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value[len(encPrefix):])
	if err != nil {
		return nil, fmt.Errorf("crypto: base64 decode: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: create GCM: %w", err)
	}
	if len(decoded) < gcm.NonceSize() {
		return nil, errors.New("crypto: ciphertext too short")
	}
	nonce, ciphertext := decoded[:gcm.NonceSize()], decoded[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: decrypt: %w", err)
	}
	return plaintext, nil
}

// IsEncrypted reports whether the value was produced by Encrypt.
func IsEncrypted(value string) bool {
	return len(value) >= len(encPrefix) && value[:len(encPrefix)] == encPrefix
}

// LoadOrCreateKey loads a 32-byte key from keyPath. If the file does not
// exist, a new random key is generated and written to that path (mode 0600).
// The directory must already exist.
func LoadOrCreateKey(keyPath string) ([]byte, error) {
	data, err := os.ReadFile(keyPath)
	if err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("crypto: key file %s has %d bytes, want 32", keyPath, len(data))
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("crypto: read key file: %w", err)
	}

	// Generate a new random key.
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("crypto: generate key: %w", err)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return nil, fmt.Errorf("crypto: create key directory: %w", err)
	}

	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return nil, fmt.Errorf("crypto: write key file: %w", err)
	}
	return key, nil
}
