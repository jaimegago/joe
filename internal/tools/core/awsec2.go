package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/llm"
)

// AWSEC2Tool queries AWS EC2 instances via joecored.
type AWSEC2Tool struct {
	client *client.Client
}

// NewAWSEC2Tool creates a new aws_ec2 tool.
func NewAWSEC2Tool(c *client.Client) *AWSEC2Tool {
	return &AWSEC2Tool{client: c}
}

func (t *AWSEC2Tool) Name() string { return "aws_ec2" }

func (t *AWSEC2Tool) Description() string {
	return "Query AWS EC2 instances from a connected AWS account. Lists all instances in the region, or gets a specific instance by ID. Provides information about instance state, type, IPs, VPC, security groups, tags, and more. If you don't know the source_id, call list_sources first to discover available AWS connections."
}

func (t *AWSEC2Tool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the AWS source to query.",
			},
			"instance_id": {
				Type:        "string",
				Description: "EC2 instance ID to get (e.g., i-1234567890abcdef0). Omit to list all instances.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *AWSEC2Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	instanceID, _ := args["instance_id"].(string)

	// Single instance get
	if instanceID != "" {
		instance, err := t.client.AWSEC2GetInstance(ctx, sourceID, instanceID)
		if err != nil {
			return nil, fmt.Errorf("aws ec2 get instance failed: %w", err)
		}
		return map[string]any{
			"instance":  instance,
			"source_id": sourceID,
		}, nil
	}

	// List all instances
	instances, err := t.client.AWSEC2ListInstances(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("aws ec2 list instances failed: %w", err)
	}

	return map[string]any{
		"instances": instances,
		"count":     len(instances),
		"source_id": sourceID,
	}, nil
}
