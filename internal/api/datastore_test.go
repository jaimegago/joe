package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	mysqladapter "github.com/jaimegago/joe/internal/adapters/datastore/mysql"
	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
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

// --- MySQL mock adapter ---

type mockMySQLAdapter struct {
	stat *mysqladapter.Stat
	rows []map[string]any
	err  error
}

func (m *mockMySQLAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockMySQLAdapter) Disconnect() error                               { return nil }
func (m *mockMySQLAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (m *mockMySQLAdapter) Stat(_ context.Context) (*mysqladapter.Stat, error) {
	return m.stat, m.err
}
func (m *mockMySQLAdapter) Query(_ context.Context, _ string) ([]map[string]any, error) {
	return m.rows, m.err
}

// --- Redis mock adapter ---

type mockRedisAdapter struct {
	info   map[string]string
	slow   []redisadapter.SlowLogEntry
	dbSize int64
	err    error
}

func (m *mockRedisAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockRedisAdapter) Disconnect() error                               { return nil }
func (m *mockRedisAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (m *mockRedisAdapter) Info(_ context.Context, _ string) (map[string]string, error) {
	return m.info, m.err
}
func (m *mockRedisAdapter) SlowLog(_ context.Context, _ int64) ([]redisadapter.SlowLogEntry, error) {
	return m.slow, m.err
}
func (m *mockRedisAdapter) DBSize(_ context.Context) (int64, error) {
	return m.dbSize, m.err
}

// --- MongoDB mock adapter ---

type mockMongoDBAdapter struct {
	status  map[string]any
	replica map[string]any
	current map[string]any
	err     error
}

func (m *mockMongoDBAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockMongoDBAdapter) Disconnect() error                               { return nil }
func (m *mockMongoDBAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (m *mockMongoDBAdapter) ServerStatus(_ context.Context) (map[string]any, error) {
	return m.status, m.err
}
func (m *mockMongoDBAdapter) ReplicaStatus(_ context.Context) (map[string]any, error) {
	return m.replica, m.err
}
func (m *mockMongoDBAdapter) CurrentOp(_ context.Context) (map[string]any, error) {
	return m.current, m.err
}

// --- Kafka mock adapter ---

type mockKafkaAdapter struct {
	topics  []kafkaadapter.TopicInfo
	brokers []kafkaadapter.BrokerInfo
	groups  []kafkaadapter.ConsumerGroupInfo
	err     error
}

func (m *mockKafkaAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockKafkaAdapter) Disconnect() error                               { return nil }
func (m *mockKafkaAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (m *mockKafkaAdapter) Topics(_ context.Context) ([]kafkaadapter.TopicInfo, error) {
	return m.topics, m.err
}
func (m *mockKafkaAdapter) Brokers(_ context.Context) ([]kafkaadapter.BrokerInfo, error) {
	return m.brokers, m.err
}
func (m *mockKafkaAdapter) ConsumerGroups(_ context.Context) ([]kafkaadapter.ConsumerGroupInfo, error) {
	return m.groups, m.err
}

// --- Elasticsearch mock adapter ---

type mockElasticsearchAdapter struct {
	health  *elasticsearchadapter.ClusterHealth
	indices []elasticsearchadapter.IndexInfo
	err     error
}

func (m *mockElasticsearchAdapter) Connect(_ context.Context, _ store.Source) error { return nil }
func (m *mockElasticsearchAdapter) Disconnect() error                               { return nil }
func (m *mockElasticsearchAdapter) Status() adapters.Status                         { return adapters.Status{Connected: true} }
func (m *mockElasticsearchAdapter) ClusterHealth(_ context.Context) (*elasticsearchadapter.ClusterHealth, error) {
	return m.health, m.err
}
func (m *mockElasticsearchAdapter) ListIndices(_ context.Context, _ string) ([]elasticsearchadapter.IndexInfo, error) {
	return m.indices, m.err
}

// --- MySQL tests ---

func TestHandleMySQLStat(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockMySQLAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockMySQLAdapter{stat: &mysqladapter.Stat{}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockMySQLAdapter{err: fmt.Errorf("mysql error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "mysql-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/mysql/mysql-src/stat", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleMySQLQuery(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		mock       *mockMySQLAdapter
		wantStatus int
	}{
		{
			name:       "success",
			url:        "/api/v1/mysql/mysql-src/query?sql=SELECT+1",
			mock:       &mockMySQLAdapter{rows: []map[string]any{{"1": 1}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing sql",
			url:        "/api/v1/mysql/mysql-src/query",
			mock:       &mockMySQLAdapter{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/mysql/mysql-src/query?sql=SELECT+1",
			mock:       &mockMySQLAdapter{err: fmt.Errorf("mysql error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "mysql-src", tt.mock)
			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

// --- Postgres query success test ---

func TestHandlePostgresQuery(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		mock       *mockPostgresAdapter
		wantStatus int
	}{
		{
			name:       "success",
			url:        "/api/v1/postgresql/pg-src/query?sql=SELECT+1",
			mock:       &mockPostgresAdapter{rows: []map[string]any{{"1": 1}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing sql",
			url:        "/api/v1/postgresql/pg-src/query",
			mock:       &mockPostgresAdapter{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "adapter error",
			url:        "/api/v1/postgresql/pg-src/query?sql=SELECT+1",
			mock:       &mockPostgresAdapter{err: fmt.Errorf("pg error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "pg-src", tt.mock)
			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

// --- Redis tests ---

func TestHandleRedisInfo(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockRedisAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockRedisAdapter{info: map[string]string{"redis_version": "7.0.0"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockRedisAdapter{err: fmt.Errorf("redis error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "redis-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/redis/redis-src/info", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleRedisSlowLog(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockRedisAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockRedisAdapter{slow: []redisadapter.SlowLogEntry{{ID: 1, ExecutionTimeUS: 5000}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "nil result normalised",
			mock:       &mockRedisAdapter{slow: nil},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockRedisAdapter{err: fmt.Errorf("redis error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "redis-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/redis/redis-src/slowlog", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleRedisDBSize(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockRedisAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockRedisAdapter{dbSize: 42},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockRedisAdapter{err: fmt.Errorf("redis error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "redis-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/redis/redis-src/dbsize", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

// --- MongoDB tests ---

func TestHandleMongoDBServerStatus(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockMongoDBAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockMongoDBAdapter{status: map[string]any{"version": "6.0"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockMongoDBAdapter{err: fmt.Errorf("mongo error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "mongo-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/mongodb/mongo-src/server-status", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleMongoDBReplicaStatus(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockMongoDBAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockMongoDBAdapter{replica: map[string]any{"set": "rs0"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockMongoDBAdapter{err: fmt.Errorf("mongo error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "mongo-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/mongodb/mongo-src/replica-status", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleMongoDBCurrentOp(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockMongoDBAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockMongoDBAdapter{current: map[string]any{"inprog": []any{}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockMongoDBAdapter{err: fmt.Errorf("mongo error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "mongo-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/mongodb/mongo-src/current-op", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleMongoDBCurrentOp_NotFound(t *testing.T) {
	server, _ := setupTestServer(t)
	mux := setupMux(t, server)
	req := httptest.NewRequest("GET", "/api/v1/mongodb/nonexistent/current-op", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// --- Kafka tests ---

func TestHandleKafkaTopics(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockKafkaAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockKafkaAdapter{topics: []kafkaadapter.TopicInfo{{Name: "orders", Partitions: 3}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockKafkaAdapter{err: fmt.Errorf("kafka error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "kafka-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/kafka/kafka-src/topics", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleKafkaBrokers(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockKafkaAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockKafkaAdapter{brokers: []kafkaadapter.BrokerInfo{{ID: 1, Host: "kafka:9092"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockKafkaAdapter{err: fmt.Errorf("kafka error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "kafka-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/kafka/kafka-src/brokers", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleKafkaConsumerGroups(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockKafkaAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockKafkaAdapter{groups: []kafkaadapter.ConsumerGroupInfo{{GroupID: "payment-group"}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockKafkaAdapter{err: fmt.Errorf("kafka error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "kafka-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/kafka/kafka-src/consumer-groups", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

// --- Elasticsearch tests ---

func TestHandleElasticsearchHealth(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockElasticsearchAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockElasticsearchAdapter{health: &elasticsearchadapter.ClusterHealth{Status: "green", ClusterName: "my-cluster"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockElasticsearchAdapter{err: fmt.Errorf("es error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "es-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/elasticsearch/es-src/health", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestHandleElasticsearchIndices(t *testing.T) {
	tests := []struct {
		name       string
		mock       *mockElasticsearchAdapter
		wantStatus int
	}{
		{
			name:       "success",
			mock:       &mockElasticsearchAdapter{indices: []elasticsearchadapter.IndexInfo{{Name: ".kibana", Docs: 10}}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "nil result normalised",
			mock:       &mockElasticsearchAdapter{indices: nil},
			wantStatus: http.StatusOK,
		},
		{
			name:       "adapter error",
			mock:       &mockElasticsearchAdapter{err: fmt.Errorf("es error")},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := setupDatastoreServer(t, "es-src", tt.mock)
			req := httptest.NewRequest("GET", "/api/v1/elasticsearch/es-src/indices", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body: %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}
