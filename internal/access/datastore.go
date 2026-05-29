package access

import (
	"context"

	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	mongodbadapter "github.com/jaimegago/joe/internal/adapters/datastore/mongodb"
	mysqladapter "github.com/jaimegago/joe/internal/adapters/datastore/mysql"
	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
	"github.com/jaimegago/joe/internal/rbac"
)

// --- PostgreSQL ---

func (a *Accessor) PostgresStat(ctx context.Context, principal rbac.Principal, sourceID string) (*postgresadapter.Stat, error) {
	ad, err := guard[postgresadapter.PostgreSQLAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "postgres")
	if err != nil {
		return nil, err
	}
	return ad.Stat(ctx)
}

func (a *Accessor) PostgresQuery(ctx context.Context, principal rbac.Principal, sourceID, rawSQL string) ([]map[string]any, error) {
	ad, err := guard[postgresadapter.PostgreSQLAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "postgres")
	if err != nil {
		return nil, err
	}
	return ad.Query(ctx, rawSQL)
}

// --- MySQL ---

func (a *Accessor) MySQLStat(ctx context.Context, principal rbac.Principal, sourceID string) (*mysqladapter.Stat, error) {
	ad, err := guard[mysqladapter.MySQLAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "mysql")
	if err != nil {
		return nil, err
	}
	return ad.Stat(ctx)
}

func (a *Accessor) MySQLQuery(ctx context.Context, principal rbac.Principal, sourceID, rawSQL string) ([]map[string]any, error) {
	ad, err := guard[mysqladapter.MySQLAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "mysql")
	if err != nil {
		return nil, err
	}
	return ad.Query(ctx, rawSQL)
}

// --- Redis ---

func (a *Accessor) RedisInfo(ctx context.Context, principal rbac.Principal, sourceID, section string) (map[string]string, error) {
	ad, err := guard[redisadapter.RedisAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "redis")
	if err != nil {
		return nil, err
	}
	return ad.Info(ctx, section)
}

func (a *Accessor) RedisSlowLog(ctx context.Context, principal rbac.Principal, sourceID string, count int64) ([]redisadapter.SlowLogEntry, error) {
	ad, err := guard[redisadapter.RedisAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "redis")
	if err != nil {
		return nil, err
	}
	return ad.SlowLog(ctx, count)
}

func (a *Accessor) RedisDBSize(ctx context.Context, principal rbac.Principal, sourceID string) (int64, error) {
	ad, err := guard[redisadapter.RedisAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "redis")
	if err != nil {
		return 0, err
	}
	return ad.DBSize(ctx)
}

// --- MongoDB ---

func (a *Accessor) MongoDBServerStatus(ctx context.Context, principal rbac.Principal, sourceID string) (map[string]any, error) {
	ad, err := guard[mongodbadapter.MongoDBAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "mongodb")
	if err != nil {
		return nil, err
	}
	return ad.ServerStatus(ctx)
}

func (a *Accessor) MongoDBReplicaStatus(ctx context.Context, principal rbac.Principal, sourceID string) (map[string]any, error) {
	ad, err := guard[mongodbadapter.MongoDBAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "mongodb")
	if err != nil {
		return nil, err
	}
	return ad.ReplicaStatus(ctx)
}

func (a *Accessor) MongoDBCurrentOp(ctx context.Context, principal rbac.Principal, sourceID string) (map[string]any, error) {
	ad, err := guard[mongodbadapter.MongoDBAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "mongodb")
	if err != nil {
		return nil, err
	}
	return ad.CurrentOp(ctx)
}

// --- Kafka ---

func (a *Accessor) KafkaTopics(ctx context.Context, principal rbac.Principal, sourceID string) ([]kafkaadapter.TopicInfo, error) {
	ad, err := guard[kafkaadapter.KafkaAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "kafka")
	if err != nil {
		return nil, err
	}
	return ad.Topics(ctx)
}

func (a *Accessor) KafkaBrokers(ctx context.Context, principal rbac.Principal, sourceID string) ([]kafkaadapter.BrokerInfo, error) {
	ad, err := guard[kafkaadapter.KafkaAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "kafka")
	if err != nil {
		return nil, err
	}
	return ad.Brokers(ctx)
}

func (a *Accessor) KafkaConsumerGroups(ctx context.Context, principal rbac.Principal, sourceID string) ([]kafkaadapter.ConsumerGroupInfo, error) {
	ad, err := guard[kafkaadapter.KafkaAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "kafka")
	if err != nil {
		return nil, err
	}
	return ad.ConsumerGroups(ctx)
}

// --- Elasticsearch ---

func (a *Accessor) ElasticsearchClusterHealth(ctx context.Context, principal rbac.Principal, sourceID string) (*elasticsearchadapter.ClusterHealth, error) {
	ad, err := guard[elasticsearchadapter.ElasticsearchAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "elasticsearch")
	if err != nil {
		return nil, err
	}
	return ad.ClusterHealth(ctx)
}

func (a *Accessor) ElasticsearchListIndices(ctx context.Context, principal rbac.Principal, sourceID, pattern string) ([]elasticsearchadapter.IndexInfo, error) {
	ad, err := guard[elasticsearchadapter.ElasticsearchAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "elasticsearch")
	if err != nil {
		return nil, err
	}
	return ad.ListIndices(ctx, pattern)
}
