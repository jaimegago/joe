package api_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	_ "modernc.org/sqlite"
)

func setupTestServer(t *testing.T) (*api.Server, graph.GraphStore) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE graph_nodes (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			component_id TEXT,
			metadata TEXT DEFAULT '{}',
			first_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX idx_graph_nodes_type ON graph_nodes(type);
		CREATE INDEX idx_graph_nodes_source ON graph_nodes(component_id);

		CREATE TABLE graph_edges (
			from_node TEXT NOT NULL REFERENCES graph_nodes(id) ON DELETE CASCADE,
			to_node TEXT NOT NULL REFERENCES graph_nodes(id) ON DELETE CASCADE,
			relation TEXT NOT NULL,
			confidence INTEGER DEFAULT 3,
			source TEXT,
			context TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (from_node, to_node, relation)
		);
		CREATE INDEX idx_graph_edges_from ON graph_edges(from_node);
		CREATE INDEX idx_graph_edges_to ON graph_edges(to_node);
		CREATE INDEX idx_graph_edges_relation ON graph_edges(relation);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}

	graphStore := graph.NewSQLiteStore(db, nil)
	services := &core.Services{
		Config:   &config.Config{},
		Graph:    graphStore,
		Adapters: adapters.NewRegistry(),
	}

	server := api.New(services, api.TestingPolicyEngine(services))
	return server, graphStore
}

func setupMux(t *testing.T, server *api.Server) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

func seedTestGraph(t *testing.T, store graph.GraphStore) {
	t.Helper()
	ctx := context.Background()

	nodes := []graph.Node{
		{ID: "payment-svc", Type: "service"},
		{ID: "user-svc", Type: "service"},
		{ID: "postgres", Type: "database"},
	}
	for _, n := range nodes {
		if err := store.AddNode(ctx, n); err != nil {
			t.Fatalf("add node %s: %v", n.ID, err)
		}
	}

	edges := []graph.Edge{
		{From: "payment-svc", To: "user-svc", Relation: "calls", ComponentID: ""},
		{From: "user-svc", To: "postgres", Relation: "reads_from", ComponentID: ""},
	}
	for _, e := range edges {
		if err := store.AddEdge(ctx, e); err != nil {
			t.Fatalf("add edge %s->%s: %v", e.From, e.To, err)
		}
	}
}

func TestNew_NilServices_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("api.New(nil, nil) should panic")
		}
	}()
	api.New(nil, nil) //nolint:staticcheck // intentional nil to verify panic
}

func TestHandleStatus(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	if body["version"] == nil {
		t.Error("missing version field")
	}
	if body["time"] == nil {
		t.Error("missing time field")
	}
}

func TestHandleVersion(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)

	req := httptest.NewRequest("GET", "/api/v1/version", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The full buildinfo.Info is serialized: version/commit/build_time/ui_digest.
	// On the test build the injected fields carry their unset defaults.
	for _, field := range []string{"version", "commit", "build_time", "ui_digest"} {
		if _, ok := body[field]; !ok {
			t.Errorf("missing %q field in version response", field)
		}
	}
	if body["version"] != "dev" {
		t.Errorf("version = %v, want dev (unset build)", body["version"])
	}
}

func TestHandleGraphQuery(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		seed       bool
		wantStatus int
		wantCount  int
	}{
		{
			name:       "missing query param",
			query:      "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "query by type returns matches",
			query:      "type:service",
			seed:       true,
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name:       "query by type no matches",
			query:      "type:deployment",
			seed:       true,
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "free text search",
			query:      "payment",
			seed:       true,
			wantStatus: http.StatusOK,
			wantCount:  1,
		},
		{
			name:       "empty graph",
			query:      "type:service",
			seed:       false,
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, store := setupTestServer(t)
			mux := setupMux(t, server)

			if tt.seed {
				seedTestGraph(t, store)
			}

			path := "/api/v1/graph/query"
			if tt.query != "" {
				path += "?q=" + tt.query
			}

			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var body map[string]any
				if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				count := int(body["count"].(float64))
				if count != tt.wantCount {
					t.Errorf("count = %d, want %d", count, tt.wantCount)
				}
			}
		})
	}
}

