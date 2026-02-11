package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
)

// ListRDSInstances lists all RDS instances in the region
func (a *Adapter) ListRDSInstances(ctx context.Context) ([]RDSInstance, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected || a.rdsClient == nil {
		return nil, fmt.Errorf("adapter not connected to AWS")
	}

	input := &rds.DescribeDBInstancesInput{}
	result, err := a.rdsClient.DescribeDBInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe RDS instances: %w", err)
	}

	var instances []RDSInstance
	for _, dbInstance := range result.DBInstances {
		rdsInstance := convertRDSInstance(dbInstance)
		instances = append(instances, rdsInstance)
	}

	return instances, nil
}

// GetRDSInstance retrieves a specific RDS instance by ID
func (a *Adapter) GetRDSInstance(ctx context.Context, dbInstanceID string) (*RDSInstance, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected || a.rdsClient == nil {
		return nil, fmt.Errorf("adapter not connected to AWS")
	}

	input := &rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: &dbInstanceID,
	}

	result, err := a.rdsClient.DescribeDBInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe RDS instance %s: %w", dbInstanceID, err)
	}

	if len(result.DBInstances) == 0 {
		return nil, fmt.Errorf("RDS instance %s not found", dbInstanceID)
	}

	rdsInstance := convertRDSInstance(result.DBInstances[0])
	return &rdsInstance, nil
}

// convertRDSInstance converts AWS RDS DBInstance to our RDSInstance struct
func convertRDSInstance(dbInstance types.DBInstance) RDSInstance {
	result := RDSInstance{
		Tags: make(map[string]string),
	}

	// Set required fields
	if dbInstance.DBInstanceIdentifier != nil {
		result.DBInstanceID = *dbInstance.DBInstanceIdentifier
	}

	if dbInstance.DBName != nil {
		result.DBName = *dbInstance.DBName
	}

	if dbInstance.Engine != nil {
		result.Engine = *dbInstance.Engine
	}

	if dbInstance.EngineVersion != nil {
		result.EngineVersion = *dbInstance.EngineVersion
	}

	if dbInstance.DBInstanceClass != nil {
		result.InstanceClass = *dbInstance.DBInstanceClass
	}

	if dbInstance.DBInstanceStatus != nil {
		result.Status = *dbInstance.DBInstanceStatus
	}

	// Set optional fields
	if dbInstance.Endpoint != nil {
		if dbInstance.Endpoint.Address != nil {
			result.Endpoint = *dbInstance.Endpoint.Address
		}
		if dbInstance.Endpoint.Port != nil {
			result.Port = *dbInstance.Endpoint.Port
		}
	}

	if dbInstance.DbInstancePort != nil {
		result.Port = *dbInstance.DbInstancePort
	}

	if dbInstance.DBSubnetGroup != nil && dbInstance.DBSubnetGroup.VpcId != nil {
		result.VpcID = *dbInstance.DBSubnetGroup.VpcId
	}

	if dbInstance.DBSubnetGroup != nil && dbInstance.DBSubnetGroup.DBSubnetGroupName != nil {
		result.SubnetGroup = *dbInstance.DBSubnetGroup.DBSubnetGroupName
	}

	if dbInstance.InstanceCreateTime != nil {
		result.CreatedTime = dbInstance.InstanceCreateTime.Format(time.RFC3339)
	}

	// Convert security groups
	for _, sg := range dbInstance.VpcSecurityGroups {
		if sg.VpcSecurityGroupId != nil && sg.Status != nil {
			result.SecurityGroups = append(result.SecurityGroups, SecurityGroup{
				GroupID:   *sg.VpcSecurityGroupId,
				GroupName: *sg.Status, // Status is used as name for VPC security groups
			})
		}
	}

	// Convert tags (note: RDS tags need separate API call, but we'll set this up for future enhancement)
	// For now, we'll leave Tags empty but the structure is ready

	return result
}
