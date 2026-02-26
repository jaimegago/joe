package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/knowledge"
	"github.com/jaimegago/joe/internal/knowledge/drift"
)

func TestDetectDrift_NoFilter(t *testing.T) {
	var capturedQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Method = %q, want GET", r.Method)
		}
		if r.URL.Path != apiKnowledgeDriftPath {
			t.Errorf("Path = %q, want %q", r.URL.Path, apiKnowledgeDriftPath)
		}
		capturedQuery = r.URL.RawQuery
		result := struct {
			Reports []*drift.DriftReport `json:"reports"`
			Count   int                  `json:"count"`
		}{
			Reports: []*drift.DriftReport{{EntryID: "e1"}, {EntryID: "e2"}},
			Count:   2,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	c := New(ts.URL)
	reports, err := c.DetectDrift(context.Background(), "")
	if err != nil {
		t.Fatalf("DetectDrift() error = %v", err)
	}
	if len(reports) != 2 {
		t.Errorf("len(reports) = %d, want 2", len(reports))
	}
	// No filter → no query string appended.
	if capturedQuery != "" {
		t.Errorf("query = %q, want empty (no filter)", capturedQuery)
	}
}

func TestDetectDrift_WithSourceTypeFilter(t *testing.T) {
	var capturedSourceType string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSourceType = r.URL.Query().Get("source_type")
		result := struct {
			Reports []*drift.DriftReport `json:"reports"`
			Count   int                  `json:"count"`
		}{}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.DetectDrift(context.Background(), knowledge.SourceTypeConfluence)
	if err != nil {
		t.Fatalf("DetectDrift() error = %v", err)
	}
	if capturedSourceType != "confluence" {
		t.Errorf("source_type param = %q, want %q", capturedSourceType, "confluence")
	}
}

func TestDetectDriftByEntry(t *testing.T) {
	want := drift.DriftReport{EntryID: "entry-1", ExternalChanged: true}
	var capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Method = %q, want GET", r.Method)
		}
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer ts.Close()

	c := New(ts.URL)
	got, err := c.DetectDriftByEntry(context.Background(), "entry-1")
	if err != nil {
		t.Fatalf("DetectDriftByEntry() error = %v", err)
	}
	if got.EntryID != want.EntryID {
		t.Errorf("EntryID = %q, want %q", got.EntryID, want.EntryID)
	}
	if !got.ExternalChanged {
		t.Error("ExternalChanged = false, want true")
	}
	wantPath := apiKnowledgeDriftPath + "/entry-1"
	if capturedPath != wantPath {
		t.Errorf("Path = %q, want %q", capturedPath, wantPath)
	}
}

func TestDetectDriftByEntry_URLEscapedID(t *testing.T) {
	var capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Use EscapedPath() to get the percent-encoded form as sent by the client.
		capturedPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(drift.DriftReport{EntryID: "entry with spaces"})
	}))
	defer ts.Close()

	c := New(ts.URL)
	_, err := c.DetectDriftByEntry(context.Background(), "entry with spaces")
	if err != nil {
		t.Fatalf("DetectDriftByEntry() error = %v", err)
	}
	wantPath := apiKnowledgeDriftPath + "/entry%20with%20spaces"
	if capturedPath != wantPath {
		t.Errorf("Path = %q, want %q (URL encoding)", capturedPath, wantPath)
	}
}
