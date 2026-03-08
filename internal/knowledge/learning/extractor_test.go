package learning

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/test/mocks"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(":memory:?_pragma=foreign_keys(1)", nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newTestExtractor(t *testing.T, mockLLM *mocks.MockLLM) (*Extractor, *store.Store) {
	t.Helper()
	sqlStore := newTestStore(t)
	knowledgeSvc := knowledge.NewService(sqlStore.Knowledge, nil)
	return New(knowledgeSvc, mockLLM, sqlStore), sqlStore
}

// TestExtractFromSession_NonexistentSession verifies that a session with no
// messages returns nil without error.
func TestExtractFromSession_NonexistentSession(t *testing.T) {
	mockLLM := mocks.NewMockLLM()
	ext, _ := newTestExtractor(t, mockLLM)

	err := ext.ExtractFromSession(context.Background(), "does-not-exist")
	if err != nil {
		t.Errorf("ExtractFromSession() error = %v, want nil", err)
	}
	if mockLLM.CallCount != 0 {
		t.Errorf("LLM.Chat called %d times, want 0 (no messages)", mockLLM.CallCount)
	}
}

// TestExtractFromSession_EmptyMessages verifies no-op when session exists but
// has only tool messages (which are filtered out).
func TestExtractFromSession_EmptyMessages(t *testing.T) {
	mockLLM := mocks.NewMockLLM()
	ext, sqlStore := newTestExtractor(t, mockLLM)
	ctx := context.Background()

	// Create session with only tool-result messages.
	sess := &store.Session{ID: "sess-tool-only"}
	if err := sqlStore.Sessions.Create(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := sqlStore.Sessions.AddMessage(ctx, &store.SessionMessage{
		SessionID: "sess-tool-only",
		Role:      "tool",
		Content:   "tool result here",
		ToolName:  "read_file",
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	err := ext.ExtractFromSession(ctx, "sess-tool-only")
	if err != nil {
		t.Errorf("ExtractFromSession() error = %v, want nil", err)
	}
	// LLM should not be called when transcript is effectively empty.
	if mockLLM.CallCount != 0 {
		t.Errorf("LLM.Chat called %d times, want 0 (only tool messages)", mockLLM.CallCount)
	}
}

// TestExtractFromSession_ValidLearnings verifies that valid LLM output is
// persisted as Tier 3 knowledge entries.
func TestExtractFromSession_ValidLearnings(t *testing.T) {
	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `[{"type":"pattern","title":"High DB latency","description":"Payment service shows high DB latency under load","related_nodes":[],"confidence":0.9}]`,
	}

	ext, sqlStore := newTestExtractor(t, mockLLM)
	ctx := context.Background()

	sess := &store.Session{ID: "sess-valid"}
	if err := sqlStore.Sessions.Create(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := sqlStore.Sessions.AddMessage(ctx, &store.SessionMessage{
		SessionID: "sess-valid",
		Role:      "user",
		Content:   "why is payment slow?",
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}
	if err := sqlStore.Sessions.AddMessage(ctx, &store.SessionMessage{
		SessionID: "sess-valid",
		Role:      "assistant",
		Content:   "It seems the DB pool is saturated.",
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	err := ext.ExtractFromSession(ctx, "sess-valid")
	if err != nil {
		t.Fatalf("ExtractFromSession() error = %v", err)
	}
	if mockLLM.CallCount != 1 {
		t.Errorf("LLM.Chat called %d times, want 1", mockLLM.CallCount)
	}

	// Verify the entry was upserted into the knowledge store.
	// Note: ExtractFromSession uses UpsertSynced which forces Tier = TierSynced,
	// so entries are stored as TierSynced with SourceType = session.
	svc := knowledge.NewService(sqlStore.Knowledge, nil)
	entries, err := svc.List(ctx, knowledge.EntryFilter{Tier: knowledge.TierSynced})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 synced entry, got %d", len(entries))
	}
	if entries[0].Title != "High DB latency" {
		t.Errorf("entry title = %q, want %q", entries[0].Title, "High DB latency")
	}
	if entries[0].SourceType != knowledge.SourceTypeSession {
		t.Errorf("SourceType = %q, want %q", entries[0].SourceType, knowledge.SourceTypeSession)
	}
}

// TestExtractFromSession_LLMError verifies error propagation from LLM.
func TestExtractFromSession_LLMError(t *testing.T) {
	mockLLM := mocks.NewMockLLM()
	mockLLM.ShouldError = true
	mockLLM.ErrorMessage = "LLM unavailable"

	ext, sqlStore := newTestExtractor(t, mockLLM)
	ctx := context.Background()

	sess := &store.Session{ID: "sess-llm-err"}
	if err := sqlStore.Sessions.Create(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := sqlStore.Sessions.AddMessage(ctx, &store.SessionMessage{
		SessionID: "sess-llm-err",
		Role:      "user",
		Content:   "help me debug this",
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	err := ext.ExtractFromSession(ctx, "sess-llm-err")
	if err == nil {
		t.Error("ExtractFromSession() expected error, got nil")
	}
}

// TestExtractFromSession_MalformedJSON verifies that malformed LLM JSON
// returns a parse error.
func TestExtractFromSession_MalformedJSON(t *testing.T) {
	mockLLM := mocks.NewMockLLM()
	mockLLM.DefaultResponse = &llm.ChatResponse{
		Content: `not valid json at all`,
	}

	ext, sqlStore := newTestExtractor(t, mockLLM)
	ctx := context.Background()

	sess := &store.Session{ID: "sess-bad-json"}
	if err := sqlStore.Sessions.Create(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := sqlStore.Sessions.AddMessage(ctx, &store.SessionMessage{
		SessionID: "sess-bad-json",
		Role:      "user",
		Content:   "question",
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	err := ext.ExtractFromSession(ctx, "sess-bad-json")
	if err == nil {
		t.Error("ExtractFromSession() expected error for malformed JSON, got nil")
	}
}

// TestSanitizeTitle verifies the sanitizeTitle helper.
func TestSanitizeTitle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"spaces become dashes", "High DB Latency", "high-db-latency"},
		{"slashes become dashes", "payment/service error", "payment-service-error"},
		{"backslashes become dashes", `path\to\thing`, "path-to-thing"},
		{"empty string", "", ""},
		{"already lowercase", "simple-title", "simple-title"},
		{"truncated at 80 chars",
			"this-is-a-very-long-title-that-exceeds-eighty-characters-and-should-be-truncated-here",
			"this-is-a-very-long-title-that-exceeds-eighty-characters-and-should-be-truncated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeTitle(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestBuildTranscript verifies tool messages are filtered out and user/assistant
// messages are included.
func TestBuildTranscript(t *testing.T) {
	msgs := []*store.SessionMessage{
		{Role: "user", Content: "what is wrong?"},
		{Role: "assistant", Content: "Let me check."},
		{Role: "tool", Content: "tool result", ToolName: "graph_query"},
		{Role: "assistant", Content: "The service is down."},
	}

	got := buildTranscript(msgs)

	// Tool messages must be absent.
	if contains(got, "tool result") {
		t.Error("transcript should not contain tool result content")
	}
	// User/assistant messages must be present.
	if !contains(got, "what is wrong?") {
		t.Error("transcript should contain user message")
	}
	if !contains(got, "The service is down.") {
		t.Error("transcript should contain final assistant message")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
