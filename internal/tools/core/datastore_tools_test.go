package core_test

import (
	"context"
	"errors"
	"testing"

	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	mysqladapter "github.com/jaimegago/joe/internal/adapters/datastore/mysql"
	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
	"github.com/jaimegago/joe/internal/tools/core"
)

// --- PostgreSQL mocks ---

type mockPostgresClient struct {
	stat *postgresadapter.Stat
	rows []map[string]any
	err  error
}

func (m *mockPostgresClient) PostgresStat(_ context.Context, _ string) (*postgresadapter.Stat, error) {
	return m.stat, m.err
}

func (m *mockPostgresClient) PostgresQuery(_ context.Context, _, _ string) ([]map[string]any, error) {
	return m.rows, m.err
}

// --- PostgresStatTool tests ---

func TestPostgresStatTool_Name(t *testing.T) {
	tool := core.NewPostgresStatTool(&mockPostgresClient{})
	if tool.Name() != "postgres_stat" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "postgres_stat")
	}
}

func TestPostgresStatTool_Description(t *testing.T) {
	tool := core.NewPostgresStatTool(&mockPostgresClient{})
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestPostgresStatTool_Parameters(t *testing.T) {
	tool := core.NewPostgresStatTool(&mockPostgresClient{})
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["component_id"]; !ok {
		t.Error("Parameters() missing component_id")
	}
}

func TestPostgresStatTool_Execute_Success(t *testing.T) {
	stat := &postgresadapter.Stat{
		Activity: []postgresadapter.ActivityRow{
			{PID: 1, State: "active", Query: "SELECT 1"},
		},
	}
	tool := core.NewPostgresStatTool(&mockPostgresClient{stat: stat})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "pg-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("result is not map[string]any")
	}
	if m["component_id"] != "pg-1" {
		t.Errorf("component_id = %v, want pg-1", m["component_id"])
	}
	if m["stat"] == nil {
		t.Error("stat should not be nil")
	}
}

func TestPostgresStatTool_Execute_MissingComponentID(t *testing.T) {
	tool := core.NewPostgresStatTool(&mockPostgresClient{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing component_id, got nil")
	}
}

func TestPostgresStatTool_Execute_Error(t *testing.T) {
	tool := core.NewPostgresStatTool(&mockPostgresClient{err: errors.New("connection refused")})
	_, err := tool.Execute(context.Background(), map[string]any{"component_id": "pg-1"})
	if err == nil {
		t.Error("expected error from stat, got nil")
	}
}

// --- PostgresQueryTool tests ---

func TestPostgresQueryTool_Name(t *testing.T) {
	tool := core.NewPostgresQueryTool(&mockPostgresClient{})
	if tool.Name() != "postgres_query" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "postgres_query")
	}
}

func TestPostgresQueryTool_Execute_Success(t *testing.T) {
	rows := []map[string]any{
		{"id": 1, "name": "payment"},
	}
	tool := core.NewPostgresQueryTool(&mockPostgresClient{rows: rows})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "pg-1",
		"query":        "SELECT id, name FROM services",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["count"] != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}
	if m["query"] != "SELECT id, name FROM services" {
		t.Errorf("query = %v, want SELECT id, name FROM services", m["query"])
	}
}

func TestPostgresQueryTool_Execute_MissingComponentID(t *testing.T) {
	tool := core.NewPostgresQueryTool(&mockPostgresClient{})
	_, err := tool.Execute(context.Background(), map[string]any{"query": "SELECT 1"})
	if err == nil {
		t.Error("expected error for missing component_id, got nil")
	}
}

func TestPostgresQueryTool_Execute_MissingQuery(t *testing.T) {
	tool := core.NewPostgresQueryTool(&mockPostgresClient{})
	_, err := tool.Execute(context.Background(), map[string]any{"component_id": "pg-1"})
	if err == nil {
		t.Error("expected error for missing query, got nil")
	}
}

func TestPostgresQueryTool_Execute_Error(t *testing.T) {
	tool := core.NewPostgresQueryTool(&mockPostgresClient{err: errors.New("permission denied")})
	_, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "pg-1",
		"query":        "SELECT 1",
	})
	if err == nil {
		t.Error("expected error from query, got nil")
	}
}

