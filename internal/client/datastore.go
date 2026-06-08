package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	mysqladapter "github.com/jaimegago/joe/internal/adapters/datastore/mysql"
	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
)

// --- PostgreSQL ---

// PostgresStat returns pg_stat_* statistics from a PostgreSQL source.
func (c *Client) PostgresStat(ctx context.Context, sourceID string) (*postgresadapter.Stat, error) {
	u := fmt.Sprintf("%s%s/%s/stat",
		c.baseURL, apiPostgresBasePath, url.PathEscape(sourceID))

	var result struct {
		Stat        *postgresadapter.Stat `json:"stat"`
		ComponentID string                `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "postgres stat"); err != nil {
		return nil, err
	}

	return result.Stat, nil
}

// PostgresQuery executes a read-only SQL query against a PostgreSQL source.
func (c *Client) PostgresQuery(ctx context.Context, sourceID, query string) ([]map[string]any, error) {
	u := fmt.Sprintf("%s%s/%s/query?sql=%s",
		c.baseURL, apiPostgresBasePath,
		url.PathEscape(sourceID), url.QueryEscape(query))

	var result struct {
		Rows        []map[string]any `json:"rows"`
		ComponentID string           `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "postgres query"); err != nil {
		return nil, err
	}

	return result.Rows, nil
}

// --- MySQL ---

// MySQLStat returns status statistics from a MySQL source.
func (c *Client) MySQLStat(ctx context.Context, sourceID string) (*mysqladapter.Stat, error) {
	u := fmt.Sprintf("%s%s/%s/stat",
		c.baseURL, apiMySQLBasePath, url.PathEscape(sourceID))

	var result struct {
		Stat        *mysqladapter.Stat `json:"stat"`
		ComponentID string             `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "mysql stat"); err != nil {
		return nil, err
	}

	return result.Stat, nil
}

// MySQLQuery executes a read-only SQL query against a MySQL source.
func (c *Client) MySQLQuery(ctx context.Context, sourceID, query string) ([]map[string]any, error) {
	u := fmt.Sprintf("%s%s/%s/query?sql=%s",
		c.baseURL, apiMySQLBasePath,
		url.PathEscape(sourceID), url.QueryEscape(query))

	var result struct {
		Rows        []map[string]any `json:"rows"`
		ComponentID string           `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "mysql query"); err != nil {
		return nil, err
	}

	return result.Rows, nil
}

// --- Redis ---

// RedisInfo returns Redis INFO output for the given section.
// section can be: server, clients, memory, stats, replication, cpu, all, or empty for default.
func (c *Client) RedisInfo(ctx context.Context, sourceID, section string) (map[string]string, error) {
	u := fmt.Sprintf("%s%s/%s/info",
		c.baseURL, apiRedisBasePath, url.PathEscape(sourceID))
	if section != "" {
		u += "?section=" + url.QueryEscape(section)
	}

	var result struct {
		Info        map[string]string `json:"info"`
		ComponentID string            `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "redis info"); err != nil {
		return nil, err
	}

	return result.Info, nil
}

// RedisSlowLog returns the most recent slow log entries from a Redis source.
func (c *Client) RedisSlowLog(ctx context.Context, sourceID string, count int64) ([]redisadapter.SlowLogEntry, error) {
	u := fmt.Sprintf("%s%s/%s/slowlog?count=%s",
		c.baseURL, apiRedisBasePath,
		url.PathEscape(sourceID), strconv.FormatInt(count, 10))

	var result struct {
		Entries     []redisadapter.SlowLogEntry `json:"entries"`
		ComponentID string                      `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "redis slowlog"); err != nil {
		return nil, err
	}

	return result.Entries, nil
}

// RedisDBSize returns the number of keys in the current Redis database.
func (c *Client) RedisDBSize(ctx context.Context, sourceID string) (int64, error) {
	u := fmt.Sprintf("%s%s/%s/dbsize",
		c.baseURL, apiRedisBasePath, url.PathEscape(sourceID))

	var result struct {
		DBSize      int64  `json:"db_size"`
		ComponentID string `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "redis dbsize"); err != nil {
		return 0, err
	}

	return result.DBSize, nil
}

// --- MongoDB ---

// MongoDBServerStatus returns the db.serverStatus() result from a MongoDB source.
func (c *Client) MongoDBServerStatus(ctx context.Context, sourceID string) (map[string]any, error) {
	u := fmt.Sprintf("%s%s/%s/server-status",
		c.baseURL, apiMongoDBBasePath, url.PathEscape(sourceID))

	var result struct {
		Status      map[string]any `json:"status"`
		ComponentID string         `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "mongodb server status"); err != nil {
		return nil, err
	}

	return result.Status, nil
}

