package coreagent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

func (r *Refresher) refreshAWSSource(ctx context.Context, source *store.Source, adapter awsadapter.AWSAdapter) error {
	start := time.Now()
	r.logger.Info("refreshing aws source", "source_id", source.ID)

	now := time.Now()
	region := awsRegionFromSource(source)

	desiredNodes := make([]graph.Node, 0)
	desiredEdges := make([]graph.Edge, 0)
	nodeIndex := make(map[string]struct{})
	vpcIndex := make(map[string]string)

	vpcs, err := adapter.ListVPCs(ctx)
	if err != nil {
		return fmt.Errorf("list vpcs: %w", err)
	}
	for _, vpc := range vpcs {
		nodeID := awsNodeID(source.ID, "vpc", vpc.VpcID)
		metadata := map[string]any{
			"vpc_id":     vpc.VpcID,
			"cidr_block": vpc.CidrBlock,
			"state":      vpc.State,
			"is_default": vpc.IsDefault,
		}
		if region != "" {
			metadata["region"] = region
		}
		if len(vpc.Tags) > 0 {
			metadata["tags"] = vpc.Tags
		}
		desiredNodes = append(desiredNodes, graph.Node{
			ID:       nodeID,
			Type:     "vpc",
			SourceID: source.ID,
			Metadata: metadata,
			LastSeen: now,
		})
		nodeIndex[nodeID] = struct{}{}
		vpcIndex[vpc.VpcID] = nodeID
	}

	ec2Instances, err := adapter.ListEC2Instances(ctx)
	if err != nil {
		return fmt.Errorf("list ec2 instances: %w", err)
	}
	for _, instance := range ec2Instances {
		nodeID := awsNodeID(source.ID, "ec2", instance.InstanceID)
		metadata := map[string]any{
			"instance_id":       instance.InstanceID,
			"instance_type":     instance.InstanceType,
			"state":             instance.State,
			"vpc_id":            instance.VpcID,
			"subnet_id":         instance.SubnetID,
			"public_ip":         instance.PublicIP,
			"private_ip":        instance.PrivateIP,
			"availability_zone": instance.AvailabilityZone,
		}
		if region != "" {
			metadata["region"] = region
		}
		if len(instance.Tags) > 0 {
			metadata["tags"] = instance.Tags
		}
		if len(instance.SecurityGroups) > 0 {
			metadata["security_groups"] = instance.SecurityGroups
		}
		desiredNodes = append(desiredNodes, graph.Node{
			ID:       nodeID,
			Type:     "ec2_instance",
			SourceID: source.ID,
			Metadata: metadata,
			LastSeen: now,
		})
		nodeIndex[nodeID] = struct{}{}

		if instance.VpcID != "" {
			if vpcID, ok := vpcIndex[instance.VpcID]; ok {
				desiredEdges = append(desiredEdges, graph.Edge{
					From:       nodeID,
					To:         vpcID,
					Relation:   "in_vpc",
					Confidence: graph.Explicit,
					Source:     "aws_api",
					SourceID:   source.ID,
					Context:    "vpc_id",
				})
			}
		}
	}

	eksClusters, err := adapter.ListEKSClusters(ctx)
	if err != nil {
		return fmt.Errorf("list eks clusters: %w", err)
	}
	for _, cluster := range eksClusters {
		nodeID := awsNodeID(source.ID, "eks", cluster.Name)
		metadata := map[string]any{
			"name":               cluster.Name,
			"arn":                cluster.ARN,
			"version":            cluster.Version,
			"status":             cluster.Status,
			"endpoint":           cluster.Endpoint,
			"role_arn":           cluster.RoleARN,
			"vpc_id":             cluster.VpcID,
			"subnet_ids":         cluster.VpcConfig.SubnetIDs,
			"security_group_ids": cluster.VpcConfig.SecurityGroupIDs,
			"platform_version":   cluster.PlatformVersion,
			"created_at":         cluster.CreatedAt,
		}
		if region != "" {
			metadata["region"] = region
		}
		if len(cluster.Tags) > 0 {
			metadata["tags"] = cluster.Tags
		}
		desiredNodes = append(desiredNodes, graph.Node{
			ID:       nodeID,
			Type:     "eks_cluster",
			SourceID: source.ID,
			Metadata: metadata,
			LastSeen: now,
		})
		nodeIndex[nodeID] = struct{}{}

		if cluster.VpcID != "" {
			if vpcID, ok := vpcIndex[cluster.VpcID]; ok {
				desiredEdges = append(desiredEdges, graph.Edge{
					From:       nodeID,
					To:         vpcID,
					Relation:   "in_vpc",
					Confidence: graph.Explicit,
					Source:     "aws_api",
					SourceID:   source.ID,
					Context:    "vpc_id",
				})
			}
		}
	}

	rdsInstances, err := adapter.ListRDSInstances(ctx)
	if err != nil {
		return fmt.Errorf("list rds instances: %w", err)
	}
	for _, instance := range rdsInstances {
		nodeID := awsNodeID(source.ID, "rds", instance.DBInstanceID)
		metadata := map[string]any{
			"db_instance_id": instance.DBInstanceID,
			"engine":         instance.Engine,
			"engine_version": instance.EngineVersion,
			"status":         instance.Status,
			"endpoint":       instance.Endpoint,
			"vpc_id":         instance.VpcID,
			"subnet_group":   instance.SubnetGroup,
			"instance_class": instance.InstanceClass,
			"created_time":   instance.CreatedTime,
		}
		if region != "" {
			metadata["region"] = region
		}
		if len(instance.Tags) > 0 {
			metadata["tags"] = instance.Tags
		}
		if len(instance.SecurityGroups) > 0 {
			metadata["security_groups"] = instance.SecurityGroups
		}
		desiredNodes = append(desiredNodes, graph.Node{
			ID:       nodeID,
			Type:     "rds_instance",
			SourceID: source.ID,
			Metadata: metadata,
			LastSeen: now,
		})
		nodeIndex[nodeID] = struct{}{}

		if instance.VpcID != "" {
			if vpcID, ok := vpcIndex[instance.VpcID]; ok {
				desiredEdges = append(desiredEdges, graph.Edge{
					From:       nodeID,
					To:         vpcID,
					Relation:   "in_vpc",
					Confidence: graph.Explicit,
					Source:     "aws_api",
					SourceID:   source.ID,
					Context:    "vpc_id",
				})
			}
		}
	}

	existingNodes, existingEdges, err := LoadGraphStateForSource(ctx, r.services.Graph, source.ID)
	if err != nil {
		return err
	}

	delta := BuildGraphDelta(existingNodes, existingEdges, desiredNodes, desiredEdges)
	if err := ApplyGraphDelta(ctx, r.services.Graph, delta); err != nil {
		return err
	}

	r.logger.Info("aws refresh completed", "source_id", source.ID, "nodes", len(desiredNodes), "edges", len(desiredEdges), "duration_ms", time.Since(start).Milliseconds())
	return nil
}

func awsNodeID(sourceID, service, resourceID string) string {
	return fmt.Sprintf("aws/%s/%s/%s", sourceID, service, resourceID)
}

func awsRegionFromSource(source *store.Source) string {
	if source == nil || len(source.Config) == 0 {
		return ""
	}
	var configMap map[string]any
	if err := json.Unmarshal(source.Config, &configMap); err != nil {
		return ""
	}
	cfg, err := awsadapter.ParseConfig(configMap)
	if err != nil {
		return ""
	}
	return cfg.Region
}
