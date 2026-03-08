package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/observability"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE knowledge_entries (
			id TEXT PRIMARY KEY,
			tier TEXT NOT NULL,
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			embedding BLOB,
			embedding_model TEXT,
			embedding_at TIMESTAMP,
			source_type TEXT,
			source_id TEXT,
			source_url TEXT,
			related_nodes TEXT,
			confidence REAL DEFAULT 1.0,
			created_by TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_synced_at TIMESTAMP
		);
		CREATE TABLE knowledge_sources (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			name TEXT NOT NULL,
			config TEXT NOT NULL,
			status TEXT DEFAULT 'active',
			sync_interval_minutes INTEGER DEFAULT 60,
			last_sync_at TIMESTAMP,
			last_error TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		db.Close()
		t.Fatalf("create tables: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestServiceCRUD(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, observability.EnsureMetrics(nil))
	svc := NewService(repo, nil) // no embedder
	ctx := context.Background()

	e := &Entry{
		Tier:    TierCurated,
		Type:    EntryTypeRunbook,
		Title:   "Payment service restart runbook",
		Content: "1. Check pod status\n2. Restart deployment",
	}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if e.ID == "" {
		t.Error("Create: ID should be set")
	}
	if e.ContentHash == "" {
		t.Error("Create: ContentHash should be set")
	}

	got, err := svc.Get(ctx, e.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != e.Title {
		t.Errorf("Get title = %q, want %q", got.Title, e.Title)
	}
}

func TestTierCuratedImmutable(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, observability.EnsureMetrics(nil))
	svc := NewService(repo, nil)
	ctx := context.Background()

	e := &Entry{
		Tier:    TierCurated,
		Type:    EntryTypeDoc,
		Title:   "Architecture doc",
		Content: "immutable content",
	}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Update should be blocked.
	e.Content = "changed content"
	if err := svc.Update(ctx, e); err == nil {
		t.Error("Update: expected error for Tier 1 entry, got nil")
	}

	// Delete should be blocked.
	if err := svc.Delete(ctx, e.ID); err == nil {
		t.Error("Delete: expected error for Tier 1 entry, got nil")
	}
}

func TestTierDerivedMutable(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, observability.EnsureMetrics(nil))
	svc := NewService(repo, nil)
	ctx := context.Background()

	e := &Entry{
		Tier:    TierDerived,
		Type:    EntryTypeInsight,
		Title:   "Derived insight",
		Content: "original content",
	}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Content = "updated content"
	if err := svc.Update(ctx, e); err != nil {
		t.Errorf("Update: unexpected error for Tier 3: %v", err)
	}

	if err := svc.Delete(ctx, e.ID); err != nil {
		t.Errorf("Delete: unexpected error for Tier 3: %v", err)
	}
}

func TestUpsertSynced(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, observability.EnsureMetrics(nil))
	svc := NewService(repo, nil)
	ctx := context.Background()

	e := &Entry{
		Tier:       TierSynced,
		Type:       EntryTypeDoc,
		Title:      "Confluence page",
		Content:    "original",
		SourceType: SourceTypeConfluence,
		SourceID:   "page-123",
	}
	if err := svc.UpsertSynced(ctx, e); err != nil {
		t.Fatalf("UpsertSynced (create): %v", err)
	}
	id := e.ID

	// Same content → no-op except last_synced_at.
	e2 := &Entry{
		Tier: TierSynced, Type: EntryTypeDoc,
		Title: "Confluence page", Content: "original",
		SourceType: SourceTypeConfluence, SourceID: "page-123",
	}
	if err := svc.UpsertSynced(ctx, e2); err != nil {
		t.Fatalf("UpsertSynced (same content): %v", err)
	}
	if e2.ID != "" && e2.ID != id {
		t.Error("UpsertSynced: same content should not create new entry")
	}

	// Changed content → update.
	e3 := &Entry{
		Tier: TierSynced, Type: EntryTypeDoc,
		Title: "Confluence page", Content: "updated content",
		SourceType: SourceTypeConfluence, SourceID: "page-123",
	}
	if err := svc.UpsertSynced(ctx, e3); err != nil {
		t.Fatalf("UpsertSynced (changed): %v", err)
	}
	got, _ := svc.Get(ctx, id)
	if got != nil && got.Content != "updated content" {
		t.Errorf("UpsertSynced: content not updated, got %q", got.Content)
	}
}

func TestSourceCRUD(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, observability.EnsureMetrics(nil))
	svc := NewService(repo, nil)
	ctx := context.Background()

	src := &KnowledgeSource{
		Type:   "confluence",
		Name:   "Engineering wiki",
		Config: json.RawMessage(`{"base_url":"https://example.atlassian.net"}`),
	}
	if err := svc.CreateSource(ctx, src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if src.ID == "" {
		t.Error("CreateSource: ID should be set")
	}

	sources, err := svc.ListSources(ctx)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("ListSources: got %d, want 1", len(sources))
	}

	if err := svc.DeleteSource(ctx, src.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	sources, _ = svc.ListSources(ctx)
	if len(sources) != 0 {
		t.Errorf("after DeleteSource: got %d sources, want 0", len(sources))
	}
}

func TestListEntriesFilter(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, observability.EnsureMetrics(nil))
	svc := NewService(repo, nil)
	ctx := context.Background()

	for _, e := range []*Entry{
		{Tier: TierCurated, Type: EntryTypeDoc, Title: "Curated 1", Content: "a"},
		{Tier: TierSynced, Type: EntryTypeDoc, Title: "Synced 1", Content: "b"},
		{Tier: TierDerived, Type: EntryTypeInsight, Title: "Derived 1", Content: "c"},
	} {
		if err := svc.Create(ctx, e); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	curated, err := svc.List(ctx, EntryFilter{Tier: TierCurated})
	if err != nil {
		t.Fatalf("List(curated): %v", err)
	}
	if len(curated) != 1 {
		t.Errorf("List(curated): got %d, want 1", len(curated))
	}

	all, _ := svc.List(ctx, EntryFilter{})
	if len(all) != 3 {
		t.Errorf("List(all): got %d, want 3", len(all))
	}
}

func TestHashContent(t *testing.T) {
	h1 := hashContent("hello")
	h2 := hashContent("hello")
	h3 := hashContent("world")
	if h1 != h2 {
		t.Error("same content should produce same hash")
	}
	if h1 == h3 {
		t.Error("different content should produce different hash")
	}
}

func TestUpdateSourceSyncStatus(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db, observability.EnsureMetrics(nil))
	svc := NewService(repo, nil)
	ctx := context.Background()

	src := &KnowledgeSource{
		Type:   "notion",
		Name:   "Notion DB",
		Config: json.RawMessage(`{"api_token":"tok","database_id":"db1"}`),
	}
	if err := svc.CreateSource(ctx, src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := svc.UpdateSourceSyncStatus(ctx, src.ID, now, ""); err != nil {
		t.Fatalf("UpdateSourceSyncStatus: %v", err)
	}

	got, err := svc.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if got.LastSyncAt == nil {
		t.Error("LastSyncAt should be set after UpdateSourceSyncStatus")
	}
}
