package aws

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
)

// ListEKSClusters lists all EKS clusters in the region
func (a *Adapter) ListEKSClusters(ctx context.Context) ([]EKSCluster, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if a.eksClient == nil {
		return nil, fmt.Errorf("EKS client not initialized")
	}

	// First get cluster names
	listInput := &eks.ListClustersInput{}
	listResult, err := a.eksClient.ListClusters(ctx, listInput)
	if err != nil {
		return nil, fmt.Errorf("list EKS clusters: %w", err)
	}

	var clusters []EKSCluster
	for _, clusterName := range listResult.Clusters {
		// Lock-free variant: we already hold RLock. Re-acquiring RLock via the
		// public GetEKSCluster would deadlock once a writer queues between the
		// two acquisitions (sync.RWMutex readers block behind a waiting writer).
		cluster, err := a.getEKSCluster(ctx, clusterName)
		if err != nil {
			// Log error but continue with other clusters
			slog.Warn("skipping EKS cluster in list result", "cluster", clusterName, "error", err)
			continue
		}
		if cluster != nil {
			clusters = append(clusters, *cluster)
		}
	}

	return clusters, nil
}

// GetEKSCluster retrieves a specific EKS cluster by name
func (a *Adapter) GetEKSCluster(ctx context.Context, clusterName string) (*EKSCluster, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.getEKSCluster(ctx, clusterName)
}

// getEKSCluster is the lock-free core of GetEKSCluster; callers must hold
// a.mu (read or write). The lock is taken once at the public boundary so
// ListEKSClusters can reuse this without a recursive RLock.
func (a *Adapter) getEKSCluster(ctx context.Context, clusterName string) (*EKSCluster, error) {
	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if a.eksClient == nil {
		return nil, fmt.Errorf("EKS client not initialized")
	}

	input := &eks.DescribeClusterInput{
		Name: &clusterName,
	}

	result, err := a.eksClient.DescribeCluster(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe EKS cluster %s: %w", clusterName, err)
	}

	if result.Cluster == nil {
		return nil, fmt.Errorf("%w: %s", ErrClusterNotFound, clusterName)
	}

	cluster := convertEKSCluster(*result.Cluster)
	return &cluster, nil
}

// convertEKSCluster converts AWS EKS Cluster to our EKSCluster struct
func convertEKSCluster(cluster types.Cluster) EKSCluster {
	result := EKSCluster{
		Tags: make(map[string]string),
	}

	// Set required fields
	if cluster.Name != nil {
		result.Name = *cluster.Name
	}

	if cluster.Arn != nil {
		result.ARN = *cluster.Arn
	}

	if cluster.Version != nil {
		result.Version = *cluster.Version
	}

	if cluster.Status != "" {
		result.Status = string(cluster.Status)
	}

	if cluster.RoleArn != nil {
		result.RoleARN = *cluster.RoleArn
	}

	// Set optional fields
	if cluster.Endpoint != nil {
		result.Endpoint = *cluster.Endpoint
	}

	if cluster.CreatedAt != nil {
		result.CreatedAt = cluster.CreatedAt.Format(timeFormatRFC3339)
	}

	if cluster.PlatformVersion != nil {
		result.PlatformVersion = *cluster.PlatformVersion
	}

	// Convert VPC configuration
	if cluster.ResourcesVpcConfig != nil {
		if cluster.ResourcesVpcConfig.VpcId != nil {
			result.VpcID = *cluster.ResourcesVpcConfig.VpcId
		}
		vpcConfig := VPCConfig{
			SubnetIDs:        cluster.ResourcesVpcConfig.SubnetIds,
			SecurityGroupIDs: cluster.ResourcesVpcConfig.SecurityGroupIds,
		}

		// Note: EndpointConfig details are available but simplified for now
		vpcConfig.EndpointConfig = "available" // Could be enhanced to show public/private details

		result.VpcConfig = vpcConfig
	}

	// Convert tags
	for key, value := range cluster.Tags {
		result.Tags[key] = value
	}

	return result
}
