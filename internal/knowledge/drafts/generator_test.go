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
	sqlStore, err := store.New(":memory:?_pragma=foreign_keys(1)", nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := sqlStore.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { sqlStore.Close() })

	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, &mockEmbedder{})
	proposalRepo := proposals.NewRepository(sqlStore.DB())
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
