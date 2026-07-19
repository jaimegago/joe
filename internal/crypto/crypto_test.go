package crypto

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

	key, err := LoadOrCreateKey(keyPath, firstRun)
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

	key, err := LoadOrCreateKey(keyPath, firstRun)
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

	_, err := LoadOrCreateKey(keyPath, firstRun)
	if err == nil {
		t.Error("expected error for wrong-length key file, got nil")
	}
}

func TestLoadOrCreateKey_Idempotent(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "encryption.key")

	key1, err := LoadOrCreateKey(keyPath, firstRun)
	if err != nil {
		t.Fatal(err)
	}
	key2, err := LoadOrCreateKey(keyPath, firstRun)
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

	_, err := LoadOrCreateKey(keyPath, firstRun)
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
	_, err := LoadOrCreateKey(keyPath, firstRun)
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

func TestLoadOrCreateKey_MkdirError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write to any directory, skipping permission test")
	}
	dir := t.TempDir()
	// Make the temp dir read-only so MkdirAll cannot create a subdirectory.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0755) //nolint:errcheck

	// Key path requires creating a new subdirectory inside the read-only dir.
	keyPath := filepath.Join(dir, "newsubdir", "encryption.key")
	_, err := LoadOrCreateKey(keyPath, firstRun)
	if err == nil {
		t.Error("expected error when MkdirAll fails, got nil")
	}
}

// firstRun is the EncryptedDataProbe answer for an installation that holds
// nothing encrypted — the genuine first-run case, in which minting a key is
// correct and loses nothing.
func firstRun() (bool, error) { return false, nil }

// TestLoadOrCreateKey_AbsentKeyWithEncryptedDataRefuses pins Rule A, the reason
// the probe exists at all: an absent key file is a first run ONLY if the
// database agrees. With encrypted data present, the same absent file means the
// key was lost, and minting a replacement over it would be silent, permanent,
// and unrecoverable — so it refuses instead.
func TestLoadOrCreateKey_AbsentKeyWithEncryptedDataRefuses(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "encryption.key")

	_, err := LoadOrCreateKey(keyPath, func() (bool, error) { return true, nil })
	if !errors.Is(err, ErrKeyAbsentWithEncryptedData) {
		t.Fatalf("error = %v, want ErrKeyAbsentWithEncryptedData", err)
	}
	// The refusal must leave no key behind. A key written on the way out would
	// turn the next boot into the disaster this branch just prevented, because
	// the file would then be present and the probe never consulted.
	if _, statErr := os.Stat(keyPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("key file exists after refusal (stat error = %v), want no file written", statErr)
	}
	// The operator has to find the file; the message must name it.
	if !strings.Contains(err.Error(), keyPath) {
		t.Errorf("error %q does not name the key path %q", err, keyPath)
	}
}

// TestLoadOrCreateKey_ProbeNotConsultedWhenKeyPresent pins that the probe is
// strictly the absent-file discriminator. A present key is loaded without asking
// the database anything — this is the ordinary boot, and it must not depend on a
// query that could fail.
func TestLoadOrCreateKey_ProbeNotConsultedWhenKeyPresent(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "encryption.key")
	want := make([]byte, 32)
	if err := os.WriteFile(keyPath, want, 0600); err != nil {
		t.Fatal(err)
	}

	key, err := LoadOrCreateKey(keyPath, func() (bool, error) {
		t.Error("probe consulted even though the key file exists")
		return true, nil
	})
	if err != nil {
		t.Fatalf("LoadOrCreateKey() error = %v", err)
	}
	if !bytes.Equal(key, want) {
		t.Error("loaded key does not match the key on disk")
	}
}

// TestLoadOrCreateKey_ProbeErrorFailsClosed pins the direction of the failure.
// An unanswerable probe is not evidence of a first run, and minting is the
// irreversible direction, so an unreadable database refuses rather than assuming
// there is nothing to lose.
func TestLoadOrCreateKey_ProbeErrorFailsClosed(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "encryption.key")

	probeErr := errors.New("database unreachable")
	_, err := LoadOrCreateKey(keyPath, func() (bool, error) { return false, probeErr })
	if !errors.Is(err, probeErr) {
		t.Fatalf("error = %v, want it to wrap the probe error", err)
	}
	if _, statErr := os.Stat(keyPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("key file exists after a failed probe (stat error = %v), want no file written", statErr)
	}
}

// TestLoadOrCreateKey_NilProbeRefused pins that a forgotten probe is an error
// rather than an implied "nothing is encrypted". The implied answer is exactly
// the unconditional-generate behaviour Rule A removes, so a caller that failed to
// wire the database must not silently get it back.
func TestLoadOrCreateKey_NilProbeRefused(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "encryption.key")

	if _, err := LoadOrCreateKey(keyPath, nil); err == nil {
		t.Fatal("expected an error for a nil probe, got nil")
	}
	if _, statErr := os.Stat(keyPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("key file exists after a nil-probe refusal (stat error = %v), want no file written", statErr)
	}
}

// TestDecrypt_WrongKeyIsAuthenticationClass pins the typed error Rule B branches
// on. A wrong key and a tampered ciphertext are the SAME failure to AES-GCM, and
// both must be reachable via errors.Is so a caller can separate them from decode
// and I/O failures — which must NOT carry the sentinel.
func TestDecrypt_WrongKeyIsAuthenticationClass(t *testing.T) {
	keyA := make([]byte, 32)
	for i := range keyA {
		keyA[i] = byte(i)
	}
	keyB := make([]byte, 32)
	for i := range keyB {
		keyB[i] = byte(i + 100)
	}

	encrypted, err := Encrypt(keyA, []byte(`{"token":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}

	_, err = Decrypt(keyB, encrypted)
	if !errors.Is(err, ErrAuthentication) {
		t.Errorf("wrong-key decrypt error = %v, want ErrAuthentication", err)
	}

	// Tamper with the CIPHERTEXT, not with its base64 envelope: flipping a
	// character of the encoded string usually breaks base64 first and never
	// reaches GCM, which would test the wrong branch.
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encrypted, "enc:"))
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xFF
	tampered := "enc:" + base64.StdEncoding.EncodeToString(raw)
	if _, err := Decrypt(keyA, tampered); !errors.Is(err, ErrAuthentication) {
		t.Errorf("tampered-ciphertext error = %v, want ErrAuthentication", err)
	}

	// Not the authentication class: a decode failure never reached GCM.
	if _, err := Decrypt(keyA, "enc:!!!not-base64!!!"); errors.Is(err, ErrAuthentication) {
		t.Error("base64 decode failure carries ErrAuthentication; it must not")
	}
}
