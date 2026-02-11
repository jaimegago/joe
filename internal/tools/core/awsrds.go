package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/llm"
)

// AWSRDSTool queries AWS RDS instances via joecored.
type AWSRDSTool struct {
	client *client.Client
}

// NewAWSRDSTool creates a new aws_rds tool.
func NewAWSRDSTool(c *client.Client) *AWSRDSTool {
	return &AWSRDSTool{client: c}
}

func (t *AWSRDSTool) Name() string { return "aws_rds" }

func (t *AWSRDSTool) Description() string {
	return "Query AWS RDS (Relational Database Service) instances from a connected AWS account. Lists all database instances in the region, or gets a specific instance by ID. Provides database engine, status, endpoint, VPC configuration, and more. If you don't know the source_id, call list_sources first to discover available AWS connections."
}

func (t *AWSRDSTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the AWS source to query.",
			},
			"db_instance_id": {
				Type:        "string",
				Description: "RDS DB instance identifier to get. Omit to list all DB instances.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *AWSRDSTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	dbInstanceID, _ := args["db_instance_id"].(string)

	// Single DB instance get
	if dbInstanceID != "" {
		instance, err := t.client.AWSRDSGetInstance(ctx, sourceID, dbInstanceID)
		if err != nil {
			return nil, fmt.Errorf("aws rds get instance failed: %w", err)
		}
		return map[string]any{
			"instance":  instance,
			"source_id": sourceID,
		}, nil
	}

	// List all DB instances
	instances, err := t.client.AWSRDSListInstances(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("aws rds list instances failed: %w", err)
	}

	return map[string]any{
		"instances": instances,
		"count":     len(instances),
		"source_id": sourceID,
	}, nil
}
