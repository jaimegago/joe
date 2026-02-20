package coreagent

import (
	"context"
	"fmt"
	"time"

	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	mongodbadapter "github.com/jaimegago/joe/internal/adapters/datastore/mongodb"
	mysqladapter "github.com/jaimegago/joe/internal/adapters/datastore/mysql"
	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

// refreshPostgreSQLSource refreshes a PostgreSQL source.
// Creates a graph node and attempts stores_in edge discovery by matching the
// source name to existing service/deployment nodes.
func (r *Refresher) refreshPostgreSQLSource(ctx context.Context, source *store.Source, _ postgresadapter.PostgreSQLAdapter) error {
	r.logger.Info("refreshing postgresql source", "source_id", source.ID)
	return r.refreshDataStoreSource(ctx, source, "postgresql_source", graph.RelationStoresIn, "postgresql")
}

// refreshMySQLSource refreshes a MySQL source.
func (r *Refresher) refreshMySQLSource(ctx context.Context, source *store.Source, _ mysqladapter.MySQLAdapter) error {
	r.logger.Info("refreshing mysql source", "source_id", source.ID)
	return r.refreshDataStoreSource(ctx, source, "mysql_source", graph.RelationStoresIn, "mysql")
}

// refreshRedisSource refreshes a Redis source.
func (r *Refresher) refreshRedisSource(ctx context.Context, source *store.Source, _ redisadapter.RedisAdapter) error {
	r.logger.Info("refreshing redis source", "source_id", source.ID)
	return r.refreshDataStoreSource(ctx, source, "redis_source", graph.RelationStoresIn, "redis")
}

// refreshMongoDBSource refreshes a MongoDB source.
func (r *Refresher) refreshMongoDBSource(ctx context.Context, source *store.Source, _ mongodbadapter.MongoDBAdapter) error {
	r.logger.Info("refreshing mongodb source", "source_id", source.ID)
	return r.refreshDataStoreSource(ctx, source, "mongodb_source", graph.RelationStoresIn, "mongodb")
}

// refreshElasticsearchSource refreshes an Elasticsearch source.
func (r *Refresher) refreshElasticsearchSource(ctx context.Context, source *store.Source, _ elasticsearchadapter.ElasticsearchAdapter) error {
	r.logger.Info("refreshing elasticsearch source", "source_id", source.ID)
	return r.refreshDataStoreSource(ctx, source, "elasticsearch_source", graph.RelationStoresIn, "elasticsearch")
}

// refreshKafkaSource refreshes a Kafka source.
// Creates a graph node and attempts queues_in edge discovery by matching
// topic names to existing service/deployment nodes.
func (r *Refresher) refreshKafkaSource(ctx context.Context, source *store.Source, adapter kafkaadapter.KafkaAdapter) error {
	r.logger.Info("refreshing kafka source", "source_id", source.ID)

	now := time.Now()
	nodeID := datastoreNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:       nodeID,
			Type:     "kafka_source",
			SourceID: source.ID,
			Metadata: datastoreMetadata(source),
			LastSeen: now,
		},
	}

	desiredEdges := make([]graph.Edge, 0)

	// Discover topic names and attempt to match to service nodes via queues_in edges.
	topics, err := adapter.Topics(ctx)
	if err != nil {
		r.logger.Warn("failed to list kafka topics (skipping edge discovery)", "source_id", source.ID, "error", err)
	} else {
		edges := r.buildQueuesInEdges(ctx, source, nodeID, topicNames(topics), now)
		desiredEdges = append(desiredEdges, edges...)
	}

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for kafka source %s: %w", source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for kafka source %s: %w", source.ID, err)
	}

	r.logger.Info("kafka refresh completed",
		"source_id", source.ID,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// refreshDataStoreSource is the common refresh path for SQL/NoSQL data stores.
// It creates a source node and attempts name-based service matching for stores_in edges.
func (r *Refresher) refreshDataStoreSource(ctx context.Context, source *store.Source, nodeType, relation, tag string) error {
	now := time.Now()
	nodeID := datastoreNodeID(source.ID, source.Type)

	desiredNodes := []graph.Node{
		{
			ID:       nodeID,
			Type:     nodeType,
			SourceID: source.ID,
			Metadata: datastoreMetadata(source),
			LastSeen: now,
		},
	}

	// Attempt name-based match: services whose name contains the source name
	// (or vice versa) are considered candidates for a stores_in edge.
	desiredEdges := r.buildStoresInEdgesByName(ctx, source, nodeID, relation, tag, now)

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return fmt.Errorf("load graph state for %s source %s: %w", tag, source.ID, err)
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return fmt.Errorf("apply graph delta for %s source %s: %w", tag, source.ID, err)
	}

	r.logger.Info("datastore refresh completed",
		"source_id", source.ID,
		"type", tag,
		"nodes", len(desiredNodes),
		"edges", len(desiredEdges),
	)
	return nil
}