func TestPostgresQueryTool_Execute_NilRows(t *testing.T) {
	tool := core.NewPostgresQueryTool(&mockPostgresClient{rows: nil})
	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "pg-1",
		"query":        "SELECT 1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["count"] != 0 {
		t.Errorf("count = %v, want 0 for nil rows", m["count"])
	}
}

// --- MySQL mocks ---

type mockMySQLClient struct {
	stat *mysqladapter.Stat
	rows []map[string]any
	err  error
}

func (m *mockMySQLClient) MySQLStat(_ context.Context, _ string) (*mysqladapter.Stat, error) {
	return m.stat, m.err
}

func (m *mockMySQLClient) MySQLQuery(_ context.Context, _, _ string) ([]map[string]any, error) {
	return m.rows, m.err
}

// --- MySQLStatTool tests ---

func TestMySQLStatTool_Name(t *testing.T) {
	tool := core.NewMySQLStatTool(&mockMySQLClient{})
	if tool.Name() != "mysql_stat" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "mysql_stat")
	}
}

func TestMySQLStatTool_Description(t *testing.T) {
	tool := core.NewMySQLStatTool(&mockMySQLClient{})
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestMySQLStatTool_Parameters(t *testing.T) {
	tool := core.NewMySQLStatTool(&mockMySQLClient{})
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["component_id"]; !ok {
		t.Error("Parameters() missing component_id")
	}
}

func TestMySQLStatTool_Execute_Success(t *testing.T) {
	stat := &mysqladapter.Stat{
		Processes: []mysqladapter.ProcessRow{
			{ID: 1, User: "app", Command: "Query"},
		},
	}
	tool := core.NewMySQLStatTool(&mockMySQLClient{stat: stat})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "mysql-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["component_id"] != "mysql-1" {
		t.Errorf("component_id = %v, want mysql-1", m["component_id"])
	}
}

func TestMySQLStatTool_Execute_MissingComponentID(t *testing.T) {
	tool := core.NewMySQLStatTool(&mockMySQLClient{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing component_id, got nil")
	}
}

func TestMySQLStatTool_Execute_Error(t *testing.T) {
	tool := core.NewMySQLStatTool(&mockMySQLClient{err: errors.New("connection refused")})
	_, err := tool.Execute(context.Background(), map[string]any{"component_id": "mysql-1"})
	if err == nil {
		t.Error("expected error from stat, got nil")
	}
}

// --- MySQLQueryTool tests ---

func TestMySQLQueryTool_Name(t *testing.T) {
	tool := core.NewMySQLQueryTool(&mockMySQLClient{})
	if tool.Name() != "mysql_query" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "mysql_query")
	}
}

func TestMySQLQueryTool_Execute_Success(t *testing.T) {
	rows := []map[string]any{{"count": 42}}
	tool := core.NewMySQLQueryTool(&mockMySQLClient{rows: rows})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "mysql-1",
		"query":        "SELECT COUNT(*) as count FROM orders",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["count"] != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}
}

func TestMySQLQueryTool_Execute_MissingQuery(t *testing.T) {
	tool := core.NewMySQLQueryTool(&mockMySQLClient{})
	_, err := tool.Execute(context.Background(), map[string]any{"component_id": "mysql-1"})
	if err == nil {
		t.Error("expected error for missing query, got nil")
	}
}

func TestMySQLQueryTool_Execute_Error(t *testing.T) {
	tool := core.NewMySQLQueryTool(&mockMySQLClient{err: errors.New("table not found")})
	_, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "mysql-1",
		"query":        "SELECT * FROM nonexistent",
	})
	if err == nil {
		t.Error("expected error from query, got nil")
	}
}

// --- Redis mocks ---

type mockRedisClient struct {
	info    map[string]string
	entries []redisadapter.SlowLogEntry
	dbSize  int64
	err     error
}

func (m *mockRedisClient) RedisInfo(_ context.Context, _, _ string) (map[string]string, error) {
	return m.info, m.err
}

func (m *mockRedisClient) RedisSlowLog(_ context.Context, _ string, _ int64) ([]redisadapter.SlowLogEntry, error) {
	return m.entries, m.err
}

func (m *mockRedisClient) RedisDBSize(_ context.Context, _ string) (int64, error) {
	return m.dbSize, m.err
}

// --- RedisInfoTool tests ---

func TestRedisInfoTool_Name(t *testing.T) {
	tool := core.NewRedisInfoTool(&mockRedisClient{})
	if tool.Name() != "redis_info" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "redis_info")
	}
}

