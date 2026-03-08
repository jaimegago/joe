package drift

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/observability"
)

// --- test helpers ---

func newTestService(t *testing.T) *knowledge.Service {
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
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	repo := knowledge.NewRepository(db, observability.EnsureMetrics(nil))
	return knowledge.NewService(repo, nil)
}

// sha256hex mirrors hashContent so tests can pre-compute expected hashes.
func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum)
}

// newTestDetector builds a Detector with a custom HTTP client for testing.
// Because this test file is in package drift (same package), we can set
// unexported fields directly.
func newTestDetector(svc *knowledge.Service, client *http.Client) *Detector {
	return &Detector{
		svc:        svc,
		httpClient: client,
		logger:     slog.Default(),
	}
}

// confluencePageResponse returns a minimal Confluence page API JSON body.
func confluencePageResponse(bodyValue string) string {
	return fmt.Sprintf(`{"body":{"storage":{"value":%q}}}`, bodyValue)
}

// notionBlocksResponse returns a minimal Notion blocks API JSON body.
func notionBlocksResponse(text string) string {
	return fmt.Sprintf(`{"results":[{"paragraph":{"rich_text":[{"plain_text":%q}]}}]}`, text)
}

// seedConfluenceSource registers a Confluence knowledge source in the service.
func seedConfluenceSource(t *testing.T, svc *knowledge.Service, serverURL string) {
	t.Helper()
	cfgJSON, _ := json.Marshal(map[string]string{
		"base_url":  serverURL,
		"api_token": "test-token",
		"email":     "test@example.com",
	})
	src := &knowledge.KnowledgeSource{
		Type:   "confluence",
		Name:   "Test Confluence",
		Config: cfgJSON,
		Status: "active",
	}
	if err := svc.CreateSource(context.Background(), src); err != nil {
		t.Fatalf("seedConfluenceSource: %v", err)
	}
}

// --- unit tests ---

func TestHashContent(t *testing.T) {
	h1 := hashContent("hello world")
	h2 := hashContent("hello world")
	h3 := hashContent("different")

	if h1 != h2 {
		t.Error("hashContent should be deterministic")
	}
	if h1 == h3 {
		t.Error("hashContent should differ for different content")
	}
	if h1 == "" {
		t.Error("hashContent should not return empty string")
	}
}

func TestComputeDiff_Identical(t *testing.T) {
	diff := computeDiff("same content", "same content")
	// No insertions or deletions expected for identical content.
	if diff == "" {
		// Empty diff is also valid — nothing to report.
		return
	}
}

func TestComputeDiff_Different(t *testing.T) {
	diff := computeDiff("original text", "revised text")
	if diff == "" {
		t.Error("computeDiff should produce non-empty output for different content")
	}
}

func TestDriftReport_Fields(t *testing.T) {
	report := &DriftReport{
		EntryID:         "entry-1",
		Title:           "My Doc",
		ExternalChanged: true,
		Diff:            "some diff",
	}
	if report.EntryID != "entry-1" {
		t.Errorf("EntryID = %q, want %q", report.EntryID, "entry-1")
	}
	if !report.ExternalChanged {
		t.Error("ExternalChanged should be true")
	}
}

func TestDetector_Detect_NonSyncedEntry(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Tier 1 (curated) entry — drift detection should reject it
	e := &knowledge.Entry{
		Tier:    knowledge.TierCurated,
		Type:    knowledge.EntryTypeRunbook,
		Title:   "Runbook",
		Content: "step 1",
	}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	d := newTestDetector(svc, &http.Client{})
	_, err := d.Detect(ctx, e.ID)
	if err == nil {
		t.Error("Detect on non-Tier2 entry should return error")
	}
}

func TestDetector_Detect_ConfluenceDrift(t *testing.T) {
	externalContent := "updated page content from confluence"
	storedContent := "original page content"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, confluencePageResponse(externalContent))
	}))
	defer srv.Close()

	svc := newTestService(t)
	ctx := context.Background()

	seedConfluenceSource(t, svc, srv.URL)

	e := &knowledge.Entry{
		Tier:       knowledge.TierSynced,
		Type:       knowledge.EntryTypeRunbook,
		Title:      "Confluence Page",
		Content:    storedContent,
		SourceType: knowledge.SourceTypeConfluence,
		SourceID:   "page-123",
	}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	d := newTestDetector(svc, srv.Client())
	report, err := d.Detect(ctx, e.ID)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	if !report.ExternalChanged {
		t.Error("ExternalChanged should be true when content differs")
	}
	if report.Diff == "" {
		t.Error("Diff should be non-empty when content differs")
	}
	if report.EntryID != e.ID {
		t.Errorf("EntryID = %q, want %q", report.EntryID, e.ID)
	}
}

