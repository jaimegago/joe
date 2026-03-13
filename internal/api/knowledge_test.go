package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

func setupKnowledgeTestServer(t *testing.T) (*http.ServeMux, *knowledge.Service) {
	t.Helper()

	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, nil)

	services := &core.Services{
		Config:    &config.Config{},
		Store:     sqlStore,
		Adapters:  adapters.NewRegistry(),
		Knowledge: knowledgeSvc,
	}

	server := New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux, knowledgeSvc
}

func doRequest(mux *http.ServeMux, method, path string, body any) *httptest.ResponseRecorder {
	var bodyBytes []byte
	if body != nil {
		bodyBytes, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// --- Entry CRUD tests ---

func TestHandleCreateEntry_Success(t *testing.T) {
	mux, _ := setupKnowledgeTestServer(t)

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/entries", map[string]any{
		"title":   "Runbook: restart payments",
		"content": "Run kubectl rollout restart deployment/payments",
		"type":    "runbook",
	})
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp knowledge.Entry
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID == "" {
		t.Error("entry ID should be auto-generated")
	}
	if resp.Title != "Runbook: restart payments" {
		t.Errorf("Title = %q", resp.Title)
	}
}

func TestHandleCreateEntry_DefaultsTierToCurated(t *testing.T) {
	mux, _ := setupKnowledgeTestServer(t)

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/entries", map[string]any{
		"title":   "Best Practice",
		"content": "Always set resource limits.",
		"type":    "best_practice",
		// tier not set → defaults to curated
	})
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}

	var resp knowledge.Entry
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Tier != knowledge.TierCurated {
		t.Errorf("Tier = %q, want %q", resp.Tier, knowledge.TierCurated)
	}
}

func TestHandleCreateEntry_MissingFields(t *testing.T) {
	mux, _ := setupKnowledgeTestServer(t)

	tests := []struct {
		name string
		body map[string]any
	}{
		{"missing title", map[string]any{"content": "content"}},
		{"missing content", map[string]any{"title": "title"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/entries", tt.body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestHandleListEntries_Empty(t *testing.T) {
	mux, _ := setupKnowledgeTestServer(t)

	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/entries", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	entries := resp["entries"].([]any)
	if len(entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(entries))
	}
}

func TestHandleListEntries_WithFilter(t *testing.T) {
	mux, knowledgeSvc := setupKnowledgeTestServer(t)
	ctx := context.Background()

	// Create entries with different tiers.
	if err := knowledgeSvc.Create(ctx, &knowledge.Entry{
		Title: "Curated", Content: "c1", Type: "doc", Tier: knowledge.TierCurated,
	}); err != nil {
		t.Fatalf("create entry: %v", err)
	}

	// List with tier filter.
	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/entries?tier=curated", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	count := resp["count"].(float64)
	if count != 1 {
		t.Errorf("count = %.0f, want 1", count)
	}
}

func TestHandleGetEntry(t *testing.T) {
	mux, knowledgeSvc := setupKnowledgeTestServer(t)
	ctx := context.Background()

	entry := &knowledge.Entry{
		ID: "test-entry-1", Title: "Test", Content: "content", Type: "doc", Tier: knowledge.TierCurated,
	}
	if err := knowledgeSvc.Create(ctx, entry); err != nil {
		t.Fatalf("create entry: %v", err)
	}

	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/entries/test-entry-1", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp knowledge.Entry
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.ID != "test-entry-1" {
		t.Errorf("ID = %q, want %q", resp.ID, "test-entry-1")
	}
}

func TestHandleGetEntry_NotFound(t *testing.T) {
	mux, _ := setupKnowledgeTestServer(t)

	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/entries/nonexistent", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleUpdateEntry_ImmutableTier1(t *testing.T) {
	mux, knowledgeSvc := setupKnowledgeTestServer(t)
	ctx := context.Background()

	// Create a tier 1 (curated) entry.
	entry := &knowledge.Entry{
		ID: "immutable-1", Title: "Immutable", Content: "old", Type: "doc", Tier: knowledge.TierCurated,
	}
	if err := knowledgeSvc.Create(ctx, entry); err != nil {
		t.Fatalf("create entry: %v", err)
	}

	w := doRequest(mux, http.MethodPut, apiPrefix+"/knowledge/entries/immutable-1", map[string]any{
		"title":   "Updated Title",
		"content": "new content",
		"type":    "doc",
		"tier":    knowledge.TierCurated,
	})
	// Tier 1 entries are immutable → 422 Unprocessable Entity
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d (immutable tier 1)", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandleDeleteEntry(t *testing.T) {
	mux, knowledgeSvc := setupKnowledgeTestServer(t)
	ctx := context.Background()

	// Create a tier 2 (synced) entry — tier 1 entries are also undeletable.
	entry := &knowledge.Entry{
		ID: "delete-me", Title: "To Delete", Content: "content", Type: "doc", Tier: knowledge.TierSynced,
	}
	if err := knowledgeSvc.UpsertSynced(ctx, entry); err != nil {
		t.Fatalf("upsert entry: %v", err)
	}

	w := doRequest(mux, http.MethodDelete, apiPrefix+"/knowledge/entries/delete-me", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestHandleSearch_MissingQuery(t *testing.T) {
	mux, _ := setupKnowledgeTestServer(t)

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/search", map[string]any{
		// no "query" field
		"top_k": 5,
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSearch_NoEmbedder(t *testing.T) {
	mux, _ := setupKnowledgeTestServer(t) // knowledge service has no embedder

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/search", map[string]any{
		"query": "how to restart payment service",
		"top_k": 5,
	})
	// Without an embedder, Search() fails → 500 internal error
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d (no embedder configured)", w.Code, http.StatusInternalServerError)
	}
}

// --- Source CRUD tests ---

func TestHandleCreateSource(t *testing.T) {
	mux, _ := setupKnowledgeTestServer(t)

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/sources", map[string]any{
		"name":   "Confluence ENG",
		"type":   "confluence",
		"config": map[string]any{"base_url": "https://company.atlassian.net"},
	})
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp knowledge.KnowledgeSource
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.ID == "" {
		t.Error("source ID should be auto-generated")
	}
	if resp.Name != "Confluence ENG" {
		t.Errorf("Name = %q", resp.Name)
	}
}

