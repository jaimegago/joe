package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// ListEC2Instances lists all EC2 instances in the region
func (a *Adapter) ListEC2Instances(ctx context.Context) ([]EC2Instance, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if a.ec2Client == nil {
		return nil, fmt.Errorf("EC2 client not initialized")
	}

	input := &ec2.DescribeInstancesInput{}
	result, err := a.ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe EC2 instances: %w", err)
	}

	var instances []EC2Instance
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			ec2Instance := convertEC2Instance(instance)
			instances = append(instances, ec2Instance)
		}
	}

	return instances, nil
}

// GetEC2Instance retrieves a specific EC2 instance by ID
func (a *Adapter) GetEC2Instance(ctx context.Context, instanceID string) (*EC2Instance, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if a.ec2Client == nil {
		return nil, fmt.Errorf("EC2 client not initialized")
	}

	input := &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}

	result, err := a.ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe EC2 instance %s: %w", instanceID, err)
	}

	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			if *instance.InstanceId == instanceID {
				ec2Instance := convertEC2Instance(instance)
				return &ec2Instance, nil
			}
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrInstanceNotFound, instanceID)
}

// convertEC2Instance converts AWS EC2 Instance to our EC2Instance struct
func convertEC2Instance(instance types.Instance) EC2Instance {
	result := EC2Instance{
		InstanceID:   *instance.InstanceId,
		InstanceType: string(instance.InstanceType),
		State:        string(instance.State.Name),
		Tags:         make(map[string]string),
	}

	// Set optional fields if they exist
	if instance.PublicIpAddress != nil {
		result.PublicIP = *instance.PublicIpAddress
	}

	if instance.PrivateIpAddress != nil {
		result.PrivateIP = *instance.PrivateIpAddress
	}

	if instance.VpcId != nil {
		result.VpcID = *instance.VpcId
	}

	if instance.SubnetId != nil {
		result.SubnetID = *instance.SubnetId
	}

	if instance.LaunchTime != nil {
		result.LaunchTime = instance.LaunchTime.Format(timeFormatRFC3339)
	}

	if instance.Placement != nil && instance.Placement.AvailabilityZone != nil {
		result.AvailabilityZone = *instance.Placement.AvailabilityZone
	}

	// Convert security groups
	for _, sg := range instance.SecurityGroups {
		if sg.GroupId != nil && sg.GroupName != nil {
			result.SecurityGroups = append(result.SecurityGroups, SecurityGroup{
				GroupID:   *sg.GroupId,
				GroupName: *sg.GroupName,
			})
		}
	}

	// Convert tags
	for _, tag := range instance.Tags {
		if tag.Key != nil && tag.Value != nil {
			result.Tags[*tag.Key] = *tag.Value
		}
	}

	return result
}
