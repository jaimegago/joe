package drafts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/proposals"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/test/mocks"
	_ "modernc.org/sqlite"
)

// mockEmbedder always returns the same unit vector so searches succeed.
type mockEmbedder struct{}

func (m *mockEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}
func (m *mockEmbedder) ModelName() string { return "mock" }

func newTestSetup(t *testing.T, mockLLM *mocks.MockLLM) (*Generator, *proposals.Service, *knowledge.Service) {
	t.Helper()
	sqlStore, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, &mockEmbedder{})
	proposalRepo := proposals.NewRepository(sqlStore.DB(), sqlStore.Driver())
	proposalSvc := proposals.NewService(proposalRepo)
	gen := New(knowledgeSvc, proposalSvc, mockLLM)
	return gen, proposalSvc, knowledgeSvc
}

// TestOrFallback tests the orFallback helper.
func TestOrFallback(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		fallback string
		want     string
	}{
		{"non-empty string", "hello", "default", "hello"},
		{"empty string uses fallback", "", "default", "default"},
		{"whitespace-only uses fallback", "   ", "default", "default"},
		{"fallback itself empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orFallback(tt.s, tt.fallback)
			if got != tt.want {
				t.Errorf("orFallback(%q, %q) = %q, want %q", tt.s, tt.fallback, got, tt.want)
			}
		})
	}
}

// TestComputeDiff tests the computeDiff helper.
func TestComputeDiff(t *testing.T) {
	t.Run("identical content", func(t *testing.T) {
		diff := computeDiff("hello world", "hello world")
		// Identical text produces no insertions or deletions.
		if strings.Contains(diff, "\x1b[32m") || strings.Contains(diff, "\x1b[31m") {
			t.Errorf("identical content diff should have no ANSI colour codes, got: %q", diff)
		}
	})

	t.Run("changed content has diff", func(t *testing.T) {
		diff := computeDiff("old content", "new content")
		if diff == "" {
			t.Error("expected non-empty diff for changed content")
		}
	})

	t.Run("empty original", func(t *testing.T) {
		diff := computeDiff("", "new content")
		if diff == "" {
			t.Error("expected non-empty diff when adding content to empty original")
		}
	})
}

// TestGenerate_LLMError verifies that an LLM failure is propagated.
func TestGenerate_LLMError(t *testing.T) {
	mockLLM := mocks.NewMockLLM()
	mockLLM.ShouldError = true
	mockLLM.ErrorMessage = "LLM unavailable"

	gen, _, _ := newTestSetup(t, mockLLM)

	_, err := gen.Generate(context.Background(), GenerateRequest{
		Topic:      "deployment guide",
		TargetType: proposals.TargetConfluence,
		TargetID:   "page-1",
	})
	if err == nil {
		t.Fatal("Generate() expected error, got nil")
	}
}