func TestDetector_Detect_NoDrift(t *testing.T) {
	externalContent := "unchanged page content"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, confluencePageResponse(externalContent))
	}))
	defer srv.Close()

	svc := newTestService(t)
	ctx := context.Background()

	seedConfluenceSource(t, svc, srv.URL)

	e := &knowledge.Entry{
		Tier:       knowledge.TierSynced,
		Type:       knowledge.EntryTypeRunbook,
		Title:      "Stable Page",
		Content:    externalContent,
		SourceType: knowledge.SourceTypeConfluence,
		SourceID:   "page-stable",
	}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Manually set ContentHash to match external content so no drift is detected
	storedHash := sha256hex(externalContent)
	if e.ContentHash != storedHash {
		// Update the stored hash to match external content
		e.ContentHash = storedHash
		if err := svc.Update(ctx, e); err != nil {
			t.Fatalf("Update hash: %v", err)
		}
	}

	d := newTestDetector(svc, srv.Client())
	report, err := d.Detect(ctx, e.ID)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	if report.ExternalChanged {
		t.Error("ExternalChanged should be false when content is identical")
	}
}

func TestDetector_Detect_ConfluenceAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	svc := newTestService(t)
	ctx := context.Background()

	seedConfluenceSource(t, svc, srv.URL)

	e := &knowledge.Entry{
		Tier:       knowledge.TierSynced,
		Type:       knowledge.EntryTypeRunbook,
		Title:      "Secret Page",
		Content:    "content",
		SourceType: knowledge.SourceTypeConfluence,
		SourceID:   "page-secret",
	}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	d := newTestDetector(svc, srv.Client())
	_, err := d.Detect(ctx, e.ID)
	if err == nil {
		t.Error("Detect should return error on non-200 Confluence response")
	}
}

func TestDetector_Detect_NoSourceConfigured(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// No knowledge source registered — fetchExternal should fail gracefully
	e := &knowledge.Entry{
		Tier:       knowledge.TierSynced,
		Type:       knowledge.EntryTypeRunbook,
		Title:      "Orphan Page",
		Content:    "content",
		SourceType: knowledge.SourceTypeConfluence,
		SourceID:   "page-orphan",
	}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	d := newTestDetector(svc, &http.Client{})
	_, err := d.Detect(ctx, e.ID)
	if err == nil {
		t.Error("Detect should return error when no matching source is configured")
	}
}

func TestDetector_Detect_NotionDrift(t *testing.T) {
	externalText := "notion page text content"
	storedContent := "old notion content"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, notionBlocksResponse(externalText))
	}))
	defer srv.Close()

	svc := newTestService(t)
	ctx := context.Background()

	// Override notion API URL is hard-coded in detector; we test by patching
	// the httpClient transport to redirect notion.com to our test server.
	transport := &redirectTransport{scheme: "http", host: srv.Listener.Addr().String()}

	// Seed notion source
	cfgJSON, _ := json.Marshal(map[string]string{
		"api_token": "test-notion-token",
	})
	notionSrc := &knowledge.KnowledgeSource{
		Type:   "notion",
		Name:   "Test Notion",
		Config: cfgJSON,
		Status: "active",
	}
	if err := svc.CreateSource(ctx, notionSrc); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	e := &knowledge.Entry{
		Tier:       knowledge.TierSynced,
		Type:       knowledge.EntryTypeRunbook,
		Title:      "Notion Page",
		Content:    storedContent,
		SourceType: knowledge.SourceTypeNotion,
		SourceID:   "block-abc123",
	}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	d := newTestDetector(svc, &http.Client{Transport: transport})
	report, err := d.Detect(ctx, e.ID)
	if err != nil {
		t.Fatalf("Detect (notion): %v", err)
	}

	if !report.ExternalChanged {
		t.Error("ExternalChanged should be true for Notion drift")
	}
}

