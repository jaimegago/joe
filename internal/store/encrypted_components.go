package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/crypto"
)

// encryptedComponentRepository wraps a ComponentRepository and transparently
// encrypts source.Config on write and decrypts it on read.
type encryptedComponentRepository struct {
	inner ComponentRepository
	key   []byte // 32-byte AES-256 key
}

// NewEncryptedComponentRepository wraps repo so that source Config fields are
// encrypted at rest. key must be exactly 32 bytes.
func NewEncryptedComponentRepository(repo ComponentRepository, key []byte) (ComponentRepository, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("store: encryption key must be 32 bytes, got %d", len(key))
	}
	return &encryptedComponentRepository{inner: repo, key: key}, nil
}

func (r *encryptedComponentRepository) Create(ctx context.Context, source *Component) error {
	enc, err := r.encryptComponent(source)
	if err != nil {
		return err
	}
	return r.inner.Create(ctx, enc)
}

func (r *encryptedComponentRepository) CreateTx(ctx context.Context, tx *sql.Tx, source *Component) error {
	enc, err := r.encryptComponent(source)
	if err != nil {
		return err
	}
	return r.inner.CreateTx(ctx, tx, enc)
}

func (r *encryptedComponentRepository) Get(ctx context.Context, id string) (*Component, error) {
	s, err := r.inner.Get(ctx, id)
	if err != nil || s == nil {
		return s, err
	}
	return r.decryptComponent(s)
}

func (r *encryptedComponentRepository) List(ctx context.Context) ([]*Component, error) {
	components, err := r.inner.List(ctx)
	if err != nil {
		return nil, err
	}
	return r.decryptAll(components)
}

func (r *encryptedComponentRepository) ListByType(ctx context.Context, sourceType string) ([]*Component, error) {
	components, err := r.inner.ListByType(ctx, sourceType)
	if err != nil {
		return nil, err
	}
	return r.decryptAll(components)
}

func (r *encryptedComponentRepository) Update(ctx context.Context, source *Component) error {
	enc, err := r.encryptComponent(source)
	if err != nil {
		return err
	}
	return r.inner.Update(ctx, enc)
}

func (r *encryptedComponentRepository) UpdateSyncStatus(ctx context.Context, id string, syncedAt time.Time, lastError string) error {
	return r.inner.UpdateSyncStatus(ctx, id, syncedAt, lastError)
}

func (r *encryptedComponentRepository) Delete(ctx context.Context, id string) error {
	return r.inner.Delete(ctx, id)
}

func (r *encryptedComponentRepository) DeleteTx(ctx context.Context, tx *sql.Tx, id string) error {
	return r.inner.DeleteTx(ctx, tx, id)
}

// encryptComponent returns a shallow copy of source with Config encrypted.
// The original source is not modified.
func (r *encryptedComponentRepository) encryptComponent(source *Component) (*Component, error) {
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

// decryptComponent returns a shallow copy of source with Config decrypted.
func (r *encryptedComponentRepository) decryptComponent(source *Component) (*Component, error) {
	if len(source.Config) == 0 {
		return source, nil
	}
	// Config may be stored as a JSON-encoded string (from encryptComponent) or as
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

func (r *encryptedComponentRepository) decryptAll(components []*Component) ([]*Component, error) {
	result := make([]*Component, 0, len(components))
	for _, s := range components {
		dec, err := r.decryptComponent(s)
		if err != nil {
			return nil, err
		}
		result = append(result, dec)
	}
	return result, nil
}