// TestGenerate_Success verifies a successful proposal is created and persisted.
func TestGenerate_Success(t *testing.T) {
	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `{"title":"Deployment Guide","content":"# Deployment\nRun kubectl apply."}`,
	}

	gen, proposalSvc, _ := newTestSetup(t, mockLLM)

	p, err := gen.Generate(context.Background(), GenerateRequest{
		Topic:      "deployment",
		TargetType: proposals.TargetNotion,
		TargetID:   "db-123",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if p == nil {
		t.Fatal("Generate() returned nil proposal")
	}
	if p.Title != "Deployment Guide" {
		t.Errorf("Title = %q, want %q", p.Title, "Deployment Guide")
	}
	if p.Status != proposals.StatusPending {
		t.Errorf("Status = %q, want %q", p.Status, proposals.StatusPending)
	}
	if p.TargetType != proposals.TargetNotion {
		t.Errorf("TargetType = %q, want %q", p.TargetType, proposals.TargetNotion)
	}
	if p.ID == "" {
		t.Error("Proposal ID should be auto-generated")
	}

	// Verify the proposal was actually persisted.
	got, err := proposalSvc.Get(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Title != p.Title {
		t.Errorf("persisted title = %q, want %q", got.Title, p.Title)
	}
}

// TestGenerate_TopicFallback verifies that when the LLM returns an empty title,
// the request topic is used as the fallback.
func TestGenerate_TopicFallback(t *testing.T) {
	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `{"title":"","content":"Some content."}`,
	}

	gen, _, _ := newTestSetup(t, mockLLM)

	p, err := gen.Generate(context.Background(), GenerateRequest{
		Topic:      "runbook for payment",
		TargetType: proposals.TargetGit,
		TargetID:   "docs/payment.md",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if p.Title != "runbook for payment" {
		t.Errorf("Title = %q, want topic fallback %q", p.Title, "runbook for payment")
	}
}

// useNotionAPIBase overrides notionAPIBase for the duration of t and restores it after.
func useNotionAPIBase(t *testing.T, base string) {
	t.Helper()
	orig := notionAPIBase
	notionAPIBase = base
	t.Cleanup(func() { notionAPIBase = orig })
}

// TestGenerate_WithKnowledgeEntries verifies the loop body that builds the knowledge
// context from search results executes when entries exist in the store.
func TestGenerate_WithKnowledgeEntries(t *testing.T) {
	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `{"title":"Enriched Doc","content":"Content with knowledge."}`,
	}

	gen, _, knowledgeSvc := newTestSetup(t, mockLLM)

	// Pre-seed a knowledge entry so Search returns results, exercising the loop body.
	entry := &knowledge.Entry{
		Tier:       knowledge.TierCurated,
		Type:       knowledge.EntryTypeDoc,
		Title:      "Deployment runbook",
		Content:    "Run kubectl apply -f manifests/",
		SourceType: "manual",
		Confidence: 1.0,
	}
	if err := knowledgeSvc.Create(context.Background(), entry); err != nil {
		t.Fatalf("Create knowledge entry: %v", err)
	}

	p, err := gen.Generate(context.Background(), GenerateRequest{
		Topic:      "deployment",
		TargetType: proposals.TargetGit,
		TargetID:   "docs/deploy.md",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(p.KnowledgeEntryIDs) == 0 {
		t.Error("expected KnowledgeEntryIDs to be populated when entries match search")
	}
}

// TestGenerate_ConfluenceMixedSources verifies the continue branches in
// fetchCurrentContent when non-matching source types appear before the target.
func TestGenerate_ConfluenceMixedSources(t *testing.T) {
	confluenceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"body":   map[string]any{"storage": map[string]any{"value": "<p>found it</p>"}},
			"_links": map[string]any{"webui": "/wiki/page"},
		})
	}))
	defer confluenceSrv.Close()

	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `{"title":"Mixed","content":"content."}`,
	}

	gen, _, knowledgeSvc := newTestSetup(t, mockLLM)

	// Sources are returned ORDER BY created_at DESC (newest first). To exercise the continue
	// branches, bad sources must be NEWER (created later) so they are iterated before the good one.

	// Good confluence source created FIRST (oldest) → iterated last.
	cfgBytes, _ := json.Marshal(map[string]any{
		"base_url": confluenceSrv.URL, "api_token": "tok", "email": "test@example.com",
	})
	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		Name: "Good Confluence", Type: "confluence", Config: json.RawMessage(cfgBytes),
	}); err != nil {
		t.Fatalf("CreateSource good confluence: %v", err)
	}

	// Confluence source with bad JSON config (newer) → exercises unmarshal `continue`.
	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		Name: "Bad Confluence", Type: "confluence", Config: json.RawMessage(`{invalid`),
	}); err != nil {
		t.Fatalf("CreateSource bad confluence: %v", err)
	}

	// Non-confluence source created last (newest) → iterated first, exercises type `continue`.
	nonMatchCfg, _ := json.Marshal(map[string]any{"host": "localhost"})
	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		Name: "MySQL Source", Type: "mysql", Config: json.RawMessage(nonMatchCfg),
	}); err != nil {
		t.Fatalf("CreateSource mysql: %v", err)
	}

	p, err := gen.Generate(context.Background(), GenerateRequest{
		Topic:      "topic",
		TargetType: proposals.TargetConfluence,
		TargetID:   "page-1",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !strings.Contains(p.CurrentContent, "found it") {
		t.Errorf("CurrentContent = %q, expected fetched Confluence content", p.CurrentContent)
	}
}

// TestGenerate_NotionMixedSources verifies continue branches in fetchCurrentContent
// for the TargetNotion case.
func TestGenerate_NotionMixedSources(t *testing.T) {
	notionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"paragraph": map[string]any{
					"rich_text": []map[string]any{{"plain_text": "notion mixed"}},
				}},
			},
		})
	}))
	defer notionSrv.Close()
	useNotionAPIBase(t, notionSrv.URL)

	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `{"title":"NotionMixed","content":"out."}`,
	}

	gen, _, knowledgeSvc := newTestSetup(t, mockLLM)

	// Sources are returned ORDER BY created_at DESC (newest first). Good source created first
	// (oldest) → iterated last; bad sources created later (newer) → iterated first.

	// Good notion source created FIRST (oldest) → iterated last.
	notionCfg, _ := json.Marshal(map[string]any{"api_token": "tok", "database_id": "db-1"})
	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		Name: "Test Notion", Type: "notion", Config: json.RawMessage(notionCfg),
	}); err != nil {
		t.Fatalf("CreateSource notion: %v", err)
	}

	// Notion source with bad JSON config (newer) → exercises unmarshal `continue`.
	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		Name: "Bad Notion", Type: "notion", Config: json.RawMessage(`{invalid`),
	}); err != nil {
		t.Fatalf("CreateSource bad notion: %v", err)
	}

	// Non-notion source created last (newest) → iterated first, exercises type `continue`.
	nonCfg, _ := json.Marshal(map[string]any{"x": 1})
	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		Name: "Other", Type: "prometheus", Config: json.RawMessage(nonCfg),
	}); err != nil {
		t.Fatalf("CreateSource other: %v", err)
	}

	p, err := gen.Generate(context.Background(), GenerateRequest{
		Topic:      "topic",
		TargetType: proposals.TargetNotion,
		TargetID:   "page-abc",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !strings.Contains(p.CurrentContent, "notion mixed") {
		t.Errorf("CurrentContent = %q, expected Notion content", p.CurrentContent)
	}
}

// TestGenerate_ConfluenceFetchMalformedJSON verifies that a 200 response with
// malformed JSON from Confluence triggers the parse error path (warning, not fatal).
func TestGenerate_ConfluenceFetchMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()

	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{Content: `{"title":"T","content":"c."}`}

	gen, _, knowledgeSvc := newTestSetup(t, mockLLM)

	cfgBytes, _ := json.Marshal(map[string]any{
		"base_url": srv.URL, "api_token": "tok", "email": "e@x.com",
	})
	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		Name: "Conf", Type: "confluence", Config: json.RawMessage(cfgBytes),
	}); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	_, err := gen.Generate(context.Background(), GenerateRequest{
		Topic: "t", TargetType: proposals.TargetConfluence, TargetID: "p1",
	})
	if err != nil {
		t.Fatalf("Generate() should tolerate fetch error, got %v", err)
	}
}