func TestRedisInfoTool_Description(t *testing.T) {
	tool := core.NewRedisInfoTool(&mockRedisClient{})
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestRedisInfoTool_Parameters(t *testing.T) {
	tool := core.NewRedisInfoTool(&mockRedisClient{})
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["component_id"]; !ok {
		t.Error("Parameters() missing component_id")
	}
	if _, ok := params.Properties["section"]; !ok {
		t.Error("Parameters() missing section")
	}
}

func TestRedisInfoTool_Execute_Success(t *testing.T) {
	info := map[string]string{
		"used_memory":       "1048576",
		"connected_clients": "10",
	}
	tool := core.NewRedisInfoTool(&mockRedisClient{info: info})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "redis-1",
		"section":      "memory",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["component_id"] != "redis-1" {
		t.Errorf("component_id = %v, want redis-1", m["component_id"])
	}
	if m["section"] != "memory" {
		t.Errorf("section = %v, want memory", m["section"])
	}
}

func TestRedisInfoTool_Execute_NoSection(t *testing.T) {
	tool := core.NewRedisInfoTool(&mockRedisClient{info: map[string]string{"uptime_in_seconds": "3600"}})
	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "redis-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["section"] != "" {
		t.Errorf("section = %v, want empty string", m["section"])
	}
}

func TestRedisInfoTool_Execute_MissingComponentID(t *testing.T) {
	tool := core.NewRedisInfoTool(&mockRedisClient{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing component_id, got nil")
	}
}

func TestRedisInfoTool_Execute_Error(t *testing.T) {
	tool := core.NewRedisInfoTool(&mockRedisClient{err: errors.New("connection refused")})
	_, err := tool.Execute(context.Background(), map[string]any{"component_id": "redis-1"})
	if err == nil {
		t.Error("expected error from info, got nil")
	}
}

// --- RedisSlowLogTool tests ---

func TestRedisSlowLogTool_Name(t *testing.T) {
	tool := core.NewRedisSlowLogTool(&mockRedisClient{})
	if tool.Name() != "redis_slowlog" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "redis_slowlog")
	}
}

func TestRedisSlowLogTool_Execute_Success(t *testing.T) {
	entries := []redisadapter.SlowLogEntry{
		{ID: 1, ExecutionTimeUS: 12345, Command: []string{"GET", "mykey"}},
	}
	tool := core.NewRedisSlowLogTool(&mockRedisClient{entries: entries})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "redis-1",
		"count":        float64(5),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["count"] != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}
}

func TestRedisSlowLogTool_Execute_DefaultCount(t *testing.T) {
	tool := core.NewRedisSlowLogTool(&mockRedisClient{entries: []redisadapter.SlowLogEntry{}})
	_, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "redis-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestRedisSlowLogTool_Execute_MissingComponentID(t *testing.T) {
	tool := core.NewRedisSlowLogTool(&mockRedisClient{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing component_id, got nil")
	}
}

func TestRedisSlowLogTool_Execute_Error(t *testing.T) {
	tool := core.NewRedisSlowLogTool(&mockRedisClient{err: errors.New("timeout")})
	_, err := tool.Execute(context.Background(), map[string]any{"component_id": "redis-1"})
	if err == nil {
		t.Error("expected error from slowlog, got nil")
	}
}

// --- MongoDB mocks ---

type mockMongoDBClient struct {
	serverStatus  map[string]any
	replicaStatus map[string]any
	currentOp     map[string]any
	err           error
}

func (m *mockMongoDBClient) MongoDBServerStatus(_ context.Context, _ string) (map[string]any, error) {
	return m.serverStatus, m.err
}

func (m *mockMongoDBClient) MongoDBReplicaStatus(_ context.Context, _ string) (map[string]any, error) {
	return m.replicaStatus, m.err
}

func (m *mockMongoDBClient) MongoDBCurrentOp(_ context.Context, _ string) (map[string]any, error) {
	return m.currentOp, m.err
}

// --- MongoDBStatTool tests ---

func TestMongoDBStatTool_Name(t *testing.T) {
	tool := core.NewMongoDBStatTool(&mockMongoDBClient{})
	if tool.Name() != "mongodb_stat" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "mongodb_stat")
	}
}

