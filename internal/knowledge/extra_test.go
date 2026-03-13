package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/observability"
)

// --- mock embedder that can be made to fail ---

type failingEmbedder struct {
	fail bool
	vecs map[string][]float32
}

func (m *failingEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if m.fail {
		return nil, errors.New("embed failed")
	}
	if v, ok := m.vecs[text]; ok {
		return v, nil
	}
	return []float32{1, 0, 0}, nil
}
func (m *failingEmbedder) ModelName() string { return "test-model" }

// --- EmbedAll tests ---

func TestEmbedAll_NoEmbedder(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	svc := NewService(repo, nil)

	if err := svc.EmbedAll(context.Background()); err == nil {
		t.Error("EmbedAll without embedder should return error")
	}
}

func TestEmbedAll_SkipsAlreadyEmbedded(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	embedder := &failingEmbedder{vecs: map[string][]float32{
		"content a": {1, 0, 0},
	}}
	svc := NewService(repo, embedder)
	ctx := context.Background()

	// Create an entry (will be embedded automatically on create)
	e := &Entry{Tier: TierDerived, Type: EntryTypeInsight, Title: "A", Content: "content a"}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// EmbedAll should skip the already-embedded entry (same model name)
	if err := svc.EmbedAll(ctx); err != nil {
		t.Errorf("EmbedAll: unexpected error: %v", err)
	}
}

func TestEmbedAll_EmbedsMissingEmbeddings(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))

	// Create without embedder so no embeddings are stored
	svcNoEmbed := NewService(repo, nil)
	ctx := context.Background()
	e := &Entry{Tier: TierDerived, Type: EntryTypeInsight, Title: "B", Content: "content b"}
	if err := svcNoEmbed.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Now create a service with embedder and run EmbedAll
	embedder := &failingEmbedder{vecs: map[string][]float32{"content b": {0, 1, 0}}}
	svc := NewService(repo, embedder)
	if err := svc.EmbedAll(ctx); err != nil {
		t.Errorf("EmbedAll: unexpected error: %v", err)
	}

	got, _ := svc.Get(ctx, e.ID)
	if len(got.Embedding) == 0 {
		t.Error("EmbedAll should have attached embedding to previously un-embedded entry")
	}
}

func TestEmbedAll_EmbedError(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	// Store entry without embedder
	svcNoEmbed := NewService(repo, nil)
	ctx := context.Background()
	e := &Entry{Tier: TierDerived, Type: EntryTypeInsight, Title: "C", Content: "content c"}
	if err := svcNoEmbed.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Use an embedder that fails
	embedder := &failingEmbedder{fail: true}
	svc := NewService(repo, embedder)
	err := svc.EmbedAll(ctx)
	if err == nil {
		t.Error("EmbedAll should return error when embedder fails")
	}
}

// --- Service.Update error paths ---

func TestService_Update_GetEntryError(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	svc := NewService(repo, nil)

	// Update with non-existent ID should return error
	e := &Entry{
		ID:      "nonexistent",
		Tier:    TierDerived,
		Type:    EntryTypeInsight,
		Title:   "X",
		Content: "x",
	}
	if err := svc.Update(context.Background(), e); err == nil {
		t.Error("Update of non-existent entry should return error")
	}
}

func TestService_Update_SameContentPreservesEmbedding(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	embedder := &failingEmbedder{vecs: map[string][]float32{"the content": {1, 2, 3}}}
	svc := NewService(repo, embedder)
	ctx := context.Background()

	e := &Entry{Tier: TierDerived, Type: EntryTypeInsight, Title: "Embedded", Content: "the content"}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Update with same content — should preserve existing embedding
	e.Title = "Embedded (renamed)"
	if err := svc.Update(ctx, e); err != nil {
		t.Fatalf("Update (same content): %v", err)
	}

	got, _ := svc.Get(ctx, e.ID)
	if len(got.Embedding) == 0 {
		t.Error("Embedding should be preserved after update with same content")
	}
}

func TestService_Update_ChangedContentReembeds(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	embedder := &failingEmbedder{vecs: map[string][]float32{
		"original": {1, 0, 0},
		"updated":  {0, 1, 0},
	}}
	svc := NewService(repo, embedder)
	ctx := context.Background()

	e := &Entry{Tier: TierDerived, Type: EntryTypeInsight, Title: "Reembed", Content: "original"}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Content = "updated"
	if err := svc.Update(ctx, e); err != nil {
		t.Fatalf("Update (changed content): %v", err)
	}

	got, _ := svc.Get(ctx, e.ID)
	if got.Content != "updated" {
		t.Errorf("Content = %q, want %q", got.Content, "updated")
	}
}

// --- Service.Delete error path ---

