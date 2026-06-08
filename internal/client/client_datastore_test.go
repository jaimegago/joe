package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostgresURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/stat"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stat":         map[string]any{"activity": []map[string]any{}},
				"component_id": "pg-1",
			})
		case strings.HasSuffix(r.URL.Path, "/query"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"rows":         []map[string]any{{"count": 42}},
				"component_id": "pg-1",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	stat, err := c.PostgresStat(ctx, "pg-1")
	if err != nil {
		t.Fatalf("PostgresStat: %v", err)
	}
	if stat == nil {
		t.Fatal("PostgresStat returned nil")
	}

	rows, err := c.PostgresQuery(ctx, "pg-1", "SELECT count(*) FROM users")
	if err != nil {
		t.Fatalf("PostgresQuery: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("PostgresQuery: got %d rows, want 1", len(rows))
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/postgresql/pg-1/stat")
	assertContains(t, joined, "/api/v1/postgresql/pg-1/query")
	assertContains(t, joined, "sql=SELECT")
}

func TestMySQLURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/stat"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stat":         map[string]any{"processlist": []map[string]any{}},
				"component_id": "mysql-1",
			})
		case strings.HasSuffix(r.URL.Path, "/query"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"rows":         []map[string]any{},
				"component_id": "mysql-1",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	_, err := c.MySQLStat(ctx, "mysql-1")
	if err != nil {
		t.Fatalf("MySQLStat: %v", err)
	}

	_, err = c.MySQLQuery(ctx, "mysql-1", "SELECT 1")
	if err != nil {
		t.Fatalf("MySQLQuery: %v", err)
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/mysql/mysql-1/stat")
	assertContains(t, joined, "/api/v1/mysql/mysql-1/query")
}

func TestRedisURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/info"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"info":         map[string]string{"redis_version": "7.0.0"},
				"component_id": "redis-1",
			})
		case strings.HasSuffix(r.URL.Path, "/slowlog"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entries":      []map[string]any{},
				"component_id": "redis-1",
			})
		case strings.HasSuffix(r.URL.Path, "/dbsize"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"db_size":      int64(1024),
				"component_id": "redis-1",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	info, err := c.RedisInfo(ctx, "redis-1", "server")
	if err != nil {
		t.Fatalf("RedisInfo: %v", err)
	}
	if info["redis_version"] != "7.0.0" {
		t.Errorf("RedisInfo: unexpected version %q", info["redis_version"])
	}

	_, err = c.RedisInfo(ctx, "redis-1", "")
	if err != nil {
		t.Fatalf("RedisInfo (no section): %v", err)
	}

	_, err = c.RedisSlowLog(ctx, "redis-1", 50)
	if err != nil {
		t.Fatalf("RedisSlowLog: %v", err)
	}

	size, err := c.RedisDBSize(ctx, "redis-1")
	if err != nil {
		t.Fatalf("RedisDBSize: %v", err)
	}
	if size != 1024 {
		t.Errorf("RedisDBSize: got %d, want 1024", size)
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/redis/redis-1/info")
	assertContains(t, joined, "section=server")
	assertContains(t, joined, "/api/v1/redis/redis-1/slowlog?count=50")
	assertContains(t, joined, "/api/v1/redis/redis-1/dbsize")
}

func TestMongoDBURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/server-status"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":       map[string]any{"uptime": 3600},
				"component_id": "mongo-1",
			})
		case strings.HasSuffix(r.URL.Path, "/replica-status"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":       map[string]any{"set": "rs0"},
				"component_id": "mongo-1",
			})
		case strings.HasSuffix(r.URL.Path, "/current-op"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"op":           map[string]any{"inprog": []map[string]any{}},
				"component_id": "mongo-1",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	status, err := c.MongoDBServerStatus(ctx, "mongo-1")
	if err != nil {
		t.Fatalf("MongoDBServerStatus: %v", err)
	}
	if status == nil {
		t.Fatal("MongoDBServerStatus returned nil")
	}

	_, err = c.MongoDBReplicaStatus(ctx, "mongo-1")
	if err != nil {
		t.Fatalf("MongoDBReplicaStatus: %v", err)
	}

	_, err = c.MongoDBCurrentOp(ctx, "mongo-1")
	if err != nil {
		t.Fatalf("MongoDBCurrentOp: %v", err)
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/mongodb/mongo-1/server-status")
	assertContains(t, joined, "/api/v1/mongodb/mongo-1/replica-status")
	assertContains(t, joined, "/api/v1/mongodb/mongo-1/current-op")
}

func TestKafkaURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/topics"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"topics":       []map[string]any{{"name": "orders", "partitions": 3}},
				"count":        1,
				"component_id": "kafka-1",
			})
		case strings.HasSuffix(r.URL.Path, "/brokers"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"brokers":      []map[string]any{{"id": 0, "host": "kafka-0.kafka"}},
				"count":        1,
				"component_id": "kafka-1",
			})
		case strings.HasSuffix(r.URL.Path, "/consumer-groups"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"groups":       []map[string]any{{"group_id": "my-consumer"}},
				"count":        1,
				"component_id": "kafka-1",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	topics, err := c.KafkaTopics(ctx, "kafka-1")
	if err != nil {
		t.Fatalf("KafkaTopics: %v", err)
	}
	if len(topics) != 1 {
		t.Errorf("KafkaTopics: got %d, want 1", len(topics))
	}

	brokers, err := c.KafkaBrokers(ctx, "kafka-1")
	if err != nil {
		t.Fatalf("KafkaBrokers: %v", err)
	}
	if len(brokers) != 1 {
		t.Errorf("KafkaBrokers: got %d, want 1", len(brokers))
	}

	groups, err := c.KafkaConsumerGroups(ctx, "kafka-1")
	if err != nil {
		t.Fatalf("KafkaConsumerGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Errorf("KafkaConsumerGroups: got %d, want 1", len(groups))
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/kafka/kafka-1/topics")
	assertContains(t, joined, "/api/v1/kafka/kafka-1/brokers")
	assertContains(t, joined, "/api/v1/kafka/kafka-1/consumer-groups")
}

func TestElasticsearchURLsAndDecode(t *testing.T) {
	var paths []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.RequestURI)
		w.WriteHeader(http.StatusOK)
		switch {
		case strings.HasSuffix(r.URL.Path, "/health"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"health":       map[string]any{"status": "green", "cluster_name": "es-cluster"},
				"component_id": "es-1",
			})
		case strings.HasSuffix(r.URL.Path, "/indices"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"indices":      []map[string]any{{"name": "my-index", "status": "open"}},
				"count":        1,
				"component_id": "es-1",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer ts.Close()

	c := New(ts.URL)
	ctx := context.Background()

	health, err := c.ElasticsearchHealth(ctx, "es-1")
	if err != nil {
		t.Fatalf("ElasticsearchHealth: %v", err)
	}
	if health == nil {
		t.Fatal("ElasticsearchHealth returned nil")
	}

	indices, err := c.ElasticsearchIndices(ctx, "es-1", "")
	if err != nil {
		t.Fatalf("ElasticsearchIndices: %v", err)
	}
	if len(indices) != 1 {
		t.Errorf("ElasticsearchIndices: got %d, want 1", len(indices))
	}

	_, err = c.ElasticsearchIndices(ctx, "es-1", "my-*")
	if err != nil {
		t.Fatalf("ElasticsearchIndices with pattern: %v", err)
	}

	joined := strings.Join(paths, "\n")
	assertContains(t, joined, "/api/v1/elasticsearch/es-1/health")
	assertContains(t, joined, "/api/v1/elasticsearch/es-1/indices")
	assertContains(t, joined, "pattern=my")
}
