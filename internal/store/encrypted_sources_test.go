package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/crypto"
)

// mockSourceRepo is a simple in-memory SourceRepository for testing.
type mockSourceRepo struct {
	sources map[string]*Source
}

func newMockRepo() *mockSourceRepo {
	return &mockSourceRepo{sources: make(map[string]*Source)}
}

func (m *mockSourceRepo) Create(_ context.Context, s *Source) error {
	cp := *s
	m.sources[s.ID] = &cp
	return nil
}

func (m *mockSourceRepo) Get(_ context.Context, id string) (*Source, error) {
	s, ok := m.sources[id]
	if !ok {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}

func (m *mockSourceRepo) List(_ context.Context) ([]*Source, error) {
	out := make([]*Source, 0, len(m.sources))
	for _, s := range m.sources {
		cp := *s
		out = append(out, &cp)
	}
	return out, nil
}

func (m *mockSourceRepo) ListByType(_ context.Context, sourceType string) ([]*Source, error) {
	var out []*Source
	for _, s := range m.sources {
		if s.Type == sourceType {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *mockSourceRepo) Update(_ context.Context, s *Source) error {
	cp := *s
	m.sources[s.ID] = &cp
	return nil
}

func (m *mockSourceRepo) UpdateSyncStatus(_ context.Context, id string, syncedAt time.Time, lastError string) error {
	if s, ok := m.sources[id]; ok {
		s.LastSyncAt = &syncedAt
		s.LastError = lastError
	}
	return nil
}

func (m *mockSourceRepo) Delete(_ context.Context, id string) error {
	delete(m.sources, id)
	return nil
}

func testKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func TestEncryptedSourceRepository_RoundTrip(t *testing.T) {
	key := testKey()
	repo, err := NewEncryptedSourceRepository(newMockRepo(), key)
	if err != nil {
		t.Fatal(err)
	}

	config := json.RawMessage(`{"url":"http://prometheus:9090","api_key":"super-secret"}`)
	src := &Source{
		ID:     "src-1",
		Type:   "prometheus",
		Name:   "prod-prometheus",
		Config: config,
		Status: "active",
	}

	ctx := context.Background()
	if err := repo.Create(ctx, src); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.Get(ctx, "src-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil")
	}
	if string(got.Config) != string(config) {
		t.Errorf("Config after round-trip = %s, want %s", got.Config, config)
	}
}

func TestEncryptedSourceRepository_StoredValueIsEncrypted(t *testing.T) {
	key := testKey()
	inner := newMockRepo()
	repo, err := NewEncryptedSourceRepository(inner, key)
	if err != nil {
		t.Fatal(err)
	}

	config := json.RawMessage(`{"api_key":"super-secret"}`)
	src := &Source{ID: "src-2", Type: "prometheus", Name: "test", Config: config}

	ctx := context.Background()
	if err := repo.Create(ctx, src); err != nil {
		t.Fatal(err)
	}

	// Read directly from inner (unencrypted layer) — config should NOT be plaintext.
	raw, err := inner.Get(ctx, "src-2")
	if err != nil || raw == nil {
		t.Fatal("inner.Get failed")
	}

	var storedStr string
	if err := json.Unmarshal(raw.Config, &storedStr); err != nil {
		t.Fatalf("stored value is not a JSON string: %v", err)
	}
	if !crypto.IsEncrypted(storedStr) {
		t.Error("stored config is not encrypted")
	}
	if string(raw.Config) == string(config) {
		t.Error("stored config equals plaintext — not encrypted")
	}
}

func TestEncryptedSourceRepository_BackwardCompatPlaintext(t *testing.T) {
	key := testKey()
	inner := newMockRepo()

	// Write a plaintext source directly to the inner repo (simulates old data).
	plainCfg := json.RawMessage(`{"url":"http://prometheus:9090"}`)
	inner.sources["legacy"] = &Source{
		ID:     "legacy",
		Type:   "prometheus",
		Name:   "legacy",
		Config: plainCfg,
	}

	repo, err := NewEncryptedSourceRepository(inner, key)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	got, err := repo.Get(ctx, "legacy")
	if err != nil {
		t.Fatalf("Get() error for legacy plaintext = %v", err)
	}
	if got == nil {
		t.Fatal("Get() returned nil for legacy")
	}
	// Plaintext JSON object should be returned as-is.
	if string(got.Config) != string(plainCfg) {
		t.Errorf("legacy config = %s, want %s", got.Config, plainCfg)
	}
}

func TestEncryptedSourceRepository_InvalidKey(t *testing.T) {
	_, err := NewEncryptedSourceRepository(newMockRepo(), []byte("tooshort"))
	if err == nil {
		t.Error("expected error for short key, got nil")
	}
}

func TestEncryptedSourceRepository_List(t *testing.T) {
	key := testKey()
	repo, err := NewEncryptedSourceRepository(newMockRepo(), key)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		s := &Source{
			ID:     string(rune('a' + i)),
			Type:   "prometheus",
			Name:   "source",
			Config: json.RawMessage(`{"api_key":"secret"}`),
		}
		if err := repo.Create(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(sources) != 3 {
		t.Errorf("List() count = %d, want 3", len(sources))
	}
	for _, s := range sources {
		if string(s.Config) != `{"api_key":"secret"}` {
			t.Errorf("decrypted config = %s, want plaintext", s.Config)
		}
	}
}

func TestEncryptedSourceRepository_Update(t *testing.T) {
	key := testKey()
	repo, err := NewEncryptedSourceRepository(newMockRepo(), key)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	src := &Source{ID: "s1", Type: "prometheus", Name: "p", Config: json.RawMessage(`{"url":"old"}`)}
	if err := repo.Create(ctx, src); err != nil {
		t.Fatal(err)
	}

	src.Config = json.RawMessage(`{"url":"new"}`)
	if err := repo.Update(ctx, src); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := repo.Get(ctx, "s1")
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if string(got.Config) != `{"url":"new"}` {
		t.Errorf("after update config = %s, want {\"url\":\"new\"}", got.Config)
	}
}
