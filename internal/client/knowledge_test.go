package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/knowledge"
)

func TestCreateKnowledgeEntry(t *testing.T) {
	want := knowledge.Entry{ID: "entry-1", Title: "Test Entry", Tier: knowledge.TierCurated}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != apiKnowledgeEntriesPath {
			t.Errorf("Path = %q, want %q", r.URL.Path, apiKnowledgeEntriesPath)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(want)
	}))
	defer ts.Close()

	c := New(ts.URL)
	got, err := c.CreateKnowledgeEntry(context.Background(), &knowledge.Entry{Title: "Test Entry"})
	if err != nil {
		t.Fatalf("CreateKnowledgeEntry() error = %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.Title != want.Title {
		t.Errorf("Title = %q, want %q", got.Title, want.Title)
	}
}

func TestGetKnowledgeEntry(t *testing.T) {
	want := knowledge.Entry{ID: "entry-abc", Title: "My Entry"}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Method = %q, want GET", r.Method)
		}
		if r.URL.Path != apiKnowledgeEntriesPath+"/entry-abc" {
			t.Errorf("Path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer ts.Close()

	c := New(ts.URL)
	got, err := c.GetKnowledgeEntry(context.Background(), "entry-abc")
	if err != nil {
		t.Fatalf("GetKnowledgeEntry() error = %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
}

func TestListKnowledgeEntries_NoFilter(t *testing.T) {
	var capturedQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		result := struct {
			Entries []*knowledge.Entry `json:"entries"`
			Count   int                `json:"count"`
		}{
			Entries: []*knowledge.Entry{{ID: "e1"}, {ID: "e2"}},
			Count:   2,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	c := New(ts.URL)
	entries, err := c.ListKnowledgeEntries(context.Background(), "", "")
	if err != nil {
		t.Fatalf("ListKnowledgeEntries() error = %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("len(entries) = %d, want 2", len(entries))
	}
	// No filters → no query string appended.
	if capturedQuery != "" {
		t.Errorf("query = %q, want empty (no filters)", capturedQuery)
	}
}

func TestListKnowledgeEntries_WithTierFilter(t *testing.T) {
	var capturedTier string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTier = r.URL.Query().Get("tier")
		result := struct {
			Entries []*knowledge.Entry `json:"entries"`
			Count   int                `json:"count"`
		}{}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.ListKnowledgeEntries(context.Background(), knowledge.TierSynced, "")
	if err != nil {
		t.Fatalf("ListKnowledgeEntries() error = %v", err)
	}
	if capturedTier != "synced" {
		t.Errorf("tier param = %q, want %q", capturedTier, "synced")
	}
}

func TestListKnowledgeEntries_WithSourceTypeFilter(t *testing.T) {
	var capturedSourceType string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSourceType = r.URL.Query().Get("source_type")
		result := struct {
			Entries []*knowledge.Entry `json:"entries"`
			Count   int                `json:"count"`
		}{}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.ListKnowledgeEntries(context.Background(), "", knowledge.SourceTypeConfluence)
	if err != nil {
		t.Fatalf("ListKnowledgeEntries() error = %v", err)
	}
	if capturedSourceType != "confluence" {
		t.Errorf("source_type param = %q, want %q", capturedSourceType, "confluence")
	}
}

func TestDeleteKnowledgeEntry(t *testing.T) {
	var capturedMethod, capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"status": "deleted", "id": "entry-del"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.DeleteKnowledgeEntry(context.Background(), "entry-del")
	if err != nil {
		t.Fatalf("DeleteKnowledgeEntry() error = %v", err)
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("Method = %q, want DELETE", capturedMethod)
	}
	wantPath := apiKnowledgeEntriesPath + "/entry-del"
	if capturedPath != wantPath {
		t.Errorf("Path = %q, want %q", capturedPath, wantPath)
	}
}

func TestSearchKnowledge(t *testing.T) {
	var capturedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != apiKnowledgeSearchPath {
			t.Errorf("Path = %q, want %q", r.URL.Path, apiKnowledgeSearchPath)
		}
		json.NewDecoder(r.Body).Decode(&capturedBody)
		result := struct {
			Results []knowledge.SearchResult `json:"results"`
			Count   int                      `json:"count"`
			Query   string                   `json:"query"`
		}{
			Results: []knowledge.SearchResult{{Entry: knowledge.Entry{ID: "sr-1"}, Similarity: 0.9}},
			Count:   1,
			Query:   "DB latency",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	c := New(ts.URL)
	results, err := c.SearchKnowledge(context.Background(), "DB latency", 5, []knowledge.Tier{knowledge.TierSynced})
	if err != nil {
		t.Fatalf("SearchKnowledge() error = %v", err)
	}
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
	if capturedBody["query"] != "DB latency" {
		t.Errorf("query = %v, want %q", capturedBody["query"], "DB latency")
	}
	if capturedBody["top_k"] != float64(5) {
		t.Errorf("top_k = %v, want 5", capturedBody["top_k"])
	}
}

func TestCreateKnowledgeSource(t *testing.T) {
	want := knowledge.KnowledgeSource{ID: "src-1", Type: "confluence"}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != apiKnowledgeSourcesPath {
			t.Errorf("Path = %q, want %q", r.URL.Path, apiKnowledgeSourcesPath)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(want)
	}))
	defer ts.Close()

	c := New(ts.URL)
	got, err := c.CreateKnowledgeSource(context.Background(), &knowledge.KnowledgeSource{Type: "confluence"})
	if err != nil {
		t.Fatalf("CreateKnowledgeSource() error = %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
}

func TestListKnowledgeSources(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Method = %q, want GET", r.Method)
		}
		if r.URL.Path != apiKnowledgeSourcesPath {
			t.Errorf("Path = %q, want %q", r.URL.Path, apiKnowledgeSourcesPath)
		}
		result := struct {
			Sources []*knowledge.KnowledgeSource `json:"sources"`
			Count   int                          `json:"count"`
		}{
			Sources: []*knowledge.KnowledgeSource{{ID: "src-1"}, {ID: "src-2"}},
			Count:   2,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	c := New(ts.URL)
	sources, err := c.ListKnowledgeSources(context.Background())
	if err != nil {
		t.Fatalf("ListKnowledgeSources() error = %v", err)
	}
	if len(sources) != 2 {
		t.Errorf("len(sources) = %d, want 2", len(sources))
	}
}

func TestDeleteKnowledgeSource(t *testing.T) {
	var capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"status": "deleted"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.DeleteKnowledgeSource(context.Background(), "src-del")
	if err != nil {
		t.Fatalf("DeleteKnowledgeSource() error = %v", err)
	}
	wantPath := apiKnowledgeSourcesPath + "/src-del"
	if capturedPath != wantPath {
		t.Errorf("Path = %q, want %q", capturedPath, wantPath)
	}
}

func TestTriggerKnowledgeSync(t *testing.T) {
	var capturedMethod, capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	defer ts.Close()

	c := New(ts.URL)
	err := c.TriggerKnowledgeSync(context.Background(), "src-1")
	if err != nil {
		t.Fatalf("TriggerKnowledgeSync() error = %v", err)
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("Method = %q, want POST", capturedMethod)
	}
	wantPath := apiKnowledgeSourcesPath + "/src-1/sync"
	if capturedPath != wantPath {
		t.Errorf("Path = %q, want %q", capturedPath, wantPath)
	}
}
