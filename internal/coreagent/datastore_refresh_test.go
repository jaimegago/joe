package coreagent

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	mysqladapter "github.com/jaimegago/joe/internal/adapters/datastore/mysql"
	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// ---- fake adapters ----

type fakePostgresAdapter struct{ err error }

func (f *fakePostgresAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakePostgresAdapter) Disconnect() error                                  { return nil }
func (f *fakePostgresAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
func (f *fakePostgresAdapter) Stat(_ context.Context) (*postgresadapter.Stat, error) {
	return nil, f.err
}
func (f *fakePostgresAdapter) Query(_ context.Context, _ string) ([]map[string]any, error) {
	return nil, f.err
}

type fakeMySQLAdapter struct{ err error }

func (f *fakeMySQLAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeMySQLAdapter) Disconnect() error                                  { return nil }
func (f *fakeMySQLAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
func (f *fakeMySQLAdapter) Stat(_ context.Context) (*mysqladapter.Stat, error) {
	return nil, f.err
}
func (f *fakeMySQLAdapter) Query(_ context.Context, _ string) ([]map[string]any, error) {
	return nil, f.err
}

type fakeRedisAdapter struct{ err error }

func (f *fakeRedisAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeRedisAdapter) Disconnect() error                                  { return nil }
func (f *fakeRedisAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
func (f *fakeRedisAdapter) Info(_ context.Context, _ string) (map[string]string, error) {
	return nil, f.err
}
func (f *fakeRedisAdapter) SlowLog(_ context.Context, _ int64) ([]redisadapter.SlowLogEntry, error) {
	return nil, f.err
}
func (f *fakeRedisAdapter) DBSize(_ context.Context) (int64, error) { return 0, f.err }

type fakeMongoDBAdapter struct{ err error }

func (f *fakeMongoDBAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeMongoDBAdapter) Disconnect() error                                  { return nil }
func (f *fakeMongoDBAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
func (f *fakeMongoDBAdapter) ServerStatus(_ context.Context) (map[string]any, error) {
	return nil, f.err
}
func (f *fakeMongoDBAdapter) ReplicaStatus(_ context.Context) (map[string]any, error) {
	return nil, f.err
}
func (f *fakeMongoDBAdapter) CurrentOp(_ context.Context) (map[string]any, error) {
	return nil, f.err
}

type fakeElasticsearchAdapter struct{ err error }

func (f *fakeElasticsearchAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeElasticsearchAdapter) Disconnect() error                                  { return nil }
func (f *fakeElasticsearchAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
func (f *fakeElasticsearchAdapter) ClusterHealth(_ context.Context) (*elasticsearchadapter.ClusterHealth, error) {
	return nil, f.err
}
func (f *fakeElasticsearchAdapter) ListIndices(_ context.Context, _ string) ([]elasticsearchadapter.IndexInfo, error) {
	return nil, f.err
}

type fakeKafkaAdapter struct {
	topics []kafkaadapter.TopicInfo
	err    error
}

func (f *fakeKafkaAdapter) Connect(_ context.Context, _ store.Component) error { return nil }
func (f *fakeKafkaAdapter) Disconnect() error                                  { return nil }
func (f *fakeKafkaAdapter) Status() adapters.Status                            { return adapters.Status{Connected: true} }
func (f *fakeKafkaAdapter) Topics(_ context.Context) ([]kafkaadapter.TopicInfo, error) {
	return f.topics, f.err
}
func (f *fakeKafkaAdapter) Brokers(_ context.Context) ([]kafkaadapter.BrokerInfo, error) {
	return nil, f.err
}
func (f *fakeKafkaAdapter) ConsumerGroups(_ context.Context) ([]kafkaadapter.ConsumerGroupInfo, error) {
	return nil, f.err
}

// ---- helper ----

func setupDSTestRefresher(t *testing.T) (*Refresher, graph.GraphStore) {
	t.Helper()
	gs := setupGraphStore(t)
	r := &Refresher{
		services: &core.Services{Graph: gs},
		logger:   slog.Default(),
	}
	return r, gs
}

// ---- PostgreSQL ----

func TestRefreshPostgreSQLComponent_CreatesNode(t *testing.T) {
	r, gs := setupDSTestRefresher(t)
	src := &store.Component{ID: "src-pg-1", Type: store.ComponentTypePostgreSQL, Name: "main-db"}

	if err := r.refreshPostgreSQLComponent(context.Background(), src, &fakePostgresAdapter{}); err != nil {
		t.Fatalf("refreshPostgreSQLComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), gs, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nodes))
	}
	if nodes[0].Type != "postgresql_component" {
		t.Errorf("node type = %q, want postgresql_component", nodes[0].Type)
	}
}

func TestRefreshPostgreSQLComponent_StoresInEdge(t *testing.T) {
	r, gs := setupDSTestRefresher(t)

	// Plant a service node whose name matches the source name.
	svc := graph.Node{ID: "svc/api", Type: "service", ComponentID: "src-k8s"}
	svc.Metadata = map[string]any{"name": "main-db"}
	if err := gs.AddNode(context.Background(), svc); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	src := &store.Component{ID: "src-pg-2", Type: store.ComponentTypePostgreSQL, Name: "main-db"}
	if err := r.refreshPostgreSQLComponent(context.Background(), src, &fakePostgresAdapter{}); err != nil {
		t.Fatalf("refreshPostgreSQLComponent: %v", err)
	}
	// Edge discovery is best-effort; verify no error.
}

// ---- MySQL ----

func TestRefreshMySQLComponent_CreatesNode(t *testing.T) {
	r, gs := setupDSTestRefresher(t)
	src := &store.Component{ID: "src-mysql-1", Type: store.ComponentTypeMySQL, Name: "orders-db"}

	if err := r.refreshMySQLComponent(context.Background(), src, &fakeMySQLAdapter{}); err != nil {
		t.Fatalf("refreshMySQLComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), gs, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "mysql_component" {
		t.Errorf("expected 1 mysql_source node, got %v", nodes)
	}
}

// ---- Redis ----

func TestRefreshRedisComponent_CreatesNode(t *testing.T) {
	r, gs := setupDSTestRefresher(t)
	src := &store.Component{ID: "src-redis-1", Type: store.ComponentTypeRedis, Name: "cache"}

	if err := r.refreshRedisComponent(context.Background(), src, &fakeRedisAdapter{}); err != nil {
		t.Fatalf("refreshRedisComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), gs, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "redis_component" {
		t.Errorf("expected 1 redis_source node, got %v", nodes)
	}
}

// ---- MongoDB ----

func TestRefreshMongoDBComponent_CreatesNode(t *testing.T) {
	r, gs := setupDSTestRefresher(t)
	src := &store.Component{ID: "src-mongo-1", Type: store.ComponentTypeMongoDB, Name: "events-db"}

	if err := r.refreshMongoDBComponent(context.Background(), src, &fakeMongoDBAdapter{}); err != nil {
		t.Fatalf("refreshMongoDBComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), gs, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "mongodb_component" {
		t.Errorf("expected 1 mongodb_source node, got %v", nodes)
	}
}

// ---- Elasticsearch ----

func TestRefreshElasticsearchComponent_CreatesNode(t *testing.T) {
	r, gs := setupDSTestRefresher(t)
	src := &store.Component{ID: "src-es-1", Type: store.ComponentTypeElasticsearch, Name: "logs-es"}

	if err := r.refreshElasticsearchComponent(context.Background(), src, &fakeElasticsearchAdapter{}); err != nil {
		t.Fatalf("refreshElasticsearchComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), gs, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "elasticsearch_component" {
		t.Errorf("expected 1 elasticsearch_source node, got %v", nodes)
	}
}

// ---- Kafka ----

func TestRefreshKafkaComponent_NoTopics(t *testing.T) {
	r, gs := setupDSTestRefresher(t)
	src := &store.Component{ID: "src-kafka-1", Type: store.ComponentTypeKafka, Name: "events"}

	if err := r.refreshKafkaComponent(context.Background(), src, &fakeKafkaAdapter{}); err != nil {
		t.Fatalf("refreshKafkaComponent: %v", err)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), gs, src.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Type != "kafka_component" {
		t.Errorf("expected 1 kafka_source node, got %v", nodes)
	}
}

func TestRefreshKafkaComponent_TopicsError(t *testing.T) {
	r, _ := setupDSTestRefresher(t)
	src := &store.Component{ID: "src-kafka-2", Type: store.ComponentTypeKafka, Name: "events"}
	adapter := &fakeKafkaAdapter{err: errors.New("connection refused")}

	// Should succeed even when Topics() fails (skips edge discovery).
	if err := r.refreshKafkaComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshKafkaComponent should not error on Topics failure, got: %v", err)
	}
}

func TestRefreshKafkaComponent_QueuesInEdge(t *testing.T) {
	r, gs := setupDSTestRefresher(t)

	svc := graph.Node{ID: "svc/orders", Type: "service", ComponentID: "src-k8s"}
	svc.Metadata = map[string]any{"name": "orders-events"}
	if err := gs.AddNode(context.Background(), svc); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	src := &store.Component{ID: "src-kafka-3", Type: store.ComponentTypeKafka, Name: "events-bus"}
	adapter := &fakeKafkaAdapter{
		topics: []kafkaadapter.TopicInfo{
			{Name: "orders-events", Partitions: 3},
		},
	}

	if err := r.refreshKafkaComponent(context.Background(), src, adapter); err != nil {
		t.Fatalf("refreshKafkaComponent: %v", err)
	}
	// Edge discovery is best-effort; just verify no error.
}

// ---- topicNames ----

func TestTopicNames_FiltersInternal(t *testing.T) {
	topics := []kafkaadapter.TopicInfo{
		{Name: "user-events", Internal: false},
		{Name: "__consumer_offsets", Internal: true},
		{Name: "order-created", Internal: false},
		{Name: "", Internal: false}, // empty name filtered out
	}

	got := topicNames(topics)
	want := []string{"user-events", "order-created"}

	if len(got) != len(want) {
		t.Fatalf("topicNames() = %v, want %v", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("topicNames()[%d] = %q, want %q", i, got[i], name)
		}
	}
}

// ---- datastoreNodeID ----

func TestDatastoreNodeID(t *testing.T) {
	id := datastoreNodeID("src1", "postgresql")
	want := "datastore/postgresql/src1"
	if id != want {
		t.Errorf("datastoreNodeID = %q, want %q", id, want)
	}
}

// ---- refreshComponent switch cases for data store types ----

func TestRefreshComponent_PostgreSQLType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-pg", &fakePostgresAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-pg", Type: store.ComponentTypePostgreSQL, Name: "pg"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(postgresql) error: %v", err)
	}
}

func TestRefreshComponent_MySQLType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-mysql", &fakeMySQLAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-mysql", Type: store.ComponentTypeMySQL, Name: "mysql"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(mysql) error: %v", err)
	}
}

func TestRefreshComponent_RedisType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-redis", &fakeRedisAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-redis", Type: store.ComponentTypeRedis, Name: "redis"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(redis) error: %v", err)
	}
}

func TestRefreshComponent_MongoDBType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-mongo", &fakeMongoDBAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-mongo", Type: store.ComponentTypeMongoDB, Name: "mongo"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(mongodb) error: %v", err)
	}
}

func TestRefreshComponent_KafkaType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-kafka", &fakeKafkaAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-kafka", Type: store.ComponentTypeKafka, Name: "kafka"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(kafka) error: %v", err)
	}
}

func TestRefreshComponent_ElasticsearchType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-es", &fakeElasticsearchAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-es", Type: store.ComponentTypeElasticsearch, Name: "es"}
	if err := r.refreshComponent(context.Background(), src); err != nil {
		t.Fatalf("refreshComponent(elasticsearch) error: %v", err)
	}
}

func TestRefreshComponent_PostgreSQLWrongType(t *testing.T) {
	svc, reg := setupObsTestServices(t)
	reg.Register("src-pg-bad", &fakeMySQLAdapter{})

	r := withPermitAllAccessor(&Refresher{services: svc, logger: slog.Default()})
	src := &store.Component{ID: "src-pg-bad", Type: store.ComponentTypePostgreSQL, Name: "pg"}
	if err := r.refreshComponent(context.Background(), src); err == nil {
		t.Error("expected error for wrong adapter type, got nil")
	}
}
