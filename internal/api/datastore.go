package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	elasticsearchadapter "github.com/jaimegago/joe/internal/adapters/datastore/elasticsearch"
	kafkaadapter "github.com/jaimegago/joe/internal/adapters/datastore/kafka"
	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
	"github.com/jaimegago/joe/internal/rbac"
)

// --- PostgreSQL handlers ---

// handlePostgresStat returns pg_stat_* statistics for a PostgreSQL source.
// GET /api/v1/postgresql/{componentID}/stat
func (s *Server) handlePostgresStat(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	stat, err := s.accessor.PostgresStat(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "postgresql", "stat", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "PostgreSQL") {
			return
		}
		writeInternalError(w, err, "postgres stat")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"stat":         stat,
		"component_id": sourceID,
	})
}

// handlePostgresQuery executes a read-only SQL query against a PostgreSQL source.
// GET /api/v1/postgresql/{componentID}/query?sql=SELECT+...
func (s *Server) handlePostgresQuery(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	sqlParam := r.URL.Query().Get("sql")
	if sqlParam == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: sql", map[string]any{
			"param": "sql",
		})
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	rows, err := s.accessor.PostgresQuery(r.Context(), principal, sourceID, sqlParam)
	s.services.Metrics.RecordAdapterCall(r.Context(), "postgresql", "query", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "PostgreSQL") {
			return
		}
		writeInternalError(w, err, "postgres query")
		return
	}

	if rows == nil {
		rows = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"rows":         rows,
		"count":        len(rows),
		"component_id": sourceID,
	})
}

// --- MySQL handlers ---

// handleMySQLStat returns status statistics for a MySQL source.
// GET /api/v1/mysql/{componentID}/stat
func (s *Server) handleMySQLStat(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	stat, err := s.accessor.MySQLStat(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "mysql", "stat", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "MySQL") {
			return
		}
		writeInternalError(w, err, "mysql stat")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"stat":         stat,
		"component_id": sourceID,
	})
}

// handleMySQLQuery executes a read-only SQL query against a MySQL source.
// GET /api/v1/mysql/{componentID}/query?sql=SELECT+...
func (s *Server) handleMySQLQuery(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	sqlParam := r.URL.Query().Get("sql")
	if sqlParam == "" {
		writeError(w, http.StatusBadRequest, errorCodeInvalidRequest, "missing required parameter: sql", map[string]any{
			"param": "sql",
		})
		return
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	rows, err := s.accessor.MySQLQuery(r.Context(), principal, sourceID, sqlParam)
	s.services.Metrics.RecordAdapterCall(r.Context(), "mysql", "query", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "MySQL") {
			return
		}
		writeInternalError(w, err, "mysql query")
		return
	}

	if rows == nil {
		rows = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"rows":         rows,
		"count":        len(rows),
		"component_id": sourceID,
	})
}

// --- Redis handlers ---

// handleRedisInfo returns Redis INFO output for a given section.
// GET /api/v1/redis/{componentID}/info?section=memory
func (s *Server) handleRedisInfo(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	section := r.URL.Query().Get("section") // optional, default ""

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	info, err := s.accessor.RedisInfo(r.Context(), principal, sourceID, section)
	s.services.Metrics.RecordAdapterCall(r.Context(), "redis", "info", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Redis") {
			return
		}
		writeInternalError(w, err, "redis info")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"info":         info,
		"component_id": sourceID,
	})
}

// handleRedisSlowLog returns the most recent slow log entries from a Redis source.
// GET /api/v1/redis/{componentID}/slowlog?count=10
func (s *Server) handleRedisSlowLog(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	count := int64(10)
	if c := r.URL.Query().Get("count"); c != "" {
		if v, err := strconv.ParseInt(c, 10, 64); err == nil && v > 0 {
			count = v
		}
	}

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	entries, err := s.accessor.RedisSlowLog(r.Context(), principal, sourceID, count)
	s.services.Metrics.RecordAdapterCall(r.Context(), "redis", "slowlog", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Redis") {
			return
		}
		writeInternalError(w, err, "redis slowlog")
		return
	}

	if entries == nil {
		entries = []redisadapter.SlowLogEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entries":      entries,
		"count":        len(entries),
		"component_id": sourceID,
	})
}

// handleRedisDBSize returns the number of keys in the current Redis database.
// GET /api/v1/redis/{componentID}/dbsize
func (s *Server) handleRedisDBSize(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	size, err := s.accessor.RedisDBSize(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "redis", "dbsize", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Redis") {
			return
		}
		writeInternalError(w, err, "redis dbsize")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"db_size":      size,
		"component_id": sourceID,
	})
}

// --- MongoDB handlers ---

