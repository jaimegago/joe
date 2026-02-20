package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/crypto"
)

// encryptedSourceRepository wraps a SourceRepository and transparently
// encrypts source.Config on write and decrypts it on read.
type encryptedSourceRepository struct {
	inner SourceRepository
	key   []byte // 32-byte AES-256 key
}

// NewEncryptedSourceRepository wraps repo so that source Config fields are
// encrypted at rest. key must be exactly 32 bytes.
func NewEncryptedSourceRepository(repo SourceRepository, key []byte) (SourceRepository, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("store: encryption key must be 32 bytes, got %d", len(key))
	}
	return &encryptedSourceRepository{inner: repo, key: key}, nil
}

func (r *encryptedSourceRepository) Create(ctx context.Context, source *Source) error {
	enc, err := r.encryptSource(source)
	if err != nil {
		return err
	}
	return r.inner.Create(ctx, enc)
}

func (r *encryptedSourceRepository) Get(ctx context.Context, id string) (*Source, error) {
	s, err := r.inner.Get(ctx, id)
	if err != nil || s == nil {
		return s, err
	}
	return r.decryptSource(s)
}

func (r *encryptedSourceRepository) List(ctx context.Context) ([]*Source, error) {
	sources, err := r.inner.List(ctx)
	if err != nil {
		return nil, err
	}
	return r.decryptAll(sources)
}

func (r *encryptedSourceRepository) ListByType(ctx context.Context, sourceType string) ([]*Source, error) {
	sources, err := r.inner.ListByType(ctx, sourceType)
	if err != nil {
		return nil, err
	}
	return r.decryptAll(sources)
}

func (r *encryptedSourceRepository) Update(ctx context.Context, source *Source) error {
	enc, err := r.encryptSource(source)
	if err != nil {
		return err
	}
	return r.inner.Update(ctx, enc)
}

func (r *encryptedSourceRepository) UpdateSyncStatus(ctx context.Context, id string, syncedAt time.Time, lastError string) error {
	return r.inner.UpdateSyncStatus(ctx, id, syncedAt, lastError)
}

func (r *encryptedSourceRepository) Delete(ctx context.Context, id string) error {
	return r.inner.Delete(ctx, id)
}

// encryptSource returns a shallow copy of source with Config encrypted.
// The original source is not modified.
func (r *encryptedSourceRepository) encryptSource(source *Source) (*Source, error) {
	if len(source.Config) == 0 {
		return source, nil
	}
	encrypted, err := crypto.Encrypt(r.key, source.Config)
	if err != nil {
		return nil, fmt.Errorf("store: encrypt source config: %w", err)
	}
	// Store as a JSON string so the column type remains TEXT/JSON.
	quoted, err := json.Marshal(encrypted)
	if err != nil {
		return nil, fmt.Errorf("store: marshal encrypted config: %w", err)
	}
	copy := *source
	copy.Config = json.RawMessage(quoted)
	return &copy, nil
}

// decryptSource returns a shallow copy of source with Config decrypted.
func (r *encryptedSourceRepository) decryptSource(source *Source) (*Source, error) {
	if len(source.Config) == 0 {
		return source, nil
	}
	// Config may be stored as a JSON-encoded string (from encryptSource) or as
	// raw JSON bytes (plaintext, backward compat).
	var raw string
	if err := json.Unmarshal(source.Config, &raw); err != nil {
		// Not a JSON string — treat as raw JSON plaintext (backward compat).
		return source, nil
	}
	decrypted, err := crypto.Decrypt(r.key, raw)
	if err != nil {
		return nil, fmt.Errorf("store: decrypt source config: %w", err)
	}
	copy := *source
	copy.Config = json.RawMessage(decrypted)
	return &copy, nil
}

func (r *encryptedSourceRepository) decryptAll(sources []*Source) ([]*Source, error) {
	result := make([]*Source, 0, len(sources))
	for _, s := range sources {
		dec, err := r.decryptSource(s)
		if err != nil {
			return nil, err
		}
		result = append(result, dec)
	}
	return result, nil
}