func TestMongoDBStatTool_Description(t *testing.T) {
	tool := core.NewMongoDBStatTool(&mockMongoDBClient{})
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestMongoDBStatTool_Parameters(t *testing.T) {
	tool := core.NewMongoDBStatTool(&mockMongoDBClient{})
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["component_id"]; !ok {
		t.Error("Parameters() missing component_id")
	}
	if _, ok := params.Properties["action"]; !ok {
		t.Error("Parameters() missing action")
	}
}

func TestMongoDBStatTool_Execute_ServerStatus(t *testing.T) {
	status := map[string]any{"connections": map[string]any{"current": 42}}
	tool := core.NewMongoDBStatTool(&mockMongoDBClient{serverStatus: status})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "mongo-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["component_id"] != "mongo-1" {
		t.Errorf("component_id = %v, want mongo-1", m["component_id"])
	}
	if m["action"] != "server_status" {
		t.Errorf("action = %v, want server_status", m["action"])
	}
}

func TestMongoDBStatTool_Execute_ReplicaStatus(t *testing.T) {
	status := map[string]any{"set": "rs0", "myState": 1}
	tool := core.NewMongoDBStatTool(&mockMongoDBClient{replicaStatus: status})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "mongo-1",
		"action":       "replica_status",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["action"] != "replica_status" {
		t.Errorf("action = %v, want replica_status", m["action"])
	}
}

func TestMongoDBStatTool_Execute_CurrentOp(t *testing.T) {
	op := map[string]any{"inprog": []any{}}
	tool := core.NewMongoDBStatTool(&mockMongoDBClient{currentOp: op})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "mongo-1",
		"action":       "current_op",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["action"] != "current_op" {
		t.Errorf("action = %v, want current_op", m["action"])
	}
}

func TestMongoDBStatTool_Execute_UnknownAction(t *testing.T) {
	tool := core.NewMongoDBStatTool(&mockMongoDBClient{})
	_, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "mongo-1",
		"action":       "invalid_action",
	})
	if err == nil {
		t.Error("expected error for unknown action, got nil")
	}
}

func TestMongoDBStatTool_Execute_MissingComponentID(t *testing.T) {
	tool := core.NewMongoDBStatTool(&mockMongoDBClient{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing component_id, got nil")
	}
}

func TestMongoDBStatTool_Execute_Error(t *testing.T) {
	tool := core.NewMongoDBStatTool(&mockMongoDBClient{err: errors.New("connection refused")})
	_, err := tool.Execute(context.Background(), map[string]any{"component_id": "mongo-1"})
	if err == nil {
		t.Error("expected error from server_status, got nil")
	}
}

// --- Kafka mocks ---

type mockKafkaClient struct {
	topics  []kafkaadapter.TopicInfo
	brokers []kafkaadapter.BrokerInfo
	groups  []kafkaadapter.ConsumerGroupInfo
	err     error
}

func (m *mockKafkaClient) KafkaTopics(_ context.Context, _ string) ([]kafkaadapter.TopicInfo, error) {
	return m.topics, m.err
}

func (m *mockKafkaClient) KafkaBrokers(_ context.Context, _ string) ([]kafkaadapter.BrokerInfo, error) {
	return m.brokers, m.err
}

func (m *mockKafkaClient) KafkaConsumerGroups(_ context.Context, _ string) ([]kafkaadapter.ConsumerGroupInfo, error) {
	return m.groups, m.err
}

// --- KafkaTopicsTool tests ---

func TestKafkaTopicsTool_Name(t *testing.T) {
	tool := core.NewKafkaTopicsTool(&mockKafkaClient{})
	if tool.Name() != "kafka_topics" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "kafka_topics")
	}
}

func TestKafkaTopicsTool_Description(t *testing.T) {
	tool := core.NewKafkaTopicsTool(&mockKafkaClient{})
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestKafkaTopicsTool_Parameters(t *testing.T) {
	tool := core.NewKafkaTopicsTool(&mockKafkaClient{})
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["component_id"]; !ok {
		t.Error("Parameters() missing component_id")
	}
}

func TestKafkaTopicsTool_Execute_Success(t *testing.T) {
	topics := []kafkaadapter.TopicInfo{
		{Name: "orders", Partitions: 3},
		{Name: "payments", Partitions: 6},
	}
	tool := core.NewKafkaTopicsTool(&mockKafkaClient{topics: topics})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "kafka-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["count"] != 2 {
		t.Errorf("count = %v, want 2", m["count"])
	}
}

