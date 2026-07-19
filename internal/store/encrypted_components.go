package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/crypto"
)

// EncryptedComponentRepository wraps a ComponentRepository and transparently
// encrypts source.Config on write and decrypts it on read.
//
// It is exported as a concrete type rather than returned behind
// ComponentRepository because VerifyConfigs — the boot-time proof that the
// loaded key actually reads the stored data — is not part of the repository
// contract and must not be reachable through a type assertion the composition
// root could silently skip.
type EncryptedComponentRepository struct {
	inner ComponentRepository
	key   []byte // 32-byte AES-256 key
}

// NewEncryptedComponentRepository wraps repo so that source Config fields are
// encrypted at rest. key must be exactly 32 bytes.
func NewEncryptedComponentRepository(repo ComponentRepository, key []byte) (*EncryptedComponentRepository, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("store: encryption key must be 32 bytes, got %d", len(key))
	}
	return &EncryptedComponentRepository{inner: repo, key: key}, nil
}

func (r *EncryptedComponentRepository) Create(ctx context.Context, source *Component) error {
	enc, err := r.encryptComponent(source)
	if err != nil {
		return err
	}
	return r.inner.Create(ctx, enc)
}

func (r *EncryptedComponentRepository) CreateTx(ctx context.Context, tx *sql.Tx, source *Component) error {
	enc, err := r.encryptComponent(source)
	if err != nil {
		return err
	}
	return r.inner.CreateTx(ctx, tx, enc)
}

func (r *EncryptedComponentRepository) Get(ctx context.Context, id string) (*Component, error) {
	s, err := r.inner.Get(ctx, id)
	if err != nil || s == nil {
		return s, err
	}
	return r.decryptComponent(s)
}

func (r *EncryptedComponentRepository) List(ctx context.Context) ([]*Component, error) {
	components, err := r.inner.List(ctx)
	if err != nil {
		return nil, err
	}
	return r.decryptAll(components)
}

func (r *EncryptedComponentRepository) ListByType(ctx context.Context, sourceType string) ([]*Component, error) {
	components, err := r.inner.ListByType(ctx, sourceType)
	if err != nil {
		return nil, err
	}
	return r.decryptAll(components)
}

func (r *EncryptedComponentRepository) Update(ctx context.Context, source *Component) error {
	enc, err := r.encryptComponent(source)
	if err != nil {
		return err
	}
	return r.inner.Update(ctx, enc)
}

func (r *EncryptedComponentRepository) UpdateConfigTx(ctx context.Context, tx *sql.Tx, id string, config json.RawMessage) error {
	enc, err := r.encryptConfig(config)
	if err != nil {
		return err
	}
	return r.inner.UpdateConfigTx(ctx, tx, id, enc)
}

func (r *EncryptedComponentRepository) UpdateSyncStatus(ctx context.Context, id string, syncedAt time.Time, lastError string) error {
	return r.inner.UpdateSyncStatus(ctx, id, syncedAt, lastError)
}

func (r *EncryptedComponentRepository) UpdateSyncState(ctx context.Context, id string, syncedAt time.Time, status, lastError string) error {
	return r.inner.UpdateSyncState(ctx, id, syncedAt, status, lastError)
}

func (r *EncryptedComponentRepository) Delete(ctx context.Context, id string) error {
	return r.inner.Delete(ctx, id)
}

func (r *EncryptedComponentRepository) DeleteTx(ctx context.Context, tx *sql.Tx, id string) error {
	return r.inner.DeleteTx(ctx, tx, id)
}

// encryptComponent returns a shallow copy of source with Config encrypted.
// The original source is not modified.
func (r *EncryptedComponentRepository) encryptComponent(source *Component) (*Component, error) {
	enc, err := r.encryptConfig(source.Config)
	if err != nil {
		return nil, err
	}
	copy := *source
	copy.Config = enc
	return &copy, nil
}

// encryptConfig encrypts a raw config blob to the at-rest form (a JSON-encoded
// string so the column type stays TEXT/JSON), the inverse of decryptComponent's
// read path. An empty config is returned unchanged. Shared by the whole-component
// write path (encryptComponent) and the config-only promotion write
// (UpdateConfigTx) so both encrypt identically.
func (r *EncryptedComponentRepository) encryptConfig(config json.RawMessage) (json.RawMessage, error) {
	if len(config) == 0 {
		return config, nil
	}
	encrypted, err := crypto.Encrypt(r.key, config)
	if err != nil {
		return nil, fmt.Errorf("store: encrypt component config: %w", err)
	}
	// Store as a JSON string so the column type remains TEXT/JSON.
	quoted, err := json.Marshal(encrypted)
	if err != nil {
		return nil, fmt.Errorf("store: marshal encrypted config: %w", err)
	}
	return json.RawMessage(quoted), nil
}

// decryptComponent returns a shallow copy of source with Config decrypted.
func (r *EncryptedComponentRepository) decryptComponent(source *Component) (*Component, error) {
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
		if errors.Is(err, crypto.ErrAuthentication) {
			return nil, &ConfigAuthError{ComponentID: source.ID, Err: err}
		}
		return nil, fmt.Errorf("store: decrypt component config: %w", err)
	}
	copy := *source
	copy.Config = json.RawMessage(decrypted)
	return &copy, nil
}

// ConfigAuthError reports that one component's stored config could not be
// authenticated under the loaded key. It names the component because that is the
// only thing an operator can act on: AES-GCM cannot tell a wrong key from an
// altered row, so the recovery path is chosen from WHICH rows failed — all of
// them means the key is wrong, one of them means that row is damaged.
type ConfigAuthError struct {
	ComponentID string
	Err         error
}

func (e *ConfigAuthError) Error() string {
	return fmt.Sprintf("store: component %s: config failed authentication: %v", e.ComponentID, e.Err)
}

func (e *ConfigAuthError) Unwrap() error { return e.Err }

// VerifyConfigs decrypts every stored component config and reports whether the
// loaded key reads all of them. It is the boot-time proof that key and database
// belong together; a caller runs it once, immediately after wiring, and refuses
// to start on failure.
//
// It deliberately does NOT stop at the first failure. The read paths (Get, List)
// return on the first error because a caller of those wants a result, but an
// operator diagnosing a boot refusal needs the whole set: every component
// failing points at a wrong key, one component failing points at a damaged row,
// and those have different recoveries. The returned error therefore enumerates
// the failures, and errors.As reaches the first *ConfigAuthError for callers
// that want to branch on the class.
//
// Only the authentication class is reported. A transient store error (the List
// itself failing) is returned as-is and keeps its existing posture — this
// verification tightens the key/database mismatch case, not database
// availability.
func (r *EncryptedComponentRepository) VerifyConfigs(ctx context.Context) error {
	components, err := r.inner.List(ctx)
	if err != nil {
		return fmt.Errorf("store: list components for config verification: %w", err)
	}
	var failures []error
	for _, c := range components {
		if _, err := r.decryptComponent(c); err != nil {
			var authErr *ConfigAuthError
			if errors.As(err, &authErr) {
				failures = append(failures, authErr)
				continue
			}
			return err
		}
	}
	return errors.Join(failures...)
}

func (r *EncryptedComponentRepository) decryptAll(components []*Component) ([]*Component, error) {
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
