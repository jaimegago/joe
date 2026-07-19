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

// ErrAuthentication reports that AES-GCM refused to authenticate a ciphertext.
// It is the single failure class that means "this key cannot read this data" —
// the key is wrong, or the stored bytes were altered. GCM cannot distinguish the
// two, so callers must not try to; the value of the sentinel is that a caller
// can separate this class from transient I/O and decoding errors and act on it.
// Decrypt wraps it around every gcm.Open failure.
var ErrAuthentication = errors.New("crypto: authentication failed")

// ErrKeyAbsentWithEncryptedData reports that the key file does not exist while
// the installation already holds data encrypted under some key. That is a lost
// key, not a first run, and LoadOrCreateKey refuses to mint a replacement over
// it — minting would be silent, permanent, and unrecoverable.
var ErrKeyAbsentWithEncryptedData = errors.New("crypto: encryption key file is absent but encrypted data is present")

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
		// Wrap the sentinel, not the GCM error: the GCM error carries no
		// information a caller can branch on (it is a single opaque value for
		// every authentication failure), and callers need to tell this class
		// apart from decode and I/O failures. See ErrAuthentication.
		return nil, fmt.Errorf("%w: %v", ErrAuthentication, err)
	}
	return plaintext, nil
}

// IsEncrypted reports whether the value was produced by Encrypt.
func IsEncrypted(value string) bool {
	return len(value) >= len(encPrefix) && value[:len(encPrefix)] == encPrefix
}

// EncryptedDataProbe reports whether the installation already holds data
// encrypted under some key. It is the discriminator LoadOrCreateKey cannot
// derive for itself: from inside the loader, a first install and an install
// whose key was deleted look identical — the file is simply absent in both
// cases. The answer lives in the database, so the caller supplies it.
//
// The probe answers about DATA, not about rows: an install with component rows
// that are all plaintext (the backward-compatibility case) is still a first run
// for key purposes, because no existing value becomes unreadable.
type EncryptedDataProbe func() (bool, error)

// LoadOrCreateKey loads a 32-byte key from keyPath. If the file does not exist,
// the decision to mint a fresh key is CONDITIONAL on probe reporting that no
// encrypted data exists (mode 0600 on write; the parent directory is created).
//
// The conditional is the whole point. Generating unconditionally is correct for
// a first install and catastrophic for an existing one: the new key cannot read
// a single stored value, every component config stays encrypted under a key that
// no longer exists, and — before this guard — boot SUCCEEDED, so the operator
// got a Joe that started cleanly and reached nothing. There is no recovery once
// the original key is gone, which is why this fails closed rather than warning.
//
// probe is REQUIRED. A nil probe is an error rather than an implied "no data":
// the implied answer is exactly the unconditional-generate behaviour this guard
// exists to remove, and a caller that forgot to wire the database would silently
// get it back.
func LoadOrCreateKey(keyPath string, probe EncryptedDataProbe) ([]byte, error) {
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

	if probe == nil {
		return nil, errors.New("crypto: LoadOrCreateKey requires an EncryptedDataProbe")
	}
	hasEncrypted, err := probe()
	if err != nil {
		// Fail closed. An unanswerable probe is not evidence of a first run, and
		// minting on an unknown answer is the irreversible direction.
		return nil, fmt.Errorf("crypto: cannot determine whether encrypted data exists: %w", err)
	}
	if hasEncrypted {
		return nil, fmt.Errorf("%w (key path %s)", ErrKeyAbsentWithEncryptedData, keyPath)
	}

	// Genuine first run: no key on disk and nothing encrypted to lose.
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