func TestKafkaTopicsTool_Execute_MissingComponentID(t *testing.T) {
	tool := core.NewKafkaTopicsTool(&mockKafkaClient{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing component_id, got nil")
	}
}

func TestKafkaTopicsTool_Execute_Error(t *testing.T) {
	tool := core.NewKafkaTopicsTool(&mockKafkaClient{err: errors.New("broker unavailable")})
	_, err := tool.Execute(context.Background(), map[string]any{"component_id": "kafka-1"})
	if err == nil {
		t.Error("expected error from topics, got nil")
	}
}

// --- KafkaBrokersTool tests ---

func TestKafkaBrokersTool_Name(t *testing.T) {
	tool := core.NewKafkaBrokersTool(&mockKafkaClient{})
	if tool.Name() != "kafka_brokers" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "kafka_brokers")
	}
}

func TestKafkaBrokersTool_Execute_Success(t *testing.T) {
	brokers := []kafkaadapter.BrokerInfo{
		{ID: 1, Host: "kafka-0.kafka.svc", Port: 9092},
		{ID: 2, Host: "kafka-1.kafka.svc", Port: 9092},
	}
	tool := core.NewKafkaBrokersTool(&mockKafkaClient{brokers: brokers})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "kafka-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["count"] != 2 {
		t.Errorf("count = %v, want 2", m["count"])
	}
}

func TestKafkaBrokersTool_Execute_MissingComponentID(t *testing.T) {
	tool := core.NewKafkaBrokersTool(&mockKafkaClient{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing component_id, got nil")
	}
}

func TestKafkaBrokersTool_Execute_Error(t *testing.T) {
	tool := core.NewKafkaBrokersTool(&mockKafkaClient{err: errors.New("timeout")})
	_, err := tool.Execute(context.Background(), map[string]any{"component_id": "kafka-1"})
	if err == nil {
		t.Error("expected error from brokers, got nil")
	}
}

// --- KafkaConsumerGroupsTool tests ---

func TestKafkaConsumerGroupsTool_Name(t *testing.T) {
	tool := core.NewKafkaConsumerGroupsTool(&mockKafkaClient{})
	if tool.Name() != "kafka_consumers" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "kafka_consumers")
	}
}

func TestKafkaConsumerGroupsTool_Execute_Success(t *testing.T) {
	groups := []kafkaadapter.ConsumerGroupInfo{
		{
			GroupID: "order-processor",
			State:   "Stable",
		},
	}
	tool := core.NewKafkaConsumerGroupsTool(&mockKafkaClient{groups: groups})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "kafka-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["count"] != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}
}

func TestKafkaConsumerGroupsTool_Execute_MissingComponentID(t *testing.T) {
	tool := core.NewKafkaConsumerGroupsTool(&mockKafkaClient{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing component_id, got nil")
	}
}

func TestKafkaConsumerGroupsTool_Execute_Error(t *testing.T) {
	tool := core.NewKafkaConsumerGroupsTool(&mockKafkaClient{err: errors.New("connection refused")})
	_, err := tool.Execute(context.Background(), map[string]any{"component_id": "kafka-1"})
	if err == nil {
		t.Error("expected error from consumer groups, got nil")
	}
}

// --- Elasticsearch mocks ---

type mockElasticsearchClient struct {
	health  *elasticsearchadapter.ClusterHealth
	indices []elasticsearchadapter.IndexInfo
	err     error
}

func (m *mockElasticsearchClient) ElasticsearchHealth(_ context.Context, _ string) (*elasticsearchadapter.ClusterHealth, error) {
	return m.health, m.err
}

func (m *mockElasticsearchClient) ElasticsearchIndices(_ context.Context, _, _ string) ([]elasticsearchadapter.IndexInfo, error) {
	return m.indices, m.err
}

// --- ElasticsearchHealthTool tests ---

func TestElasticsearchHealthTool_Name(t *testing.T) {
	tool := core.NewElasticsearchHealthTool(&mockElasticsearchClient{})
	if tool.Name() != "elasticsearch_health" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "elasticsearch_health")
	}
}

