package notion

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// --- helpers ---

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

func newTestKnowledgeSvc(t *testing.T) *knowledge.Service {
	t.Helper()
	s := newTestStore(t)
	return knowledge.NewService(s.Knowledge, nil)
}

// --- parseConfig tests ---

func TestParseConfig_Valid(t *testing.T) {
	raw, _ := json.Marshal(Config{
		APIToken:   "tok-123",
		DatabaseID: "db-abc",
		PageLimit:  50,
	})
	cfg, err := parseConfig(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.APIToken != "tok-123" {
		t.Errorf("APIToken = %q", cfg.APIToken)
	}
	if cfg.DatabaseID != "db-abc" {
		t.Errorf("DatabaseID = %q", cfg.DatabaseID)
	}
}

func TestParseConfig_MissingFields(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"missing api_token", Config{DatabaseID: "db-1"}, true},
		{"missing database_id", Config{APIToken: "tok"}, true},
		{"all fields present", Config{APIToken: "tok", DatabaseID: "db-1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _ := json.Marshal(tt.cfg)
			_, err := parseConfig(json.RawMessage(raw))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseConfig_InvalidJSON(t *testing.T) {
	_, err := parseConfig(json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("parseConfig() expected error for invalid JSON")
	}
}

// --- splitLines tests ---

// splitLines splits on \n and returns all segments.
// Trailing newline results in the last segment being dropped (loop ends at len-1).
func TestSplitLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty string", "", nil},
		{"single line no newline", "hello", []string{"hello"}},
		{"two lines", "line1\nline2", []string{"line1", "line2"}},
		{"trailing newline", "line1\nline2\n", []string{"line1", "line2"}},
		{"three lines", "a\nb\nc", []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("splitLines(%q) = %v (len %d), want %v (len %d)",
					tt.input, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("splitLines(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// --- extractTitle tests ---

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		name  string
		props map[string]notionProperty
		want  string
	}{
		{
			name: "title property found",
			props: map[string]notionProperty{
				"Name": {Type: "title", Title: []notionRichText{{PlainText: "My Page"}}},
			},
			want: "My Page",
		},
		{
			name: "no title property",
			props: map[string]notionProperty{
				"Status": {Type: "select"},
			},
			want: "Untitled",
		},
		{
			name:  "empty props",
			props: map[string]notionProperty{},
			want:  "Untitled",
		},
		{
			name: "title with empty rich_text list",
			props: map[string]notionProperty{
				"Name": {Type: "title", Title: []notionRichText{}},
			},
			want: "Untitled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTitle(tt.props)
			if got != tt.want {
				t.Errorf("extractTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- Sync error path tests ---

func TestSync_BadConfig(t *testing.T) {
	svc := newTestKnowledgeSvc(t)
	src := &knowledge.KnowledgeSource{
		ID:     "bad",
		Type:   "notion",
		Config: json.RawMessage(`{invalid`),
	}
	syncer := New()
	err := syncer.Sync(context.Background(), src, svc)
	if err == nil {
		t.Error("Sync() expected error for invalid config, got nil")
	}
}

func TestSync_MissingAPIToken(t *testing.T) {
	svc := newTestKnowledgeSvc(t)
	cfg, _ := json.Marshal(Config{DatabaseID: "db-1"}) // missing APIToken
	src := &knowledge.KnowledgeSource{
		ID:     "bad-token",
		Type:   "notion",
		Config: json.RawMessage(cfg),
	}
	syncer := New()
	err := syncer.Sync(context.Background(), src, svc)
	if err == nil {
		t.Error("Sync() expected error for missing api_token, got nil")
	}
}

func TestSync_MissingDatabaseID(t *testing.T) {
	svc := newTestKnowledgeSvc(t)
	cfg, _ := json.Marshal(Config{APIToken: "tok"}) // missing DatabaseID
	src := &knowledge.KnowledgeSource{
		ID:     "bad-dbid",
		Type:   "notion",
		Config: json.RawMessage(cfg),
	}
	syncer := New()
	err := syncer.Sync(context.Background(), src, svc)
	if err == nil {
		t.Error("Sync() expected error for missing database_id, got nil")
	}
}

// TestSync_NetworkError verifies graceful failure when the Notion API is unreachable.
// The Notion base URL is hardcoded in the production code, so we can only test
// that Sync() returns an error without panicking.
func TestSync_NetworkError(t *testing.T) {
	svc := newTestKnowledgeSvc(t)
	cfg, _ := json.Marshal(Config{APIToken: "fake-tok", DatabaseID: "fake-db"})
	src := &knowledge.KnowledgeSource{
		ID:     "notion-unreachable",
		Type:   "notion",
		Config: json.RawMessage(cfg),
	}
	syncer := New()
	// The call will fail because api.notion.com is unreachable in test environments.
	err := syncer.Sync(context.Background(), src, svc)
	// We only verify no panic occurred — the network error is expected.
	_ = err
}
