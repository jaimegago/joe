package api_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	_ "github.com/mattn/go-sqlite3"
)

func setupTestServer(t *testing.T) (*api.Server, graph.GraphStore) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE graph_nodes (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			source_id TEXT,
			metadata TEXT DEFAULT '{}',
			first_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX idx_graph_nodes_type ON graph_nodes(type);
		CREATE INDEX idx_graph_nodes_source ON graph_nodes(source_id);

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

	graphStore := graph.NewSQLiteStore(db)
	services := &core.Services{
		Config: &config.Config{},
		Graph:  graphStore,
	}

	server := api.New(services)
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
		{From: "payment-svc", To: "user-svc", Relation: "calls"},
		{From: "user-svc", To: "postgres", Relation: "reads_from"},
	}
	for _, e := range edges {
		if err := store.AddEdge(ctx, e); err != nil {
			t.Fatalf("add edge %s->%s: %v", e.From, e.To, err)
		}
	}
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, store := setupTestServer(t)
			mux := setupMux(t, server)

			if tt.seed {
				seedTestGraph(t, store)
			}

			path := "/api/v1/graph/related/" + tt.nodeID
			if tt.depth != "" {
				path += "?depth=" + tt.depth
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

func TestHandleNotImplemented(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/sources"},
		{"POST", "/api/v1/sources"},
		{"GET", "/api/v1/clarifications"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusNotImplemented {
				t.Errorf("status = %d, want %d", w.Code, http.StatusNotImplemented)
			}
		})
	}
}