func TestElasticsearchHealthTool_Description(t *testing.T) {
	tool := core.NewElasticsearchHealthTool(&mockElasticsearchClient{})
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestElasticsearchHealthTool_Parameters(t *testing.T) {
	tool := core.NewElasticsearchHealthTool(&mockElasticsearchClient{})
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["component_id"]; !ok {
		t.Error("Parameters() missing component_id")
	}
}

func TestElasticsearchHealthTool_Execute_Success(t *testing.T) {
	health := &elasticsearchadapter.ClusterHealth{
		ClusterName:      "production",
		Status:           "green",
		Nodes:            3,
		Shards:           30,
		UnassignedShards: 0,
	}
	tool := core.NewElasticsearchHealthTool(&mockElasticsearchClient{health: health})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "es-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["component_id"] != "es-1" {
		t.Errorf("component_id = %v, want es-1", m["component_id"])
	}
	if m["health"] == nil {
		t.Error("health should not be nil")
	}
}

func TestElasticsearchHealthTool_Execute_MissingComponentID(t *testing.T) {
	tool := core.NewElasticsearchHealthTool(&mockElasticsearchClient{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing component_id, got nil")
	}
}

func TestElasticsearchHealthTool_Execute_Error(t *testing.T) {
	tool := core.NewElasticsearchHealthTool(&mockElasticsearchClient{err: errors.New("cluster unreachable")})
	_, err := tool.Execute(context.Background(), map[string]any{"component_id": "es-1"})
	if err == nil {
		t.Error("expected error from health, got nil")
	}
}

// --- ElasticsearchIndicesTool tests ---

func TestElasticsearchIndicesTool_Name(t *testing.T) {
	tool := core.NewElasticsearchIndicesTool(&mockElasticsearchClient{})
	if tool.Name() != "elasticsearch_indices" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "elasticsearch_indices")
	}
}

func TestElasticsearchIndicesTool_Description(t *testing.T) {
	tool := core.NewElasticsearchIndicesTool(&mockElasticsearchClient{})
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestElasticsearchIndicesTool_Parameters(t *testing.T) {
	tool := core.NewElasticsearchIndicesTool(&mockElasticsearchClient{})
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["component_id"]; !ok {
		t.Error("Parameters() missing component_id")
	}
	if _, ok := params.Properties["pattern"]; !ok {
		t.Error("Parameters() missing pattern")
	}
}

func TestElasticsearchIndicesTool_Execute_Success(t *testing.T) {
	indices := []elasticsearchadapter.IndexInfo{
		{Name: "logs-2024-01-01", Health: "green", Status: "open", Docs: 10000, StoreSize: "500mb"},
		{Name: "logs-2024-01-02", Health: "yellow", Status: "open", Docs: 5000, StoreSize: "250mb"},
	}
	tool := core.NewElasticsearchIndicesTool(&mockElasticsearchClient{indices: indices})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "es-1",
		"pattern":      "logs-*",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["count"] != 2 {
		t.Errorf("count = %v, want 2", m["count"])
	}
	if m["pattern"] != "logs-*" {
		t.Errorf("pattern = %v, want logs-*", m["pattern"])
	}
}

func TestElasticsearchIndicesTool_Execute_NoPattern(t *testing.T) {
	indices := []elasticsearchadapter.IndexInfo{{Name: ".kibana", Health: "green", Status: "open"}}
	tool := core.NewElasticsearchIndicesTool(&mockElasticsearchClient{indices: indices})

	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "es-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	m := result.(map[string]any)
	if m["pattern"] != "" {
		t.Errorf("pattern = %v, want empty string", m["pattern"])
	}
}

func TestElasticsearchIndicesTool_Execute_MissingComponentID(t *testing.T) {
	tool := core.NewElasticsearchIndicesTool(&mockElasticsearchClient{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing component_id, got nil")
	}
}

func TestElasticsearchIndicesTool_Execute_Error(t *testing.T) {
	tool := core.NewElasticsearchIndicesTool(&mockElasticsearchClient{err: errors.New("cluster unreachable")})
	_, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "es-1",
		"pattern":      "logs-*",
	})
	if err == nil {
		t.Error("expected error from indices, got nil")
	}
}

func TestElasticsearchIndicesTool_Execute_NilIndices(t *testing.T) {
	tool := core.NewElasticsearchIndicesTool(&mockElasticsearchClient{indices: nil})
	result, err := tool.Execute(context.Background(), map[string]any{
		"component_id": "es-1",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["count"] != 0 {
		t.Errorf("count = %v, want 0 for nil indices", m["count"])
	}
}
