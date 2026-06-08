package core

import (
	"context"
	"fmt"

	mysqladapter "github.com/jaimegago/joe/internal/adapters/datastore/mysql"
	"github.com/jaimegago/joe/internal/llm"
)

// MySQLClient defines the subset of client.Client needed for MySQL tools.
type MySQLClient interface {
	MySQLStat(ctx context.Context, sourceID string) (*mysqladapter.Stat, error)
	MySQLQuery(ctx context.Context, sourceID, query string) ([]map[string]any, error)
}

// MySQLStatTool retrieves MySQL status statistics via joecored.
type MySQLStatTool struct {
	Client MySQLClient
}

// NewMySQLStatTool creates a new mysql_stat tool.
func NewMySQLStatTool(c MySQLClient) *MySQLStatTool {
	return &MySQLStatTool{Client: c}
}

func (t *MySQLStatTool) Name() string { return "mysql_stat" }

func (t *MySQLStatTool) Description() string {
	return "Retrieve MySQL database status including the process list, replication status, " +
		"slow query count, thread counts, and aborted connections. " +
		"Use this to diagnose query pile-ups, replication lag, and connection issues. " +
		"If you don't know the component_id, call list_components first."
}

func (t *MySQLStatTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the MySQL source to query.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *MySQLStatTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	stat, err := t.Client.MySQLStat(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("mysql stat: %w", err)
	}

	return map[string]any{
		"stat":         stat,
		"component_id": sourceID,
	}, nil
}

// MySQLQueryTool executes a read-only SQL query against a MySQL source via joecored.
type MySQLQueryTool struct {
	Client MySQLClient
}

// NewMySQLQueryTool creates a new mysql_query tool.
func NewMySQLQueryTool(c MySQLClient) *MySQLQueryTool {
	return &MySQLQueryTool{Client: c}
}

func (t *MySQLQueryTool) Name() string { return "mysql_query" }

func (t *MySQLQueryTool) Description() string {
	return "Execute a read-only SELECT query against a MySQL database. " +
		"Only SELECT statements are permitted. Use this to inspect table data, " +
		"information_schema views, or run diagnostic queries. " +
		"If you don't know the component_id, call list_components first."
}

func (t *MySQLQueryTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the MySQL source to query.",
			},
			"query": {
				Type:        "string",
				Description: "SQL SELECT statement to execute. Only SELECT is allowed.",
			},
		},
		Required: []string{"component_id", "query"},
	}
}

func (t *MySQLQueryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	rows, err := t.Client.MySQLQuery(ctx, sourceID, query)
	if err != nil {
		return nil, fmt.Errorf("mysql query: %w", err)
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