func TestHandleGraphRelated(t *testing.T) {
	tests := []struct {
		name       string
		nodeID     string
		depth      string
		seed       bool
		wantStatus int
		wantNodes  int
	}{
		{
			name:       "related at depth 1",
			nodeID:     "payment-svc",
			depth:      "1",
			seed:       true,
			wantStatus: http.StatusOK,
			wantNodes:  2, // payment-svc + user-svc
		},
		{
			name:       "related at depth 2",
			nodeID:     "payment-svc",
			depth:      "2",
			seed:       true,
			wantStatus: http.StatusOK,
			wantNodes:  3, // all 3 nodes
		},
		{
			name:       "default depth",
			nodeID:     "payment-svc",
			seed:       true,
			wantStatus: http.StatusOK,
			wantNodes:  2,
		},
		{
			name:       "not found",
			nodeID:     "nonexistent",
			seed:       true,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "invalid depth",
			nodeID:     "payment-svc",
			depth:      "abc",
			seed:       true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "negative depth",
			nodeID:     "payment-svc",
			depth:      "-1",
			seed:       true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing nodeID",
			nodeID:     "",
			seed:       true,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "node ID with slashes",
			nodeID:     "deployment/prod/payment-svc",
			depth:      "1",
			seed:       false, // uses custom seed below
			wantStatus: http.StatusOK,
			wantNodes:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, store := setupTestServer(t)
			mux := setupMux(t, server)

			if tt.seed {
				seedTestGraph(t, store)
			}

			// Custom seed for slashed node IDs
			if tt.name == "node ID with slashes" {
				ctx := context.Background()
				for _, n := range []graph.Node{
					{ID: "deployment/prod/payment-svc", Type: "deployment"},
					{ID: "deployment/prod/user-svc", Type: "deployment"},
				} {
					if err := store.AddNode(ctx, n); err != nil {
						t.Fatalf("add node: %v", err)
					}
				}
				if err := store.AddEdge(ctx, graph.Edge{
					From: "deployment/prod/payment-svc", To: "deployment/prod/user-svc", Relation: "calls",
					ComponentID: "",
				}); err != nil {
					t.Fatalf("add edge: %v", err)
				}
			}

			path := "/api/v1/graph/related?nodeID=" + tt.nodeID
			if tt.depth != "" {
				path += "&depth=" + tt.depth
			}

			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var body struct {
					Nodes []graph.Node `json:"nodes"`
					Edges []graph.Edge `json:"edges"`
				}
				if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if len(body.Nodes) != tt.wantNodes {
					t.Errorf("nodes = %d, want %d", len(body.Nodes), tt.wantNodes)
				}
			}
		})
	}
}

func TestHandleGraphSummary(t *testing.T) {
	tests := []struct {
		name      string
		seed      bool
		wantNodes int
		wantEdges int
	}{
		{
			name:      "empty graph",
			seed:      false,
			wantNodes: 0,
			wantEdges: 0,
		},
		{
			name:      "seeded graph",
			seed:      true,
			wantNodes: 3,
			wantEdges: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, store := setupTestServer(t)
			mux := setupMux(t, server)

			if tt.seed {
				seedTestGraph(t, store)
			}

			req := httptest.NewRequest("GET", "/api/v1/graph/summary", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
			}

			var body graph.GraphSummary
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if body.NodeCount != tt.wantNodes {
				t.Errorf("node_count = %d, want %d", body.NodeCount, tt.wantNodes)
			}
			if body.EdgeCount != tt.wantEdges {
				t.Errorf("edge_count = %d, want %d", body.EdgeCount, tt.wantEdges)
			}
		})
	}
}

