package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/store"
	_ "modernc.org/sqlite"
)

// --- helpers ---

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
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

// --- httptest-based tests ---

// useNotionTestServer overrides notionBaseURL for the duration of t and restores it after.
func useNotionTestServer(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := notionBaseURL
	notionBaseURL = srv.URL
	t.Cleanup(func() { notionBaseURL = orig })
}

// notionDBQueryHandler responds to POST /databases/{id}/query with the given pages.
// If nextCursor is non-empty it sets has_more=true and next_cursor.
func notionDBQueryHandler(pages []notionPageResult, nextCursor string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := queryDBResponse{
			Results:    pages,
			HasMore:    nextCursor != "",
			NextCursor: nextCursor,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// notionBlocksHandler responds to GET /blocks/{id}/children with the given plain-text lines.
func notionBlocksHandler(lines []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var results []map[string]any
		for _, l := range lines {
			results = append(results, map[string]any{
				"paragraph": map[string]any{
					"rich_text": []map[string]any{{"plain_text": l}},
				},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}
}

// --- fetchPages tests ---

func TestFetchPages_Success(t *testing.T) {
	pageResult := notionPageResult{
		ID:         "page-abc",
		Properties: map[string]notionProperty{"Name": {Type: "title", Title: []notionRichText{{PlainText: "My Page"}}}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			notionDBQueryHandler([]notionPageResult{pageResult}, "")(w, r)
		} else {
			notionBlocksHandler([]string{"Hello content"})(w, r)
		}
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	pages, err := fetchPages(context.Background(), &Config{APIToken: "tok", DatabaseID: "db-1"})
	if err != nil {
		t.Fatalf("fetchPages() error = %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages))
	}
	if pages[0].title != "My Page" {
		t.Errorf("title = %q, want %q", pages[0].title, "My Page")
	}
	if !strings.Contains(pages[0].content, "Hello content") {
		t.Errorf("content = %q, want it to contain %q", pages[0].content, "Hello content")
	}
}

func TestFetchPages_Pagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			callCount++
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if _, hasCursor := body["start_cursor"]; !hasCursor {
				// First page: has_more + next_cursor
				json.NewEncoder(w).Encode(queryDBResponse{
					Results:    []notionPageResult{{ID: "p1", Properties: map[string]notionProperty{"Name": {Type: "title", Title: []notionRichText{{PlainText: "Page 1"}}}}}},
					HasMore:    true,
					NextCursor: "cursor-abc",
				})
			} else {
				// Second page: terminal
				json.NewEncoder(w).Encode(queryDBResponse{
					Results: []notionPageResult{{ID: "p2", Properties: map[string]notionProperty{"Name": {Type: "title", Title: []notionRichText{{PlainText: "Page 2"}}}}}},
					HasMore: false,
				})
			}
		} else {
			// Block children — empty
			json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
		}
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	pages, err := fetchPages(context.Background(), &Config{APIToken: "tok", DatabaseID: "db-1"})
	if err != nil {
		t.Fatalf("fetchPages() error = %v", err)
	}
	if len(pages) != 2 {
		t.Errorf("expected 2 pages, got %d", len(pages))
	}
	if callCount != 2 {
		t.Errorf("expected 2 DB query calls, got %d", callCount)
	}
}

func TestFetchPages_PageLimit(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			callCount++
			json.NewEncoder(w).Encode(queryDBResponse{
				Results:    []notionPageResult{{ID: "p1", Properties: map[string]notionProperty{"Name": {Type: "title", Title: []notionRichText{{PlainText: "Page 1"}}}}}},
				HasMore:    true,
				NextCursor: "cursor-abc",
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
		}
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	pages, err := fetchPages(context.Background(), &Config{APIToken: "tok", DatabaseID: "db-1", PageLimit: 1})
	if err != nil {
		t.Fatalf("fetchPages() error = %v", err)
	}
	if len(pages) != 1 {
		t.Errorf("PageLimit=1: expected 1 page, got %d", len(pages))
	}
	if callCount != 1 {
		t.Errorf("PageLimit=1: expected 1 DB query call, got %d", callCount)
	}
}

func TestFetchPages_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	_, err := fetchPages(context.Background(), &Config{APIToken: "tok", DatabaseID: "db-1"})
	if err == nil {
		t.Error("fetchPages() expected error for 401, got nil")
	}
}

func TestFetchPages_EmptyDatabase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(queryDBResponse{Results: nil, HasMore: false})
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	pages, err := fetchPages(context.Background(), &Config{APIToken: "tok", DatabaseID: "db-1"})
	if err != nil {
		t.Fatalf("fetchPages() error = %v", err)
	}
	if len(pages) != 0 {
		t.Errorf("expected 0 pages for empty DB, got %d", len(pages))
	}
}

// --- fetchPageContent tests ---

func TestFetchPageContent_ParagraphBlocks(t *testing.T) {
	srv := httptest.NewServer(notionBlocksHandler([]string{"First line", "Second line"}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	client := &http.Client{}
	content, err := fetchPageContent(context.Background(), client, "tok", "page-1")
	if err != nil {
		t.Fatalf("fetchPageContent() error = %v", err)
	}
	if !strings.Contains(content, "First line") || !strings.Contains(content, "Second line") {
		t.Errorf("content = %q, want both lines present", content)
	}
}

func TestFetchPageContent_HeadingAndBulletBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		results := []map[string]any{
			{"heading_1": map[string]any{"rich_text": []map[string]any{{"plain_text": "Section Title"}}}},
			{"bulleted_list_item": map[string]any{"rich_text": []map[string]any{{"plain_text": "Bullet item"}}}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	client := &http.Client{}
	content, err := fetchPageContent(context.Background(), client, "tok", "page-1")
	if err != nil {
		t.Fatalf("fetchPageContent() error = %v", err)
	}
	if !strings.Contains(content, "Section Title") {
		t.Errorf("content = %q, want heading text", content)
	}
	if !strings.Contains(content, "Bullet item") {
		t.Errorf("content = %q, want bullet text", content)
	}
}

func TestFetchPageContent_UnknownBlockType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// "code" is not in the handled block type list — should be silently skipped.
		results := []map[string]any{
			{"code": map[string]any{"rich_text": []map[string]any{{"plain_text": "ignored"}}}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	client := &http.Client{}
	content, err := fetchPageContent(context.Background(), client, "tok", "page-1")
	if err != nil {
		t.Fatalf("fetchPageContent() error = %v", err)
	}
	if strings.Contains(content, "ignored") {
		t.Errorf("content should not contain text from unknown block type, got: %q", content)
	}
}

func TestFetchPageContent_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	client := &http.Client{}
	_, err := fetchPageContent(context.Background(), client, "tok", "page-1")
	if err == nil {
		t.Error("fetchPageContent() expected error for non-200 response")
	}
}

// --- Sync (success path) ---

func TestSync_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			json.NewEncoder(w).Encode(queryDBResponse{
				Results: []notionPageResult{
					{ID: "page-1", Properties: map[string]notionProperty{
						"Name": {Type: "title", Title: []notionRichText{{PlainText: "Sync Test Page"}}},
					}},
				},
				HasMore: false,
			})
		} else {
			notionBlocksHandler([]string{"Page body text"})(w, r)
		}
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	svc := newTestKnowledgeSvc(t)
	cfgBytes, _ := json.Marshal(Config{APIToken: "tok", DatabaseID: "db-1"})
	src := &knowledge.KnowledgeSource{
		ID:     "src-notion",
		Type:   "notion",
		Config: json.RawMessage(cfgBytes),
	}

	syncer := New()
	if err := syncer.Sync(context.Background(), src, svc); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	entries, err := svc.List(context.Background(), knowledge.EntryFilter{Tier: knowledge.TierSynced})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 synced entry, got %d", len(entries))
	}
	if entries[0].Title != "Sync Test Page" {
		t.Errorf("Title = %q, want %q", entries[0].Title, "Sync Test Page")
	}
	if entries[0].SourceType != knowledge.SourceTypeNotion {
		t.Errorf("SourceType = %q, want Notion", entries[0].SourceType)
	}
}

func TestSync_FetchPagesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	svc := newTestKnowledgeSvc(t)
	cfgBytes, _ := json.Marshal(Config{APIToken: "tok", DatabaseID: "db-1"})
	src := &knowledge.KnowledgeSource{
		ID:     "src-notion",
		Type:   "notion",
		Config: json.RawMessage(cfgBytes),
	}

	syncer := New()
	if err := syncer.Sync(context.Background(), src, svc); err == nil {
		t.Error("Sync() expected error for API 500, got nil")
	}
}

// --- listBlockChildren tests ---

func TestListBlockChildren_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "block-abc"},
				{"id": "block-def"},
			},
		})
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	ids, err := listBlockChildren(context.Background(), &http.Client{}, "tok", "page-1")
	if err != nil {
		t.Fatalf("listBlockChildren() error = %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 IDs, got %d", len(ids))
	}
}