func TestService_Delete_GetEntryError(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	svc := NewService(repo, nil)

	if err := svc.Delete(context.Background(), "nonexistent"); err == nil {
		t.Error("Delete of non-existent entry should return error")
	}
}

// --- Repository error paths (closed DB) ---

func newClosedDB(t *testing.T) *sqlRepository {
	t.Helper()
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil)).(*sqlRepository)
	db.Close()
	return repo
}

func TestRepository_CreateEntry_ClosedDB(t *testing.T) {
	repo := newClosedDB(t)
	e := &Entry{ID: "x", Tier: TierDerived, Type: EntryTypeInsight, Title: "T", Content: "c", ContentHash: "h"}
	if err := repo.CreateEntry(context.Background(), e); err == nil {
		t.Error("CreateEntry on closed DB should return error")
	}
}

func TestRepository_UpdateEntry_ClosedDB(t *testing.T) {
	repo := newClosedDB(t)
	e := &Entry{ID: "x", Tier: TierDerived, Type: EntryTypeInsight, Title: "T", Content: "c", ContentHash: "h"}
	if err := repo.UpdateEntry(context.Background(), e); err == nil {
		t.Error("UpdateEntry on closed DB should return error")
	}
}

func TestRepository_DeleteEntry_ClosedDB(t *testing.T) {
	repo := newClosedDB(t)
	if err := repo.DeleteEntry(context.Background(), "x"); err == nil {
		t.Error("DeleteEntry on closed DB should return error")
	}
}

func TestRepository_GetEntry_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	_, err := repo.GetEntry(context.Background(), "nonexistent")
	if err == nil {
		t.Error("GetEntry should return error for unknown ID")
	}
}

func TestRepository_GetSource_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	_, err := repo.GetSource(context.Background(), "nonexistent")
	if err == nil {
		t.Error("GetSource should return error for unknown ID")
	}
}

func TestRepository_CreateSource_ClosedDB(t *testing.T) {
	repo := newClosedDB(t)
	src := &KnowledgeSource{ID: "s1", Type: "confluence", Name: "N", Config: []byte(`{}`)}
	if err := repo.CreateSource(context.Background(), src); err == nil {
		t.Error("CreateSource on closed DB should return error")
	}
}

func TestRepository_UpdateSourceSyncStatus_ClosedDB(t *testing.T) {
	repo := newClosedDB(t)
	if err := repo.UpdateSourceSyncStatus(context.Background(), "x", time.Now(), ""); err == nil {
		t.Error("UpdateSourceSyncStatus on closed DB should return error")
	}
}

func TestRepository_DeleteSource_ClosedDB(t *testing.T) {
	repo := newClosedDB(t)
	if err := repo.DeleteSource(context.Background(), "x"); err == nil {
		t.Error("DeleteSource on closed DB should return error")
	}
}

func TestRepository_QueryEntries_ClosedDB(t *testing.T) {
	repo := newClosedDB(t)
	_, err := repo.ListEntries(context.Background(), EntryFilter{})
	if err == nil {
		t.Error("ListEntries on closed DB should return error")
	}
}

// --- encodeStringSlice and decodeEmbedding ---

