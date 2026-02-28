package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
)

// --- PostgreSQL mock ---

type mockPostgresAdapter struct {
	stat *postgresadapter.Stat
	rows []map[string]any
	err  error
}

func (m *mockPostgresAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockPostgresAdapter) Disconnect() error                               { return nil }
func (m *mockPostgresAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (m *mockPostgresAdapter) Stat(_ context.Context) (*postgresadapter.Stat, error) {
	return m.stat, m.err
}
func (m *mockPostgresAdapter) Query(_ context.Context, _ string) ([]map[string]any, error) {
	return m.rows, m.err
}

func setupDatastoreServer(t *testing.T, sourceID string, mock adapters.Adapter) *http.ServeMux {
	t.Helper()
	registry := adapters.NewRegistry()
	registry.Register(sourceID, mock)
	services := &core.Services{
		Config:   &config.Config{},
		Adapters: registry,
	}
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return mux
}

// --- PostgreSQL tests ---

func TestHandlePostgresStat_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/postgresql/nonexistent/stat", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandlePostgresStat(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockPostgresAdapter
		wantStatus int
	}{
		{
			name: "success",
			mock: &mockPostgresAdapter{stat: &postgresadapter.Stat{
				Activity: []postgresadapter.ActivityRow{{PID: 1, State: "active"}},
			}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockPostgresAdapter{err: fmt.Errorf("postgres connection error")},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "pg-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/postgresql/pg-src/stat", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandlePostgresQuery_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/postgresql/nonexistent/query?sql=SELECT+1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- MySQL tests (404 paths) ---

func TestHandleMySQLStat_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/mysql/nonexistent/stat", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleMySQLQuery_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/mysql/nonexistent/query?sql=SELECT+1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- Redis tests (404 paths) ---

func TestHandleRedisInfo_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/redis/nonexistent/info", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleRedisSlowLog_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/redis/nonexistent/slowlog", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleRedisDBSize_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/redis/nonexistent/dbsize", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- MongoDB tests (404 paths) ---

func TestHandleMongoDBServerStatus_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/mongodb/nonexistent/server-status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleMongoDBReplicaStatus_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/mongodb/nonexistent/replica-status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- Kafka tests (404 paths) ---

func TestHandleKafkaTopics_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/kafka/nonexistent/topics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleKafkaBrokers_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/kafka/nonexistent/brokers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleKafkaConsumerGroups_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/kafka/nonexistent/consumer-groups", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- Elasticsearch tests (404 paths) ---

func TestHandleElasticsearchHealth_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/elasticsearch/nonexistent/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleElasticsearchIndices_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/elasticsearch/nonexistent/indices", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
