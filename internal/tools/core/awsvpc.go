package core

import (
	"context"
	"fmt"

	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	"github.com/jaimegago/joe/internal/llm"
)

// AWSVPCClient defines the subset of client needed for AWSVPCTool.
type AWSVPCClient interface {
	AWSVPCList(ctx context.Context, sourceID string) ([]*awsadapter.VPC, error)
	AWSVPCGet(ctx context.Context, sourceID, vpcID string) (*awsadapter.VPC, error)
}

// AWSVPCTool queries AWS VPC networks via joecored.
type AWSVPCTool struct {
	client AWSVPCClient
}

// NewAWSVPCTool creates a new aws_vpc tool.
func NewAWSVPCTool(c AWSVPCClient) *AWSVPCTool {
	return &AWSVPCTool{client: c}
}

func (t *AWSVPCTool) Name() string { return "aws_vpc" }

func (t *AWSVPCTool) Description() string {
	return "Query AWS VPC (Virtual Private Cloud) networks from a connected AWS account. Lists all VPCs in the region with their subnets, or gets a specific VPC by ID. Provides CIDR blocks, subnets, availability zones, and more. If you don't know the component_id, call list_components first to discover available AWS connections."
}

func (t *AWSVPCTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"component_id": {
				Type:        "string",
				Description: "ID of the AWS component to query.",
			},
			"vpc_id": {
				Type:        "string",
				Description: "VPC ID to get (e.g., vpc-1234567890abcdef0). Omit to list all VPCs.",
			},
		},
		Required: []string{"component_id"},
	}
}

func (t *AWSVPCTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["component_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: component_id")
	}

	vpcID, _ := args["vpc_id"].(string)

	// Single VPC get
	if vpcID != "" {
		vpc, err := t.client.AWSVPCGet(ctx, sourceID, vpcID)
		if err != nil {
			return nil, fmt.Errorf("aws vpc get vpc failed: %w", err)
		}
		return map[string]any{
			"vpc":          vpc,
			"component_id": sourceID,
		}, nil
	}

	// List all VPCs
	vpcs, err := t.client.AWSVPCList(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("aws vpc list vpcs failed: %w", err)
	}

	return map[string]any{
		"vpcs":         vpcs,
		"count":        len(vpcs),
		"component_id": sourceID,
	}, nil
}
