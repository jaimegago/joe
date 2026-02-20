package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// MongoDBClient defines the subset of client.Client needed for MongoDB tools.
type MongoDBClient interface {
	MongoDBServerStatus(ctx context.Context, sourceID string) (map[string]any, error)
	MongoDBReplicaStatus(ctx context.Context, sourceID string) (map[string]any, error)
	MongoDBCurrentOp(ctx context.Context, sourceID string) (map[string]any, error)
}

// MongoDBStatTool retrieves MongoDB statistics via joecored.
// It supports three actions: server_status, replica_status, and current_op.
type MongoDBStatTool struct {
	Client MongoDBClient
}

// NewMongoDBStatTool creates a new mongodb_stat tool.
func NewMongoDBStatTool(c MongoDBClient) *MongoDBStatTool {
	return &MongoDBStatTool{Client: c}
}

func (t *MongoDBStatTool) Name() string { return "mongodb_stat" }

func (t *MongoDBStatTool) Description() string {
	return "Retrieve MongoDB database statistics. Supports three actions: " +
		"'server_status' (default) returns db.serverStatus() with connection counts, operation rates, and memory usage; " +
		"'replica_status' returns rs.status() with replica set member health and replication lag; " +
		"'current_op' returns db.currentOp() showing in-progress operations. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *MongoDBStatTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the MongoDB source to query.",
			},
			"action": {
				Type:        "string",
				Description: "Action to perform: 'server_status' (default), 'replica_status', or 'current_op'.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *MongoDBStatTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	action, _ := args["action"].(string)
	if action == "" {
		action = "server_status"
	}

	switch action {
	case "server_status":
		status, err := t.Client.MongoDBServerStatus(ctx, sourceID)
		if err != nil {
			return nil, fmt.Errorf("mongodb server_status: %w", err)
		}
		return map[string]any{
			"status":    status,
			"action":    action,
			"source_id": sourceID,
		}, nil

	case "replica_status":
		status, err := t.Client.MongoDBReplicaStatus(ctx, sourceID)
		if err != nil {
			return nil, fmt.Errorf("mongodb replica_status: %w", err)
		}
		return map[string]any{
			"status":    status,
			"action":    action,
			"source_id": sourceID,
		}, nil

	case "current_op":
		op, err := t.Client.MongoDBCurrentOp(ctx, sourceID)
		if err != nil {
			return nil, fmt.Errorf("mongodb current_op: %w", err)
		}
		return map[string]any{
			"op":        op,
			"action":    action,
			"source_id": sourceID,
		}, nil

	default:
		return nil, fmt.Errorf("unknown action %q: must be server_status, replica_status, or current_op", action)
	}
}