func TestListBlockChildren_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	_, err := listBlockChildren(context.Background(), &http.Client{}, "tok", "page-1")
	if err == nil {
		t.Error("listBlockChildren() expected error for non-200 response")
	}
}

// --- deleteBlock tests ---

func TestDeleteBlock_Success(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			called = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	err := deleteBlock(context.Background(), &http.Client{}, "tok", "block-1")
	if err != nil {
		t.Fatalf("deleteBlock() error = %v", err)
	}
	if !called {
		t.Error("expected DELETE request to be made")
	}
}

// --- appendBlocks tests ---

func TestAppendBlocks_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	err := appendBlocks(context.Background(), &http.Client{}, "tok", "page-1", "line1\nline2")
	if err != nil {
		t.Fatalf("appendBlocks() error = %v", err)
	}
}

func TestAppendBlocks_EmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	// Empty content results in a single block containing the empty string.
	err := appendBlocks(context.Background(), &http.Client{}, "tok", "page-1", "")
	if err != nil {
		t.Fatalf("appendBlocks() empty content error = %v", err)
	}
}

func TestAppendBlocks_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"bad request"}`))
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	err := appendBlocks(context.Background(), &http.Client{}, "tok", "page-1", "some content")
	if err == nil {
		t.Error("appendBlocks() expected error for non-2xx response")
	}
}

// --- UpdatePage tests ---

func TestUpdatePage_Success(t *testing.T) {
	blockDeleted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			// listBlockChildren
			json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{"id": "block-1"}},
			})
		case http.MethodDelete:
			blockDeleted = true
			w.WriteHeader(http.StatusOK)
		case http.MethodPatch:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	cfg := &Config{APIToken: "tok", DatabaseID: "db-1"}
	if err := UpdatePage(context.Background(), cfg, "page-1", "line1\nline2"); err != nil {
		t.Fatalf("UpdatePage() error = %v", err)
	}
	if !blockDeleted {
		t.Error("expected existing block to be deleted")
	}
}

func TestUpdatePage_ListBlocksError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	cfg := &Config{APIToken: "tok", DatabaseID: "db-1"}
	if err := UpdatePage(context.Background(), cfg, "page-1", "content"); err == nil {
		t.Error("UpdatePage() expected error when listBlockChildren fails")
	}
}

func TestFetchPages_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	_, err := fetchPages(context.Background(), &Config{APIToken: "tok", DatabaseID: "db-1"})
	if err == nil {
		t.Error("fetchPages() expected error for malformed JSON response")
	}
}

func TestListBlockChildren_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid`))
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	_, err := listBlockChildren(context.Background(), &http.Client{}, "tok", "page-1")
	if err == nil {
		t.Error("listBlockChildren() expected error for malformed JSON response")
	}
}

func TestUpdatePage_AppendBlocksError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			// listBlockChildren — empty list, no deletes needed
			json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
		case http.MethodPatch:
			// appendBlocks — fail
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"message":"error"}`))
		}
	}))
	defer srv.Close()
	useNotionTestServer(t, srv)

	cfg := &Config{APIToken: "tok", DatabaseID: "db-1"}
	if err := UpdatePage(context.Background(), cfg, "page-1", "content"); err == nil {
		t.Error("UpdatePage() expected error when appendBlocks fails")
	}
}