func TestEncodeStringSlice_NonEmpty(t *testing.T) {
	result, err := encodeStringSlice([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("encodeStringSlice: %v", err)
	}
	if result == "" {
		t.Error("encodeStringSlice should return non-empty JSON for non-empty slice")
	}
}

func TestDecodeEmbedding_Error(t *testing.T) {
	_, err := decodeEmbedding([]byte(`not-valid-json`))
	if err == nil {
		t.Error("decodeEmbedding should return error for invalid JSON")
	}
}

func TestDecodeEmbedding_Valid(t *testing.T) {
	data := []byte(`[1.0, 2.0, 3.0]`)
	v, err := decodeEmbedding(data)
	if err != nil {
		t.Fatalf("decodeEmbedding: %v", err)
	}
	if len(v) != 3 {
		t.Errorf("decodeEmbedding: got %d elements, want 3", len(v))
	}
}

// --- Search additional paths ---

func TestSearch_MinConfidence(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	embedder := &failingEmbedder{vecs: map[string][]float32{
		"query":     {1, 0, 0},
		"high conf": {1, 0, 0},
		"low conf":  {1, 0, 0},
	}}
	svc := NewService(repo, embedder)
	ctx := context.Background()

	highConf := &Entry{Tier: TierDerived, Type: EntryTypeInsight, Title: "High", Content: "high conf", Confidence: 0.9}
	lowConf := &Entry{Tier: TierDerived, Type: EntryTypeInsight, Title: "Low", Content: "low conf", Confidence: 0.1}
	for _, e := range []*Entry{highConf, lowConf} {
		if err := svc.Create(ctx, e); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	results, err := svc.Search(ctx, SearchRequest{Query: "query", TopK: 10, MinConfidence: 0.5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Entry.Confidence < 0.5 {
			t.Errorf("Search returned entry with confidence %v < MinConfidence 0.5", r.Entry.Confidence)
		}
	}
}

func TestSearch_EmbedQueryError(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	embedder := &failingEmbedder{fail: true}
	svc := NewService(repo, embedder)

	_, err := svc.Search(context.Background(), SearchRequest{Query: "anything"})
	if err == nil {
		t.Error("Search should return error when embedder fails")
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	embedder := &failingEmbedder{}
	svc := NewService(repo, embedder)
	ctx := context.Background()

	// No entries — should return empty results without error
	results, err := svc.Search(ctx, SearchRequest{Query: "something", TopK: 5})
	if err != nil {
		t.Fatalf("Search on empty store: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Search on empty store: got %d results, want 0", len(results))
	}
}

func TestSearch_TopK_Truncates(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	embedder := &failingEmbedder{}
	svc := NewService(repo, embedder)
	ctx := context.Background()

	// Create 5 entries
	for i := 0; i < 5; i++ {
		e := &Entry{
			Tier: TierDerived, Type: EntryTypeInsight,
			Title:   "entry",
			Content: "doc",
		}
		if err := svc.Create(ctx, e); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	results, err := svc.Search(ctx, SearchRequest{Query: "doc", TopK: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) > 2 {
		t.Errorf("Search: got %d results, want at most 2 (TopK=2)", len(results))
	}
}

// TestService_Create_EmbedFail verifies that embed failure is non-fatal.
func TestService_Create_EmbedFail(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	embedder := &failingEmbedder{fail: true}
	svc := NewService(repo, embedder)
	ctx := context.Background()

	e := &Entry{Tier: TierDerived, Type: EntryTypeInsight, Title: "No Embed", Content: "content"}
	if err := svc.Create(ctx, e); err != nil {
		t.Errorf("Create: embedding failure should be non-fatal, got error: %v", err)
	}
}

// TestEncodeEmbedding_Empty verifies nil return for empty slice.
func TestEncodeEmbedding_Empty(t *testing.T) {
	b, err := encodeEmbedding(nil)
	if err != nil {
		t.Fatalf("encodeEmbedding(nil): %v", err)
	}
	if b != nil {
		t.Error("encodeEmbedding(nil) should return nil")
	}

	b2, err := encodeEmbedding([]float32{})
	if err != nil {
		t.Fatalf("encodeEmbedding([]): %v", err)
	}
	if b2 != nil {
		t.Error("encodeEmbedding([]) should return nil")
	}
}

// TestEncodeEmbedding_NonEmpty verifies JSON is returned for non-empty slice.
func TestEncodeEmbedding_NonEmpty(t *testing.T) {
	b, err := encodeEmbedding([]float32{1.0, 2.0})
	if err != nil {
		t.Fatalf("encodeEmbedding: %v", err)
	}
	if len(b) == 0 {
		t.Error("encodeEmbedding should return non-empty bytes for non-empty slice")
	}
}

// TestListEntriesFilter_SourceID exercises the SourceID filter path.
func TestListEntriesFilter_SourceID(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	svc := NewService(repo, nil)
	ctx := context.Background()

	entries := []*Entry{
		{Tier: TierSynced, Type: EntryTypeDoc, Title: "SrcA", Content: "a",
			SourceType: SourceTypeConfluence, SourceID: "src-1"},
		{Tier: TierSynced, Type: EntryTypeDoc, Title: "SrcB", Content: "b",
			SourceType: SourceTypeConfluence, SourceID: "src-2"},
	}
	for _, e := range entries {
		if err := svc.Create(ctx, e); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	results, err := svc.List(ctx, EntryFilter{SourceID: "src-1"})
	if err != nil {
		t.Fatalf("List(SourceID): %v", err)
	}
	if len(results) != 1 || results[0].SourceID != "src-1" {
		t.Errorf("List(SourceID=src-1): got %d results", len(results))
	}
}

// TestScanEntry_WithEmbedding exercises the scanEntry path that decodes embeddings.
func TestScanEntry_WithEmbedding(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, "sqlite", observability.EnsureMetrics(nil))
	embedder := &failingEmbedder{vecs: map[string][]float32{"embedded content": {1, 2, 3}}}
	svc := NewService(repo, embedder)
	ctx := context.Background()

	e := &Entry{Tier: TierDerived, Type: EntryTypeInsight, Title: "Emb", Content: "embedded content"}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := svc.Get(ctx, e.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Embedding) == 0 {
		t.Error("expected embedding to be stored and retrieved")
	}
}