// TestSlashedNodeIDs is a regression test ensuring that graph node IDs
// containing slashes (e.g. "deployment/prod/payment-svc") work correctly
// across all graph API endpoints. Go 1.22's http.ServeMux {wildcard} only
// matches a single path segment, so IDs with slashes must be passed as
// query parameters, not path parameters.
func TestSlashedNodeIDs(t *testing.T) {
	server, store := setupTestServer(t)
	mux := setupMux(t, server)

	ctx := context.Background()

	// Seed nodes with realistic slashed IDs
	slashedNodes := []graph.Node{
		{ID: "deployment/prod/payment-svc", Type: "deployment"},
		{ID: "deployment/prod/user-svc", Type: "deployment"},
		{ID: "service/prod/payment-svc", Type: "service"},
		{ID: "statefulset/prod/postgres", Type: "statefulset"},
		{ID: "namespace/prod", Type: "namespace"},
	}
	for _, n := range slashedNodes {
		if err := store.AddNode(ctx, n); err != nil {
			t.Fatalf("add node %s: %v", n.ID, err)
		}
	}

	slashedEdges := []graph.Edge{
		{From: "deployment/prod/payment-svc", To: "service/prod/payment-svc", Relation: "exposes"},
		{From: "deployment/prod/payment-svc", To: "deployment/prod/user-svc", Relation: "calls"},
		{From: "deployment/prod/user-svc", To: "statefulset/prod/postgres", Relation: "depends_on"},
		{From: "namespace/prod", To: "deployment/prod/payment-svc", Relation: "contains"},
	}
	for _, e := range slashedEdges {
		if err := store.AddEdge(ctx, e); err != nil {
			t.Fatalf("add edge %s->%s: %v", e.From, e.To, err)
		}
	}

	t.Run("graph/query by type finds slashed IDs", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/graph/query?q=type:deployment", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var body struct {
			Nodes []graph.Node `json:"nodes"`
			Count int          `json:"count"`
		}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Count != 2 {
			t.Errorf("count = %d, want 2", body.Count)
		}
		for _, n := range body.Nodes {
			if n.Type != "deployment" {
				t.Errorf("unexpected type %q for node %q", n.Type, n.ID)
			}
		}
	})

	t.Run("graph/query free text matches slashed ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/graph/query?q=payment", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var body struct {
			Count int `json:"count"`
		}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Count != 2 { // deployment + service both have "payment" in ID
			t.Errorf("count = %d, want 2", body.Count)
		}
	})

	t.Run("graph/related depth 1 with slashed ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/graph/related?nodeID=deployment/prod/payment-svc&depth=1", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var body struct {
			Nodes []graph.Node `json:"nodes"`
			Edges []graph.Edge `json:"edges"`
		}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// payment-svc + user-svc + service/payment-svc + namespace/prod
		if len(body.Nodes) != 4 {
			t.Errorf("nodes = %d, want 4", len(body.Nodes))
		}
		if len(body.Edges) != 3 { // exposes + calls + contains
			t.Errorf("edges = %d, want 3", len(body.Edges))
		}
	})

	t.Run("graph/related depth 2 traverses full slashed graph", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/graph/related?nodeID=deployment/prod/payment-svc&depth=2", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var body struct {
			Nodes []graph.Node `json:"nodes"`
			Edges []graph.Edge `json:"edges"`
		}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(body.Nodes) != 5 { // all nodes reachable at depth 2
			t.Errorf("nodes = %d, want 5", len(body.Nodes))
		}
		if len(body.Edges) != 4 {
			t.Errorf("edges = %d, want 4", len(body.Edges))
		}
	})

	t.Run("graph/related not found with slashed ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/graph/related?nodeID=deployment/staging/ghost-svc", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusNotFound, w.Body.String())
		}
	})

	t.Run("graph/summary includes slashed nodes", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/graph/summary", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		var body graph.GraphSummary
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.NodeCount != 5 {
			t.Errorf("node_count = %d, want 5", body.NodeCount)
		}
		if body.EdgeCount != 4 {
			t.Errorf("edge_count = %d, want 4", body.EdgeCount)
		}
		if body.NodesByType["deployment"] != 2 {
			t.Errorf("deployment count = %d, want 2", body.NodesByType["deployment"])
		}
	})
}