// handleMongoDBServerStatus returns db.serverStatus() for a MongoDB source.
// GET /api/v1/mongodb/{componentID}/server-status
func (s *Server) handleMongoDBServerStatus(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	status, err := s.accessor.MongoDBServerStatus(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "mongodb", "server_status", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "MongoDB") {
			return
		}
		writeInternalError(w, err, "mongodb server status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       status,
		"component_id": sourceID,
	})
}

// handleMongoDBReplicaStatus returns rs.status() for a MongoDB source.
// GET /api/v1/mongodb/{componentID}/replica-status
func (s *Server) handleMongoDBReplicaStatus(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	status, err := s.accessor.MongoDBReplicaStatus(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "mongodb", "replica_status", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "MongoDB") {
			return
		}
		writeInternalError(w, err, "mongodb replica status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       status,
		"component_id": sourceID,
	})
}

// handleMongoDBCurrentOp returns db.currentOp() for a MongoDB source.
// GET /api/v1/mongodb/{componentID}/current-op
func (s *Server) handleMongoDBCurrentOp(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	op, err := s.accessor.MongoDBCurrentOp(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "mongodb", "current_op", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "MongoDB") {
			return
		}
		writeInternalError(w, err, "mongodb current op")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"op":           op,
		"component_id": sourceID,
	})
}

// --- Kafka handlers ---

// handleKafkaTopics returns the list of Kafka topics from a Kafka source.
// GET /api/v1/kafka/{componentID}/topics
func (s *Server) handleKafkaTopics(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	topics, err := s.accessor.KafkaTopics(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "kafka", "topics", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Kafka") {
			return
		}
		writeInternalError(w, err, "kafka topics")
		return
	}

	if topics == nil {
		topics = []kafkaadapter.TopicInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"topics":       topics,
		"count":        len(topics),
		"component_id": sourceID,
	})
}

// handleKafkaBrokers returns the list of Kafka brokers from a Kafka source.
// GET /api/v1/kafka/{componentID}/brokers
func (s *Server) handleKafkaBrokers(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	brokers, err := s.accessor.KafkaBrokers(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "kafka", "brokers", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Kafka") {
			return
		}
		writeInternalError(w, err, "kafka brokers")
		return
	}

	if brokers == nil {
		brokers = []kafkaadapter.BrokerInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"brokers":      brokers,
		"count":        len(brokers),
		"component_id": sourceID,
	})
}

// handleKafkaConsumerGroups returns consumer group information from a Kafka source.
// GET /api/v1/kafka/{componentID}/consumer-groups
func (s *Server) handleKafkaConsumerGroups(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	groups, err := s.accessor.KafkaConsumerGroups(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "kafka", "consumer_groups", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Kafka") {
			return
		}
		writeInternalError(w, err, "kafka consumer groups")
		return
	}

	if groups == nil {
		groups = []kafkaadapter.ConsumerGroupInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"groups":       groups,
		"count":        len(groups),
		"component_id": sourceID,
	})
}

// --- Elasticsearch handlers ---

// handleElasticsearchHealth returns the cluster health from an Elasticsearch source.
// GET /api/v1/elasticsearch/{componentID}/health
func (s *Server) handleElasticsearchHealth(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	health, err := s.accessor.ElasticsearchClusterHealth(r.Context(), principal, sourceID)
	s.services.Metrics.RecordAdapterCall(r.Context(), "elasticsearch", "health", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Elasticsearch") {
			return
		}
		writeInternalError(w, err, "elasticsearch health")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"health":       health,
		"component_id": sourceID,
	})
}

// handleElasticsearchIndices returns index information from an Elasticsearch source.
// GET /api/v1/elasticsearch/{componentID}/indices?pattern=logs-*
func (s *Server) handleElasticsearchIndices(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("componentID")
	pattern := r.URL.Query().Get("pattern") // optional, default ""

	principal := rbac.PrincipalFromContext(r.Context())
	start := time.Now()
	indices, err := s.accessor.ElasticsearchListIndices(r.Context(), principal, sourceID, pattern)
	s.services.Metrics.RecordAdapterCall(r.Context(), "elasticsearch", "indices", time.Since(start), err)
	if err != nil {
		if handleAccessError(w, err, sourceID, "Elasticsearch") {
			return
		}
		writeInternalError(w, err, "elasticsearch indices")
		return
	}

	if indices == nil {
		indices = []elasticsearchadapter.IndexInfo{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"indices":      indices,
		"count":        len(indices),
		"component_id": sourceID,
	})
}

