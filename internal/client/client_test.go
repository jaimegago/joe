package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/graph"
)

func TestWithAPIKey_SetsAuthHeader(t *testing.T) {
	var capturedAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "test", "time": "now"})
	}))
	defer ts.Close()

	c := New(ts.URL, WithAPIKey("my-secret"))
	_, err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus() error: %v", err)
	}

	if capturedAuth != "Bearer my-secret" {
		t.Errorf("Authorization = %q, want %q", capturedAuth, "Bearer my-secret")
	}
}

func TestNoAPIKey_NoAuthHeader(t *testing.T) {
	var capturedAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "test", "time": "now"})
	}))
	defer ts.Close()

	c := New(ts.URL) // no WithAPIKey
	_, err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus() error: %v", err)
	}

	if capturedAuth != "" {
		t.Errorf("Authorization = %q, want empty (no key configured)", capturedAuth)
	}
}

func TestWithAPIKey_GraphRelated(t *testing.T) {
	var capturedAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(graph.Subgraph{
			Nodes: []graph.Node{{ID: "test", Type: "test"}},
		})
	}))
	defer ts.Close()

	c := New(ts.URL, WithAPIKey("graph-key"))
	_, err := c.GraphRelated(context.Background(), "test", 1)
	if err != nil {
		t.Fatalf("GraphRelated() error: %v", err)
	}

	if capturedAuth != "Bearer graph-key" {
		t.Errorf("Authorization = %q, want %q", capturedAuth, "Bearer graph-key")
	}
}

// TestGraphRelatedSlashedNodeIDs verifies the client correctly encodes
// node IDs containing slashes as query parameters, not path segments.
// This is a regression test: Go 1.22's http.ServeMux {wildcard} only
// matches a single path segment, so "deployment/prod/payment-svc" in a
// path would 404.
func TestGraphRelatedSlashedNodeIDs(t *testing.T) {
	tests := []struct {
		name   string
		nodeID string
		depth  int
	}{
		{
			name:   "two slashes",
			nodeID: "deployment/prod/payment-svc",
			depth:  1,
		},
		{
			name:   "one slash",
			nodeID: "namespace/prod",
			depth:  2,
		},
		{
			name:   "three slashes",
			nodeID: "apps/v1/deployment/nginx",
			depth:  1,
		},
		{
			name:   "no slashes",
			nodeID: "payment-svc",
			depth:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedNodeID string
			var capturedPath string

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Path
				capturedNodeID = r.URL.Query().Get("nodeID")

				resp := graph.Subgraph{
					Nodes: []graph.Node{{ID: capturedNodeID, Type: "test"}},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer ts.Close()

			c := New(ts.URL)
			result, err := c.GraphRelated(context.Background(), tt.nodeID, tt.depth)
			if err != nil {
				t.Fatalf("GraphRelated() error: %v", err)
			}

			// The path must NOT contain the nodeID — it goes in query params
			if capturedPath != "/api/v1/graph/related" {
				t.Errorf("path = %q, want %q (nodeID leaked into path)", capturedPath, "/api/v1/graph/related")
			}

			// The nodeID must arrive intact via query parameter
			if capturedNodeID != tt.nodeID {
				t.Errorf("nodeID = %q, want %q", capturedNodeID, tt.nodeID)
			}

			if len(result.Nodes) != 1 || result.Nodes[0].ID != tt.nodeID {
				t.Errorf("response node ID = %q, want %q", result.Nodes[0].ID, tt.nodeID)
			}
		})
	}
}
