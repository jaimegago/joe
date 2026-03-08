package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/drift"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// setupDriftTestServer wires DriftDet with a real drift.Detector.
func setupDriftTestServer(t *testing.T) (*http.ServeMux, *knowledge.Service) {
	t.Helper()

	sqlStore, err := store.New(":memory:?_pragma=foreign_keys(1)", nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, nil)
	driftDet := drift.New(knowledgeSvc)

	services := &core.Services{
		Config:    &config.Config{},
		Store:     sqlStore,
		Adapters:  adapters.NewRegistry(),
		Knowledge: knowledgeSvc,
		DriftDet:  driftDet,
	}

	server := New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux, knowledgeSvc
}

// setupNoDriftDetectorServer creates a server with DriftDet intentionally nil.
func setupNoDriftDetectorServer(t *testing.T) *http.ServeMux {
	t.Helper()

	sqlStore, err := store.New(":memory:?_pragma=foreign_keys(1)", nil)
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
		// DriftDet intentionally nil; handlers return 503 when nil.
	}

	server := New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

// TestHandleDetectDrift_NoDriftDetector verifies 503 when DriftDet is not wired.
func TestHandleDetectDrift_NoDriftDetector(t *testing.T) {
	mux := setupNoDriftDetectorServer(t)

	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/drift", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

// TestHandleDetectDrift_Empty verifies 200 with empty reports when no Tier 2 entries exist.
func TestHandleDetectDrift_Empty(t *testing.T) {
	mux, _ := setupDriftTestServer(t)

	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/drift", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	count := resp["count"].(float64)
	if count != 0 {
		t.Errorf("count = %.0f, want 0", count)
	}
}

// TestHandleDetectDrift_SourceTypeFilter verifies source_type query param is forwarded.
func TestHandleDetectDrift_SourceTypeFilter(t *testing.T) {
	mux, _ := setupDriftTestServer(t)

	// No Tier 2 entries → 0 regardless of filter.
	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/drift?source_type=confluence", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	count := resp["count"].(float64)
	if count != 0 {
		t.Errorf("count = %.0f, want 0", count)
	}
}

// TestHandleDetectDrift_WithSyncedEntryNoSource verifies that Tier 2 entries whose
// external fetch fails are silently skipped, yielding an empty drift report list.
func TestHandleDetectDrift_WithSyncedEntryNoSource(t *testing.T) {
	mux, knowledgeSvc := setupDriftTestServer(t)

	// Seed a Tier 2 confluence entry. No matching source is configured,
	// so fetchExternal will fail and DetectAll will skip it with a warning.
	entry := &knowledge.Entry{
		ID:         "drift-synced-1",
		Type:       knowledge.EntryTypeDoc,
		Title:      "Runbook",
		Content:    "content",
		SourceType: knowledge.SourceTypeConfluence,
		SourceID:   "page-1",
		Confidence: 1.0,
	}
	if err := knowledgeSvc.UpsertSynced(context.Background(), entry); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/drift", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	count := resp["count"].(float64)
	if count != 0 {
		t.Errorf("count = %.0f, want 0 (entries with fetch failures are skipped)", count)
	}
}

// TestHandleDetectDriftByEntry_NoDriftDetector verifies 503 when DriftDet is nil.
func TestHandleDetectDriftByEntry_NoDriftDetector(t *testing.T) {
	mux := setupNoDriftDetectorServer(t)

	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/drift/some-id", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

// TestHandleDetectDriftByEntry_NotFound verifies 404 for a nonexistent entry.
func TestHandleDetectDriftByEntry_NotFound(t *testing.T) {
	mux, _ := setupDriftTestServer(t)

	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/drift/does-not-exist", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestHandleDetectDriftByEntry_TierOneEntry verifies 404 for Tier 1 (curated) entries,
// since drift detection only applies to Tier 2 (synced) entries.
func TestHandleDetectDriftByEntry_TierOneEntry(t *testing.T) {
	mux, knowledgeSvc := setupDriftTestServer(t)

	entry := &knowledge.Entry{
		ID:         "curated-entry",
		Tier:       knowledge.TierCurated,
		Type:       knowledge.EntryTypeDoc,
		Title:      "Curated Doc",
		Content:    "important content",
		Confidence: 1.0,
	}
	if err := knowledgeSvc.Create(context.Background(), entry); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	// Detect returns error for non-Tier-2 entries → 404.
	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/drift/curated-entry", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// TestHandleDetectDriftByEntry_TierTwoNoSource verifies 404 for a Tier 2 entry when
// no matching external source is configured (fetchExternal fails → Detect returns error).
func TestHandleDetectDriftByEntry_TierTwoNoSource(t *testing.T) {
	mux, knowledgeSvc := setupDriftTestServer(t)

	entry := &knowledge.Entry{
		ID:         "synced-no-src",
		Type:       knowledge.EntryTypeDoc,
		Title:      "Runbook",
		Content:    "content",
		SourceType: knowledge.SourceTypeConfluence,
		SourceID:   "page-2",
		Confidence: 1.0,
	}
	if err := knowledgeSvc.UpsertSynced(context.Background(), entry); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	// No confluence source → fetchExternal returns error → 404.
	w := doRequest(mux, http.MethodGet, apiPrefix+"/knowledge/drift/synced-no-src", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}
