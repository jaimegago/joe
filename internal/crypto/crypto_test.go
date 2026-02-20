package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty", ""},
		{"short", "hello"},
		{"json", `{"url":"http://prometheus:9090","api_key":"secret"}`},
		{"long", string(make([]byte, 1024))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := Encrypt(key, []byte(tt.plaintext))
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}
			if !IsEncrypted(encrypted) {
				t.Error("IsEncrypted() returned false for encrypted value")
			}
			decrypted, err := Decrypt(key, encrypted)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}
			if string(decrypted) != tt.plaintext {
				t.Errorf("Decrypt() = %q, want %q", string(decrypted), tt.plaintext)
			}
		})
	}
}

func TestEncrypt_DifferentNonceEachTime(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("same input")

	enc1, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	enc2, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if enc1 == enc2 {
		t.Error("Encrypt() produced identical ciphertext for same plaintext — nonce reuse detected")
	}
}

func TestDecrypt_PlaintextPassthrough(t *testing.T) {
	key := make([]byte, 32)
	plaintext := `{"url":"http://prometheus:9090"}`

	// Value without enc: prefix is passed through as-is (backward compat).
	result, err := Decrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if string(result) != plaintext {
		t.Errorf("Decrypt() = %q, want %q", string(result), plaintext)
	}
}

func TestEncrypt_InvalidKeyLength(t *testing.T) {
	_, err := Encrypt([]byte("tooshort"), []byte("data"))
	if err == nil {
		t.Error("expected error for short key, got nil")
	}
}

func TestDecrypt_InvalidKeyLength(t *testing.T) {
	_, err := Decrypt([]byte("tooshort"), "enc:somedata")
	if err == nil {
		t.Error("expected error for short key, got nil")
	}
}

func TestDecrypt_Tampered(t *testing.T) {
	key := make([]byte, 32)
	encrypted, err := Encrypt(key, []byte("secret data"))
	if err != nil {
		t.Fatal(err)
	}
	// Tamper with the last byte.
	bs := []byte(encrypted)
	bs[len(bs)-1] ^= 0xFF
	_, err = Decrypt(key, string(bs))
	if err == nil {
		t.Error("expected error for tampered ciphertext, got nil")
	}
}

func TestLoadOrCreateKey_CreatesNew(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "encryption.key")

	key, err := LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateKey() error = %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key length = %d, want 32", len(key))
	}

	// File must exist and be 32 bytes.
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(data, key) {
		t.Error("key on disk differs from returned key")
	}
}

func TestLoadOrCreateKey_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "encryption.key")

	// Write a known key.
	want := make([]byte, 32)
	for i := range want {
		want[i] = byte(i + 1)
	}
	if err := os.WriteFile(keyPath, want, 0600); err != nil {
		t.Fatal(err)
	}

	key, err := LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateKey() error = %v", err)
	}
	if !bytes.Equal(key, want) {
		t.Error("loaded key does not match expected")
	}
}

func TestLoadOrCreateKey_WrongLength(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "encryption.key")
	if err := os.WriteFile(keyPath, []byte("tooshort"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadOrCreateKey(keyPath)
	if err == nil {
		t.Error("expected error for wrong-length key file, got nil")
	}
}

func TestLoadOrCreateKey_Idempotent(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "encryption.key")

	key1, err := LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key2, err := LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key1, key2) {
		t.Error("two calls to LoadOrCreateKey returned different keys")
	}
}

func TestLoadOrCreateKey_ReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read any file, skipping permission test")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "encryption.key")

	// Create a valid-length key file but make it unreadable.
	want := make([]byte, 32)
	if err := os.WriteFile(keyPath, want, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(keyPath, 0600) //nolint:errcheck

	_, err := LoadOrCreateKey(keyPath)
	if err == nil {
		t.Error("expected error for unreadable key file, got nil")
	}
}

func TestLoadOrCreateKey_WriteError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write to any directory, skipping permission test")
	}
	dir := t.TempDir()
	// Make directory read-only so WriteFile fails.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0755) //nolint:errcheck

	keyPath := filepath.Join(dir, "encryption.key")
	_, err := LoadOrCreateKey(keyPath)
	if err == nil {
		t.Error("expected error when key file cannot be written, got nil")
	}
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	key := make([]byte, 32)
	// "enc:" prefix but the rest is not valid base64.
	_, err := Decrypt(key, "enc:!!!not-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64, got nil")
	}
}

func TestDecrypt_CiphertextTooShort(t *testing.T) {
	key := make([]byte, 32)
	// "AQIDBA==" base64-decodes to 4 bytes, which is less than GCM nonce size (12).
	value := "enc:" + "AQIDBA=="
	_, err := Decrypt(key, value)
	if err == nil {
		t.Error("expected error for too-short ciphertext, got nil")
	}
}
