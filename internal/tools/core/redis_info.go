package core

import (
	"context"
	"fmt"

	redisadapter "github.com/jaimegago/joe/internal/adapters/datastore/redis"
	"github.com/jaimegago/joe/internal/llm"
)

// RedisClient defines the subset of client.Client needed for Redis tools.
type RedisClient interface {
	RedisInfo(ctx context.Context, sourceID, section string) (map[string]string, error)
	RedisSlowLog(ctx context.Context, sourceID string, count int64) ([]redisadapter.SlowLogEntry, error)
	RedisDBSize(ctx context.Context, sourceID string) (int64, error)
}

// RedisInfoTool retrieves Redis INFO output via joecored.
type RedisInfoTool struct {
	Client RedisClient
}

// NewRedisInfoTool creates a new redis_info tool.
func NewRedisInfoTool(c RedisClient) *RedisInfoTool {
	return &RedisInfoTool{Client: c}
}

func (t *RedisInfoTool) Name() string { return "redis_info" }

func (t *RedisInfoTool) Description() string {
	return "Retrieve Redis INFO statistics for a specific section or all sections. " +
		"Sections include: server (version, uptime), clients (connected clients), " +
		"memory (used memory, fragmentation), stats (commands processed, hits/misses), " +
		"replication (role, slave count, replication offset), cpu, or all. " +
		"Use this to diagnose memory pressure, connection saturation, and cache hit rates. " +
		"If you don't know the component_id, call list_components first."
}

func (t *RedisInfoTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Redis component to query.",
			},
			"section": {
				Type:        "string",
				Description: "INFO section to retrieve: server, clients, memory, stats, replication, cpu, or all. Omit for default sections.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *RedisInfoTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	section, _ := args["section"].(string)

	info, err := t.Client.RedisInfo(ctx, sourceID, section)
	if err != nil {
		return nil, fmt.Errorf("redis info: %w", err)
	}

	return map[string]any{
		"info":         info,
		"section":      section,
		"component_id": sourceID,
	}, nil
}

// RedisSlowLogTool retrieves Redis slow log entries via joecored.
type RedisSlowLogTool struct {
	Client RedisClient
}

// NewRedisSlowLogTool creates a new redis_slowlog tool.
func NewRedisSlowLogTool(c RedisClient) *RedisSlowLogTool {
	return &RedisSlowLogTool{Client: c}
}

func (t *RedisSlowLogTool) Name() string { return "redis_slowlog" }

func (t *RedisSlowLogTool) Description() string {
	return "Retrieve recent Redis slow log entries showing commands that exceeded the slowlog threshold. " +
		"Each entry includes the command, execution time in microseconds, and timestamp. " +
		"Use this to identify expensive or blocked Redis commands. " +
		"If you don't know the component_id, call list_components first."
}

func (t *RedisSlowLogTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the Redis component to query.",
			},
			"count": {
				Type:        "integer",
				Description: "Number of slow log entries to return. Defaults to 10.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *RedisSlowLogTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	count := int64(10)
	if c, ok := args["count"].(float64); ok && c > 0 {
		count = int64(c)
	}

	entries, err := t.Client.RedisSlowLog(ctx, sourceID, count)
	if err != nil {
		return nil, fmt.Errorf("redis slowlog: %w", err)
	}

	if entries == nil {
		entries = []redisadapter.SlowLogEntry{}
	}

	return map[string]any{
		"entries":      entries,
		"count":        len(entries),
		"component_id": sourceID,
	}, nil
}