// registerDatastoreRoutes registers all data store routes.
func (s *Server) registerDatastoreRoutes(mux *http.ServeMux, prefix string) {
	h := &datastoreHandler{server: s}
	// PostgreSQL
	mux.HandleFunc(fmt.Sprintf("GET %s/postgresql/{componentID}/stat", prefix), h.handlePostgresStat)
	mux.HandleFunc(fmt.Sprintf("GET %s/postgresql/{componentID}/query", prefix), h.handlePostgresQuery)
	// MySQL
	mux.HandleFunc(fmt.Sprintf("GET %s/mysql/{componentID}/stat", prefix), h.handleMySQLStat)
	mux.HandleFunc(fmt.Sprintf("GET %s/mysql/{componentID}/query", prefix), h.handleMySQLQuery)
	// Redis
	mux.HandleFunc(fmt.Sprintf("GET %s/redis/{componentID}/info", prefix), h.handleRedisInfo)
	mux.HandleFunc(fmt.Sprintf("GET %s/redis/{componentID}/slowlog", prefix), h.handleRedisSlowLog)
	mux.HandleFunc(fmt.Sprintf("GET %s/redis/{componentID}/dbsize", prefix), h.handleRedisDBSize)
	// MongoDB
	mux.HandleFunc(fmt.Sprintf("GET %s/mongodb/{componentID}/server-status", prefix), h.handleMongoDBServerStatus)
	mux.HandleFunc(fmt.Sprintf("GET %s/mongodb/{componentID}/replica-status", prefix), h.handleMongoDBReplicaStatus)
	mux.HandleFunc(fmt.Sprintf("GET %s/mongodb/{componentID}/current-op", prefix), h.handleMongoDBCurrentOp)
	// Kafka
	mux.HandleFunc(fmt.Sprintf("GET %s/kafka/{componentID}/topics", prefix), h.handleKafkaTopics)
	mux.HandleFunc(fmt.Sprintf("GET %s/kafka/{componentID}/brokers", prefix), h.handleKafkaBrokers)
	mux.HandleFunc(fmt.Sprintf("GET %s/kafka/{componentID}/consumer-groups", prefix), h.handleKafkaConsumerGroups)
	// Elasticsearch
	mux.HandleFunc(fmt.Sprintf("GET %s/elasticsearch/{componentID}/health", prefix), h.handleElasticsearchHealth)
	mux.HandleFunc(fmt.Sprintf("GET %s/elasticsearch/{componentID}/indices", prefix), h.handleElasticsearchIndices)
}

// datastoreHandler delegates to Server datastore methods.
type datastoreHandler struct{ server *Server }

func (h *datastoreHandler) handlePostgresStat(w http.ResponseWriter, r *http.Request) {
	h.server.handlePostgresStat(w, r)
}
func (h *datastoreHandler) handlePostgresQuery(w http.ResponseWriter, r *http.Request) {
	h.server.handlePostgresQuery(w, r)
}
func (h *datastoreHandler) handleMySQLStat(w http.ResponseWriter, r *http.Request) {
	h.server.handleMySQLStat(w, r)
}
func (h *datastoreHandler) handleMySQLQuery(w http.ResponseWriter, r *http.Request) {
	h.server.handleMySQLQuery(w, r)
}
func (h *datastoreHandler) handleRedisInfo(w http.ResponseWriter, r *http.Request) {
	h.server.handleRedisInfo(w, r)
}
func (h *datastoreHandler) handleRedisSlowLog(w http.ResponseWriter, r *http.Request) {
	h.server.handleRedisSlowLog(w, r)
}
func (h *datastoreHandler) handleRedisDBSize(w http.ResponseWriter, r *http.Request) {
	h.server.handleRedisDBSize(w, r)
}
func (h *datastoreHandler) handleMongoDBServerStatus(w http.ResponseWriter, r *http.Request) {
	h.server.handleMongoDBServerStatus(w, r)
}
func (h *datastoreHandler) handleMongoDBReplicaStatus(w http.ResponseWriter, r *http.Request) {
	h.server.handleMongoDBReplicaStatus(w, r)
}
func (h *datastoreHandler) handleMongoDBCurrentOp(w http.ResponseWriter, r *http.Request) {
	h.server.handleMongoDBCurrentOp(w, r)
}
func (h *datastoreHandler) handleKafkaTopics(w http.ResponseWriter, r *http.Request) {
	h.server.handleKafkaTopics(w, r)
}
func (h *datastoreHandler) handleKafkaBrokers(w http.ResponseWriter, r *http.Request) {
	h.server.handleKafkaBrokers(w, r)
}
func (h *datastoreHandler) handleKafkaConsumerGroups(w http.ResponseWriter, r *http.Request) {
	h.server.handleKafkaConsumerGroups(w, r)
}
func (h *datastoreHandler) handleElasticsearchHealth(w http.ResponseWriter, r *http.Request) {
	h.server.handleElasticsearchHealth(w, r)
}
func (h *datastoreHandler) handleElasticsearchIndices(w http.ResponseWriter, r *http.Request) {
	h.server.handleElasticsearchIndices(w, r)
}