func TestHandleCreateSource_MissingFields(t *testing.T) {
	mux, _ := setupKnowledgeTestServer(t)

	tests := []struct {
		name string
		body map[string]any
	}{
		{"missing type", map[string]any{"name": "My Source"}},
		{"missing name", map[string]any{"type": "confluence"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/sources", tt.body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestHandleListSources(t *testing.T) {
	mux, _ := setupKnowledgeTestServer(t)

	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/sources", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["sources"] == nil {
		t.Error("sources field should be present (even if empty)")
	}
}

func TestHandleDeleteSource(t *testing.T) {
	mux, knowledgeSvc := setupKnowledgeTestServer(t)
	ctx := context.Background()

	if err := knowledgeSvc.CreateSource(ctx, &knowledge.KnowledgeSource{
		ID: "src-to-delete", Name: "Del Me", Type: "confluence",
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}

	w := doRequest(mux, http.MethodDelete, apiPrefix+"/knowledge/sources/src-to-delete", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestHandleTriggerSync_Found(t *testing.T) {
	mux, knowledgeSvc := setupKnowledgeTestServer(t)
	ctx := context.Background()

	if err := knowledgeSvc.CreateSource(ctx, &knowledge.KnowledgeSource{
		ID: "sync-src", Name: "Sync Me", Type: "confluence",
	}); err != nil {
		t.Fatalf("create source: %v", err)
	}

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/sources/sync-src/sync", nil)
	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusAccepted, w.Body.String())
	}
}

func TestHandleTriggerSync_NotFound(t *testing.T) {
	mux, _ := setupKnowledgeTestServer(t)

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/sources/nonexistent/sync", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleUpdateEntry_Success(t *testing.T) {
	mux, knowledgeSvc := setupKnowledgeTestServer(t)
	ctx := context.Background()

	// Tier 3 (derived) entries are mutable.
	entry := &knowledge.Entry{
		ID: "update-me", Title: "Original", Content: "old content", Type: "doc", Tier: knowledge.TierDerived,
	}
	if err := knowledgeSvc.Create(ctx, entry); err != nil {
		t.Fatalf("create entry: %v", err)
	}

	w := doRequest(mux, http.MethodPut, apiPrefix+"/knowledge/entries/update-me", map[string]any{
		"title":   "Updated Title",
		"content": "new content",
		"type":    "doc",
		"tier":    knowledge.TierDerived,
	})
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestHandleUpdateEntry_InvalidJSON(t *testing.T) {
	mux, _ := setupKnowledgeTestServer(t)

	req := httptest.NewRequest(http.MethodPut, apiPrefix+"/knowledge/entries/some-id", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDeleteEntry_Tier1_Protected(t *testing.T) {
	mux, knowledgeSvc := setupKnowledgeTestServer(t)
	ctx := context.Background()

	entry := &knowledge.Entry{
		ID: "protect-me", Title: "Protected", Content: "content", Type: "doc", Tier: knowledge.TierCurated,
	}
	if err := knowledgeSvc.Create(ctx, entry); err != nil {
		t.Fatalf("create entry: %v", err)
	}

	w := doRequest(mux, http.MethodDelete, apiPrefix+"/knowledge/entries/protect-me", nil)
	// Tier 1 entries cannot be deleted → 422
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d (tier 1 protected)", w.Code, http.StatusUnprocessableEntity)
	}
}

func TestHandleSearch_Success(t *testing.T) {
	// Build a server with an embedder so search succeeds.
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	embedder := &stubKnowledgeEmbedder{}
	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, embedder)
	mux := http.NewServeMux()
	New(&core.Services{
		Config:    &config.Config{},
		Store:     sqlStore,
		Adapters:  adapters.NewRegistry(),
		Knowledge: knowledgeSvc,
	}).RegisterRoutes(mux)

	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/search", map[string]any{
		"query": "deployment runbook",
		"top_k": 5,
	})
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestHandleSearch_DefaultTopK(t *testing.T) {
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	embedder := &stubKnowledgeEmbedder{}
	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, embedder)
	mux := http.NewServeMux()
	New(&core.Services{
		Config:    &config.Config{},
		Store:     sqlStore,
		Adapters:  adapters.NewRegistry(),
		Knowledge: knowledgeSvc,
	}).RegisterRoutes(mux)

	// top_k=0 → uses default of 5.
	w := doRequest(mux, http.MethodPost, apiPrefix+"/knowledge/search", map[string]any{
		"query": "deployment",
	})
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// setupKnowledgeNoServiceServer creates a server with Knowledge == nil (service unavailable).
func setupKnowledgeNoServiceServer(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	New(&core.Services{
		Config:    &config.Config{},
		Adapters:  adapters.NewRegistry(),
		Knowledge: nil,
	}).RegisterRoutes(mux)
	return mux
}

// TestKnowledgeHandlers_ServiceUnavailable verifies that every knowledge endpoint
// returns 503 when the knowledge service is nil.
func TestKnowledgeHandlers_ServiceUnavailable(t *testing.T) {
	mux := setupKnowledgeNoServiceServer(t)

	cases := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"create entry", http.MethodPost, apiPrefix + "/knowledge/entries", map[string]any{"title": "t", "content": "c"}},
		{"list entries", http.MethodGet, apiPrefix + "/knowledge/entries", nil},
		{"get entry", http.MethodGet, apiPrefix + "/knowledge/entries/x", nil},
		{"update entry", http.MethodPut, apiPrefix + "/knowledge/entries/x", map[string]any{"title": "t", "content": "c"}},
		{"delete entry", http.MethodDelete, apiPrefix + "/knowledge/entries/x", nil},
		{"search", http.MethodPost, apiPrefix + "/knowledge/search", map[string]any{"query": "q"}},
		{"create source", http.MethodPost, apiPrefix + "/knowledge/sources", map[string]any{"type": "confluence", "name": "n"}},
		{"list sources", http.MethodGet, apiPrefix + "/knowledge/sources", nil},
		{"delete source", http.MethodDelete, apiPrefix + "/knowledge/sources/x", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doRequest(mux, tc.method, tc.path, tc.body)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("%s: status = %d, want 503", tc.name, w.Code)
			}
		})
	}
}

// stubKnowledgeEmbedder satisfies knowledge.Embedder with fixed-size embeddings.
type stubKnowledgeEmbedder struct{}

func (s *stubKnowledgeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, 384), nil
}
func (s *stubKnowledgeEmbedder) ModelName() string { return "stub" }
