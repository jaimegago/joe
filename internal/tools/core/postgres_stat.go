package core

import (
	"context"
	"fmt"

	postgresadapter "github.com/jaimegago/joe/internal/adapters/datastore/postgres"
	"github.com/jaimegago/joe/internal/llm"
)

// PostgresClient defines the subset of client.Client needed for PostgreSQL tools.
type PostgresClient interface {
	PostgresStat(ctx context.Context, sourceID string) (*postgresadapter.Stat, error)
	PostgresQuery(ctx context.Context, sourceID, query string) ([]map[string]any, error)
}

// PostgresStatTool retrieves PostgreSQL statistics via joecored.
type PostgresStatTool struct {
	Client PostgresClient
}

// NewPostgresStatTool creates a new postgres_stat tool.
func NewPostgresStatTool(c PostgresClient) *PostgresStatTool {
	return &PostgresStatTool{Client: c}
}

func (t *PostgresStatTool) Name() string { return "postgres_stat" }

func (t *PostgresStatTool) Description() string {
	return "Retrieve PostgreSQL database statistics including active connections, transaction rates, " +
		"block cache hit ratio, table statistics, and replication slot lag. " +
		"Use this to diagnose connection exhaustion, cache misses, dead tuples, and replication issues. " +
		"If you don't know the component_id, call list_components first."
}

func (t *PostgresStatTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the PostgreSQL source to query.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *PostgresStatTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	stat, err := t.Client.PostgresStat(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("postgres stat: %w", err)
	}

	return map[string]any{
		"stat":         stat,
		"component_id": sourceID,
	}, nil
}

// PostgresQueryTool executes a read-only SQL query against a PostgreSQL source via joecored.
type PostgresQueryTool struct {
	Client PostgresClient
}

// NewPostgresQueryTool creates a new postgres_query tool.
func NewPostgresQueryTool(c PostgresClient) *PostgresQueryTool {
	return &PostgresQueryTool{Client: c}
}

func (t *PostgresQueryTool) Name() string { return "postgres_query" }

func (t *PostgresQueryTool) Description() string {
	return "Execute a read-only SELECT query against a PostgreSQL database. " +
		"Only SELECT statements are permitted. Use this to inspect table data, " +
		"check pg_stat_* views, or run diagnostic queries. " +
		"If you don't know the component_id, call list_components first."
}

func (t *PostgresQueryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the PostgreSQL source to query.",
			},
			"query": {
				Type:        "string",
				Description: "SQL SELECT statement to execute. Only SELECT is allowed.",
			},
		},
		Required: []string{"component_id", "query"},
	}
}

func (t *PostgresQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	rows, err := t.Client.PostgresQuery(ctx, sourceID, query)
	if err != nil {
		return nil, fmt.Errorf("postgres query: %w", err)
	}

	if rows == nil {
		rows = []map[string]any{}
	}

	return map[string]any{
		"rows":         rows,
		"count":        len(rows),
		"component_id": sourceID,
		"query":        query,
	}, nil
}