// MongoDBReplicaStatus returns the rs.status() result from a MongoDB source.
func (c *Client) MongoDBReplicaStatus(ctx context.Context, sourceID string) (map[string]any, error) {
	u := fmt.Sprintf("%s%s/%s/replica-status",
		c.baseURL, apiMongoDBBasePath, url.PathEscape(sourceID))

	var result struct {
		Status      map[string]any `json:"status"`
		ComponentID string         `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "mongodb replica status"); err != nil {
		return nil, err
	}

	return result.Status, nil
}

// MongoDBCurrentOp returns the db.currentOp() result from a MongoDB source.
func (c *Client) MongoDBCurrentOp(ctx context.Context, sourceID string) (map[string]any, error) {
	u := fmt.Sprintf("%s%s/%s/current-op",
		c.baseURL, apiMongoDBBasePath, url.PathEscape(sourceID))

	var result struct {
		Op          map[string]any `json:"op"`
		ComponentID string         `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "mongodb current op"); err != nil {
		return nil, err
	}

	return result.Op, nil
}

// --- Kafka ---

// KafkaTopics returns the list of Kafka topics with metadata from a Kafka source.
func (c *Client) KafkaTopics(ctx context.Context, sourceID string) ([]kafkaadapter.TopicInfo, error) {
	u := fmt.Sprintf("%s%s/%s/topics",
		c.baseURL, apiKafkaBasePath, url.PathEscape(sourceID))

	var result struct {
		Topics      []kafkaadapter.TopicInfo `json:"topics"`
		Count       int                      `json:"count"`
		ComponentID string                   `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "kafka topics"); err != nil {
		return nil, err
	}

	return result.Topics, nil
}

// KafkaBrokers returns the list of Kafka brokers in the cluster.
func (c *Client) KafkaBrokers(ctx context.Context, sourceID string) ([]kafkaadapter.BrokerInfo, error) {
	u := fmt.Sprintf("%s%s/%s/brokers",
		c.baseURL, apiKafkaBasePath, url.PathEscape(sourceID))

	var result struct {
		Brokers     []kafkaadapter.BrokerInfo `json:"brokers"`
		Count       int                       `json:"count"`
		ComponentID string                    `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "kafka brokers"); err != nil {
		return nil, err
	}

	return result.Brokers, nil
}

// KafkaConsumerGroups returns consumer group information including lag from a Kafka source.
func (c *Client) KafkaConsumerGroups(ctx context.Context, sourceID string) ([]kafkaadapter.ConsumerGroupInfo, error) {
	u := fmt.Sprintf("%s%s/%s/consumer-groups",
		c.baseURL, apiKafkaBasePath, url.PathEscape(sourceID))

	var result struct {
		Groups      []kafkaadapter.ConsumerGroupInfo `json:"groups"`
		Count       int                              `json:"count"`
		ComponentID string                           `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "kafka consumer groups"); err != nil {
		return nil, err
	}

	return result.Groups, nil
}

// --- Elasticsearch ---

// ElasticsearchHealth returns the cluster health status from an Elasticsearch source.
func (c *Client) ElasticsearchHealth(ctx context.Context, sourceID string) (*elasticsearchadapter.ClusterHealth, error) {
	u := fmt.Sprintf("%s%s/%s/health",
		c.baseURL, apiElasticsearchBasePath, url.PathEscape(sourceID))

	var result struct {
		Health      *elasticsearchadapter.ClusterHealth `json:"health"`
		ComponentID string                              `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "elasticsearch health"); err != nil {
		return nil, err
	}

	return result.Health, nil
}

// ElasticsearchIndices returns index information, optionally filtered by pattern.
func (c *Client) ElasticsearchIndices(ctx context.Context, sourceID, pattern string) ([]elasticsearchadapter.IndexInfo, error) {
	u := fmt.Sprintf("%s%s/%s/indices",
		c.baseURL, apiElasticsearchBasePath, url.PathEscape(sourceID))
	if pattern != "" {
		u += "?pattern=" + url.QueryEscape(pattern)
	}

	var result struct {
		Indices     []elasticsearchadapter.IndexInfo `json:"indices"`
		Count       int                              `json:"count"`
		ComponentID string                           `json:"component_id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, u, nil, http.StatusOK, &result, "elasticsearch indices"); err != nil {
		return nil, err
	}

	return result.Indices, nil
}