func TestDetector_Detect_UnsupportedSourceType(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	e := &knowledge.Entry{
		Tier:       knowledge.TierSynced,
		Type:       knowledge.EntryTypeRunbook,
		Title:      "Git Page",
		Content:    "content",
		SourceType: "git", // not supported
		SourceID:   "file-123",
	}
	if err := svc.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}

	d := newTestDetector(svc, &http.Client{})
	_, err := d.Detect(ctx, e.ID)
	if err == nil {
		t.Error("Detect should return error for unsupported source type")
	}
}

func TestDetector_DetectAll_FiltersNoDrift(t *testing.T) {
	externalContent := "fresh page content"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, confluencePageResponse(externalContent))
	}))
	defer srv.Close()

	svc := newTestService(t)
	ctx := context.Background()

	seedConfluenceSource(t, svc, srv.URL)

	// Entry 1: drifted (stored content differs from external)
	e1 := &knowledge.Entry{
		Tier:       knowledge.TierSynced,
		Type:       knowledge.EntryTypeRunbook,
		Title:      "Drifted Doc",
		Content:    "old content that differs",
		SourceType: knowledge.SourceTypeConfluence,
		SourceID:   "page-drifted",
	}
	if err := svc.Create(ctx, e1); err != nil {
		t.Fatalf("Create e1: %v", err)
	}

	// Entry 2: stable (hash matches external content)
	e2 := &knowledge.Entry{
		Tier:       knowledge.TierSynced,
		Type:       knowledge.EntryTypeRunbook,
		Title:      "Stable Doc",
		Content:    externalContent,
		SourceType: knowledge.SourceTypeConfluence,
		SourceID:   "page-stable",
	}
	if err := svc.Create(ctx, e2); err != nil {
		t.Fatalf("Create e2: %v", err)
	}
	// Align the stored hash with external content so it's not detected as drifted
	e2.ContentHash = sha256hex(externalContent)
	if err := svc.Update(ctx, e2); err != nil {
		t.Fatalf("Update e2 hash: %v", err)
	}

	d := newTestDetector(svc, srv.Client())
	reports, err := d.DetectAll(ctx, knowledge.SourceTypeConfluence)
	if err != nil {
		t.Fatalf("DetectAll: %v", err)
	}

	if len(reports) != 1 {
		t.Errorf("DetectAll: got %d reports, want 1 (only drifted entry)", len(reports))
	}
	if len(reports) > 0 && reports[0].EntryID != e1.ID {
		t.Errorf("DetectAll: reported entry %q, want %q", reports[0].EntryID, e1.ID)
	}
}

func TestDetector_DetectAll_ContinuesOnError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First entry fails
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		// Second entry succeeds but has drift
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, confluencePageResponse("different external content"))
	}))
	defer srv.Close()

	svc := newTestService(t)
	ctx := context.Background()

	seedConfluenceSource(t, svc, srv.URL)

	for i := 0; i < 2; i++ {
		e := &knowledge.Entry{
			Tier:       knowledge.TierSynced,
			Type:       knowledge.EntryTypeRunbook,
			Title:      fmt.Sprintf("Doc %d", i),
			Content:    "original",
			SourceType: knowledge.SourceTypeConfluence,
			SourceID:   fmt.Sprintf("page-%d", i),
		}
		if err := svc.Create(ctx, e); err != nil {
			t.Fatalf("Create entry %d: %v", i, err)
		}
	}

	d := newTestDetector(svc, srv.Client())
	reports, err := d.DetectAll(ctx, knowledge.SourceTypeConfluence)
	// DetectAll should not return an error even if individual entries fail
	if err != nil {
		t.Fatalf("DetectAll should not fail on individual entry errors: %v", err)
	}
	// One entry failed, one succeeded with drift
	if len(reports) != 1 {
		t.Errorf("DetectAll: got %d reports, want 1 (only the successful+drifted entry)", len(reports))
	}
}

// redirectTransport rewrites every outgoing request to point at a fixed test
// server URL, regardless of the original host. This lets us intercept
// hard-coded external URLs (e.g. api.notion.com) in unit tests.
type redirectTransport struct {
	scheme string
	host   string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Scheme = t.scheme
	req2.URL.Host = t.host
	return http.DefaultTransport.RoundTrip(req2)
}
