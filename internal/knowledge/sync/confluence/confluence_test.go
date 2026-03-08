package confluence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// makeConfluenceSource builds a KnowledgeSource JSON config pointing to baseURL.
func makeConfluenceSource(baseURL string) *knowledge.KnowledgeSource {
	cfg, _ := json.Marshal(Config{
		BaseURL:  baseURL,
		APIToken: "test-token",
		Email:    "test@example.com",
		SpaceKey: "ENG",
	})
	return &knowledge.KnowledgeSource{
		ID:     "src-1",
		Name:   "Test Confluence",
		Type:   "confluence",
		Config: json.RawMessage(cfg),
	}
}

// --- parseConfig tests ---

func TestParseConfig_Valid(t *testing.T) {
	raw, _ := json.Marshal(Config{
		BaseURL:   "https://company.atlassian.net",
		APIToken:  "tok",
		Email:     "eng@company.com",
		SpaceKey:  "ENG",
		PageLimit: 100,
	})
	cfg, err := parseConfig(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.BaseURL != "https://company.atlassian.net" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.SpaceKey != "ENG" {
		t.Errorf("SpaceKey = %q", cfg.SpaceKey)
	}
}

func TestParseConfig_MissingFields(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "missing base_url",
			cfg:     Config{APIToken: "tok", SpaceKey: "ENG"},
			wantErr: true,
		},
		{
			name:    "missing api_token",
			cfg:     Config{BaseURL: "https://x.atlassian.net", SpaceKey: "ENG"},
			wantErr: true,
		},
		{
			name:    "missing space_key",
			cfg:     Config{BaseURL: "https://x.atlassian.net", APIToken: "tok"},
			wantErr: true,
		},
		{
			name:    "all required fields present",
			cfg:     Config{BaseURL: "https://x.atlassian.net", APIToken: "tok", SpaceKey: "ENG"},
			wantErr: false,
		},
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
	_, err := parseConfig(json.RawMessage(`{invalid json`))
	if err == nil {
		t.Error("parseConfig() expected error for invalid JSON, got nil")
	}
}

// --- Sync tests ---

// confluencePagesHandler returns a standard Confluence pages API response.
func confluencePagesHandler(pages []map[string]any, nextCursor string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"results": pages,
		}
		if nextCursor != "" {
			resp["_links"] = map[string]any{"next": fmt.Sprintf("?cursor=%s&limit=25", nextCursor)}
		} else {
			resp["_links"] = map[string]any{"next": ""}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
}

func TestSync_SinglePage(t *testing.T) {
	pages := []map[string]any{
		{
			"id":    "page-1",
			"title": "Getting Started",
			"body": map[string]any{
				"storage": map[string]any{"value": "<p>Hello World</p>"},
			},
			"_links": map[string]any{"webui": "/wiki/spaces/ENG/getting-started"},
		},
	}

	srv := httptest.NewServer(confluencePagesHandler(pages, ""))
	defer srv.Close()

	svc := newTestKnowledgeSvc(t)
	src := makeConfluenceSource(srv.URL)
	syncer := New()

	err := syncer.Sync(context.Background(), src, svc)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Verify the entry was upserted.
	entries, err := svc.List(context.Background(), knowledge.EntryFilter{Tier: knowledge.TierSynced})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 synced entry, got %d", len(entries))
	}
	if entries[0].Title != "Getting Started" {
		t.Errorf("Title = %q, want %q", entries[0].Title, "Getting Started")
	}
	if entries[0].SourceType != knowledge.SourceTypeConfluence {
		t.Errorf("SourceType = %q, want %q", entries[0].SourceType, knowledge.SourceTypeConfluence)
	}
}

func TestSync_Pagination(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		cursor := r.URL.Query().Get("cursor")

		var resp map[string]any
		if cursor == "" {
			// First page: 1 result + nextCursor
			resp = map[string]any{
				"results": []map[string]any{
					{"id": "p1", "title": "Page 1",
						"body":   map[string]any{"storage": map[string]any{"value": "content1"}},
						"_links": map[string]any{"webui": "/wiki/p1"},
					},
				},
				"_links": map[string]any{"next": "?cursor=cursor2&limit=25"},
			}
		} else {
			// Second page: 1 result, no nextCursor
			resp = map[string]any{
				"results": []map[string]any{
					{"id": "p2", "title": "Page 2",
						"body":   map[string]any{"storage": map[string]any{"value": "content2"}},
						"_links": map[string]any{"webui": "/wiki/p2"},
					},
				},
				"_links": map[string]any{"next": ""},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	svc := newTestKnowledgeSvc(t)
	src := makeConfluenceSource(srv.URL)
	syncer := New()

	err := syncer.Sync(context.Background(), src, svc)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls (pagination), got %d", callCount)
	}

	entries, _ := svc.List(context.Background(), knowledge.EntryFilter{Tier: knowledge.TierSynced})
	if len(entries) != 2 {
		t.Errorf("expected 2 synced entries, got %d", len(entries))
	}
}

func TestSync_APIError401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer srv.Close()

	svc := newTestKnowledgeSvc(t)
	src := makeConfluenceSource(srv.URL)
	syncer := New()

	err := syncer.Sync(context.Background(), src, svc)
	if err == nil {
		t.Error("Sync() expected error for 401, got nil")
	}
}

func TestSync_BadConfig(t *testing.T) {
	svc := newTestKnowledgeSvc(t)
	src := &knowledge.KnowledgeSource{
		ID:     "bad",
		Type:   "confluence",
		Config: json.RawMessage(`{invalid json`),
	}
	syncer := New()
	err := syncer.Sync(context.Background(), src, svc)
	if err == nil {
		t.Error("Sync() expected error for invalid config, got nil")
	}
}