// buildStoresInEdgesByName queries the graph for service/deployment nodes whose
// name matches the source name, creating stores_in edges (low-confidence, inferred).
// Explicit edges are expected to come from .joe/ file processing.
func (r *Refresher) buildStoresInEdgesByName(ctx context.Context, source *store.Source, dsNodeID, relation, tag string, now time.Time) []graph.Edge {
	var edges []graph.Edge

	if source.Name == "" {
		return edges
	}

	matchingNodes, err := r.services.Graph.Query(ctx, source.Name)
	if err != nil {
		r.logger.Debug("graph query failed for datastore name match", "name", source.Name, "error", err)
		return edges
	}

	for _, svcNode := range matchingNodes {
		if svcNode.Type != "service" && svcNode.Type != "deployment" {
			continue
		}
		edges = append(edges, graph.Edge{
			From:       svcNode.ID,
			To:         dsNodeID,
			Relation:   relation,
			Confidence: graph.Inferred,
			Source:     tag + "_name_match",
			SourceID:   source.ID,
			Context:    "name=" + source.Name,
			CreatedAt:  now,
		})
	}

	return edges
}

// buildQueuesInEdges creates queues_in edges by matching Kafka topic names to
// existing service/deployment nodes.
func (r *Refresher) buildQueuesInEdges(ctx context.Context, source *store.Source, kafkaNodeID string, names []string, now time.Time) []graph.Edge {
	var edges []graph.Edge
	seen := make(map[string]bool)

	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true

		matchingNodes, err := r.services.Graph.Query(ctx, name)
		if err != nil {
			r.logger.Debug("graph query failed for kafka topic match", "topic", name, "error", err)
			continue
		}

		for _, svcNode := range matchingNodes {
			if svcNode.Type != "service" && svcNode.Type != "deployment" {
				continue
			}
			edges = append(edges, graph.Edge{
				From:       svcNode.ID,
				To:         kafkaNodeID,
				Relation:   graph.RelationQueuesIn,
				Confidence: graph.Inferred,
				Source:     "kafka_topics",
				SourceID:   source.ID,
				Context:    "topic=" + name,
				CreatedAt:  now,
			})
		}
	}

	return edges
}

// datastoreNodeID builds a stable graph node ID for a data store source.
func datastoreNodeID(sourceID, sourceType string) string {
	return fmt.Sprintf("datastore/%s/%s", sourceType, sourceID)
}

// datastoreMetadata builds the standard metadata map for a data store node.
func datastoreMetadata(source *store.Source) map[string]any {
	return map[string]any{
		"source_id":   source.ID,
		"source_type": source.Type,
		"name":        source.Name,
	}
}

// topicNames extracts the name field from a slice of TopicInfo.
func topicNames(topics []kafkaadapter.TopicInfo) []string {
	names := make([]string, 0, len(topics))
	for _, t := range topics {
		if !t.Internal && t.Name != "" {
			names = append(names, t.Name)
		}
	}
	return names
}