// TestGenerate_NotionFetchMalformedJSON verifies that a 200 response with
// malformed JSON from Notion triggers the parse error path (warning, not fatal).
func TestGenerate_NotionFetchMalformedJSON(t *testing.T) {
	notionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid`))
	}))
	defer notionSrv.Close()
	useNotionAPIBase(t, notionSrv.URL)

	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{Content: `{"title":"T","content":"c."}`}

	gen, _, knowledgeSvc := newTestSetup(t, mockLLM)

	notionCfg, _ := json.Marshal(map[string]any{"api_token": "tok", "database_id": "db-1"})
	if err := knowledgeSvc.CreateSource(context.Background(), &knowledge.KnowledgeSource{
		Name: "Notion", Type: "notion", Config: json.RawMessage(notionCfg),
	}); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	_, err := gen.Generate(context.Background(), GenerateRequest{
		Topic: "t", TargetType: proposals.TargetNotion, TargetID: "p1",
	})
	if err != nil {
		t.Fatalf("Generate() should tolerate fetch error, got %v", err)
	}
}

// TestGenerate_BadLLMJSON verifies that a non-JSON LLM response is treated as an error.
func TestGenerate_BadLLMJSON(t *testing.T) {
	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{Content: `not valid json at all`}

	gen, _, _ := newTestSetup(t, mockLLM)

	_, err := gen.Generate(context.Background(), GenerateRequest{
		Topic:      "test topic",
		TargetType: proposals.TargetGit,
		TargetID:   "docs/test.md",
	})
	if err == nil {
		t.Error("Generate() expected error for invalid LLM JSON, got nil")
	}
}

// TestGenerate_NotionContentFetch verifies that current content is fetched from
// a Notion source and stored in the proposal's CurrentContent field.
func TestGenerate_NotionContentFetch(t *testing.T) {
	notionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"results": []map[string]any{
				{"paragraph": map[string]any{
					"rich_text": []map[string]any{{"plain_text": "Notion page body text"}},
				}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer notionSrv.Close()
	useNotionAPIBase(t, notionSrv.URL)

	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `{"title":"Notion Test","content":"Updated notion content."}`,
	}

	gen, _, knowledgeSvc := newTestSetup(t, mockLLM)

	cfgBytes, _ := json.Marshal(map[string]any{
		"api_token":   "test-token",
		"database_id": "db-notion-1",
	})
	src := &knowledge.KnowledgeSource{
		Name:   "Test Notion",
		Type:   "notion",
		Config: json.RawMessage(cfgBytes),
	}
	if err := knowledgeSvc.CreateSource(context.Background(), src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	p, err := gen.Generate(context.Background(), GenerateRequest{
		Topic:      "notion page topic",
		TargetType: proposals.TargetNotion,
		TargetID:   "page-notion-abc",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !strings.Contains(p.CurrentContent, "Notion page body text") {
		t.Errorf("CurrentContent = %q, want Notion content", p.CurrentContent)
	}
	if p.TargetURL == "" {
		t.Error("TargetURL should be set for Notion target")
	}
}

// TestGenerate_NotionFetchError verifies that a Notion API error during content
// fetch is treated as a warning — Generate still succeeds with empty CurrentContent.
func TestGenerate_NotionFetchError(t *testing.T) {
	notionSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer notionSrv.Close()
	useNotionAPIBase(t, notionSrv.URL)

	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `{"title":"Fallback","content":"content with no current."}`,
	}

	gen, _, knowledgeSvc := newTestSetup(t, mockLLM)

	cfgBytes, _ := json.Marshal(map[string]any{"api_token": "tok", "database_id": "db-1"})
	src := &knowledge.KnowledgeSource{
		Name:   "Test Notion",
		Type:   "notion",
		Config: json.RawMessage(cfgBytes),
	}
	if err := knowledgeSvc.CreateSource(context.Background(), src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	// Should succeed despite Notion fetch failure (fetch errors are warnings only).
	p, err := gen.Generate(context.Background(), GenerateRequest{
		Topic:      "topic",
		TargetType: proposals.TargetNotion,
		TargetID:   "page-err",
	})
	if err != nil {
		t.Fatalf("Generate() should succeed even with Notion fetch error, got %v", err)
	}
	if p.CurrentContent != "" {
		t.Errorf("CurrentContent should be empty when fetch failed, got %q", p.CurrentContent)
	}
}

// TestGenerate_ConfluenceNoMatchingSource verifies that when no confluence source
// is registered, Generate proceeds with empty current content.
func TestGenerate_ConfluenceNoMatchingSource(t *testing.T) {
	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `{"title":"New Doc","content":"fresh content."}`,
	}

	gen, _, _ := newTestSetup(t, mockLLM) // no sources registered

	p, err := gen.Generate(context.Background(), GenerateRequest{
		Topic:      "something",
		TargetType: proposals.TargetConfluence,
		TargetID:   "page-999",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if p.CurrentContent != "" {
		t.Errorf("expected empty CurrentContent when no source found, got %q", p.CurrentContent)
	}
}

// TestGenerate_ConfluenceFetchError verifies that a Confluence API error during
// content fetch is treated as a warning — Generate still succeeds.
func TestGenerate_ConfluenceFetchError(t *testing.T) {
	confluenceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer confluenceSrv.Close()

	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `{"title":"Updated","content":"content."}`,
	}

	gen, _, knowledgeSvc := newTestSetup(t, mockLLM)

	cfgBytes, _ := json.Marshal(map[string]any{
		"base_url":  confluenceSrv.URL,
		"api_token": "tok",
		"email":     "test@example.com",
		"space_key": "ENG",
	})
	src := &knowledge.KnowledgeSource{
		Name:   "Test Confluence",
		Type:   "confluence",
		Config: json.RawMessage(cfgBytes),
	}
	if err := knowledgeSvc.CreateSource(context.Background(), src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	p, err := gen.Generate(context.Background(), GenerateRequest{
		Topic:      "page topic",
		TargetType: proposals.TargetConfluence,
		TargetID:   "page-err",
	})
	if err != nil {
		t.Fatalf("Generate() should succeed even with Confluence fetch error, got %v", err)
	}
	if p.CurrentContent != "" {
		t.Errorf("CurrentContent should be empty when fetch failed, got %q", p.CurrentContent)
	}
}

// TestGenerate_ConfluenceContentFetch verifies that current content is fetched
// from a Confluence source when one is configured.
func TestGenerate_ConfluenceContentFetch(t *testing.T) {
	// Mock Confluence API server.
	var capturedPageID string
	confluenceSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract page ID from path: /wiki/api/v2/pages/{id}
		parts := strings.Split(r.URL.Path, "/")
		capturedPageID = parts[len(parts)-1]
		resp := map[string]any{
			"body": map[string]any{
				"storage": map[string]any{
					"value": "<p>Current page content</p>",
				},
			},
			"_links": map[string]any{
				"webui": "/wiki/spaces/ENG/page-99",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer confluenceSrv.Close()

	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `{"title":"Updated Page","content":"Updated content."}`,
	}

	gen, _, knowledgeSvc := newTestSetup(t, mockLLM)

	// Register a confluence source pointing to the test server.
	cfgBytes, _ := json.Marshal(map[string]any{
		"base_url":  confluenceSrv.URL,
		"api_token": "test-token",
		"email":     "test@example.com",
		"space_key": "ENG",
	})
	src := &knowledge.KnowledgeSource{
		Name:   "Test Confluence",
		Type:   "confluence",
		Config: json.RawMessage(cfgBytes),
	}
	if err := knowledgeSvc.CreateSource(context.Background(), src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}

	p, err := gen.Generate(context.Background(), GenerateRequest{
		Topic:      "page update",
		TargetType: proposals.TargetConfluence,
		TargetID:   "page-99",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if capturedPageID != "page-99" {
		t.Errorf("fetched page ID = %q, want %q", capturedPageID, "page-99")
	}
	if !strings.Contains(p.CurrentContent, "Current page content") {
		t.Errorf("CurrentContent = %q, should contain fetched content", p.CurrentContent)
	}
	if p.TargetURL == "" {
		t.Error("TargetURL should be populated from Confluence source")
	}
}
