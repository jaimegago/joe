package core

import (
	"context"
	"fmt"

	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	"github.com/jaimegago/joe/internal/llm"
)

// AWSEKSClient defines the subset of client needed for AWSEKSTool.
type AWSEKSClient interface {
	AWSEKSListClusters(ctx context.Context, sourceID string) ([]*awsadapter.EKSCluster, error)
	AWSEKSGetCluster(ctx context.Context, sourceID, clusterName string) (*awsadapter.EKSCluster, error)
}

// AWSEKSTool queries AWS EKS clusters via joecored.
type AWSEKSTool struct {
	client AWSEKSClient
}

// NewAWSEKSTool creates a new aws_eks tool.
func NewAWSEKSTool(c AWSEKSClient) *AWSEKSTool {
	return &AWSEKSTool{client: c}
}

func (t *AWSEKSTool) Name() string { return "aws_eks" }

func (t *AWSEKSTool) Description() string {
	return "Query AWS EKS (Elastic Kubernetes Service) clusters from a connected AWS account. Lists all clusters in the region, or gets a specific cluster by name. Provides cluster status, version, endpoint, VPC configuration, and more. If you don't know the source_id, call list_sources first to discover available AWS connections."
}

func (t *AWSEKSTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the AWS source to query.",
			},
			"cluster_name": {
				Type:        "string",
				Description: "EKS cluster name to get. Omit to list all clusters.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *AWSEKSTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}

	clusterName, _ := args["cluster_name"].(string)

	// Single cluster get
	if clusterName != "" {
		cluster, err := t.client.AWSEKSGetCluster(ctx, sourceID, clusterName)
		if err != nil {
			return nil, fmt.Errorf("aws eks get cluster failed: %w", err)
		}
		return map[string]any{
			"cluster":   cluster,
			"source_id": sourceID,
		}, nil
	}

	// List all clusters
	clusters, err := t.client.AWSEKSListClusters(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("aws eks list clusters failed: %w", err)
	}

	return map[string]any{
		"clusters":  clusters,
		"count":     len(clusters),
		"source_id": sourceID,
	}, nil
}
