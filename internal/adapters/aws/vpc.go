package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// ListVPCs lists all VPCs in the region
func (a *Adapter) ListVPCs(ctx context.Context) ([]VPC, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if a.ec2Client == nil {
		return nil, fmt.Errorf("EC2 client not initialized")
	}

	input := &ec2.DescribeVpcsInput{}
	result, err := a.ec2Client.DescribeVpcs(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe VPCs: %w", err)
	}

	var vpcs []VPC
	for _, vpc := range result.Vpcs {
		vpcData := convertVPC(vpc)
		if vpc.VpcId == nil {
			vpcs = append(vpcs, vpcData)
			continue
		}

		// Get subnets for this VPC
		subnets, err := a.getSubnetsForVPC(ctx, *vpc.VpcId)
		if err != nil {
			// Log error but continue
			continue
		}
		vpcData.Subnets = subnets

		vpcs = append(vpcs, vpcData)
	}

	return vpcs, nil
}

// GetVPC retrieves a specific VPC by ID
func (a *Adapter) GetVPC(ctx context.Context, vpcID string) (*VPC, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.checkConnected(); err != nil {
		return nil, err
	}

	if a.ec2Client == nil {
		return nil, fmt.Errorf("EC2 client not initialized")
	}

	input := &ec2.DescribeVpcsInput{
		VpcIds: []string{vpcID},
	}

	result, err := a.ec2Client.DescribeVpcs(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe VPC %s: %w", vpcID, err)
	}

	if len(result.Vpcs) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrVPCNotFound, vpcID)
	}

	vpc := convertVPC(result.Vpcs[0])

	// Get subnets for this VPC
	subnets, err := a.getSubnetsForVPC(ctx, vpcID)
	if err != nil {
		return nil, fmt.Errorf("get subnets for VPC %s: %w", vpcID, err)
	}
	vpc.Subnets = subnets

	return &vpc, nil
}

// getSubnetsForVPC retrieves all subnets for a specific VPC
func (a *Adapter) getSubnetsForVPC(ctx context.Context, vpcID string) ([]Subnet, error) {
	input := &ec2.DescribeSubnetsInput{
		Filters: []types.Filter{
			{
				Name:   &[]string{"vpc-id"}[0],
				Values: []string{vpcID},
			},
		},
	}

	result, err := a.ec2Client.DescribeSubnets(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("describe subnets for VPC %s: %w", vpcID, err)
	}

	var subnets []Subnet
	for _, subnet := range result.Subnets {
		subnetData := convertSubnet(subnet)
		subnets = append(subnets, subnetData)
	}

	return subnets, nil
}

// convertVPC converts AWS VPC to our VPC struct
func convertVPC(vpc types.Vpc) VPC {
	result := VPC{
		Tags: make(map[string]string),
	}

	// Set required fields
	if vpc.VpcId != nil {
		result.VpcID = *vpc.VpcId
	}

	if vpc.CidrBlock != nil {
		result.CidrBlock = *vpc.CidrBlock
	}

	if vpc.State != "" {
		result.State = string(vpc.State)
	}

	if vpc.IsDefault != nil {
		result.IsDefault = *vpc.IsDefault
	}

	// Convert tags
	for _, tag := range vpc.Tags {
		if tag.Key != nil && tag.Value != nil {
			result.Tags[*tag.Key] = *tag.Value
		}
	}

	return result
}

// convertSubnet converts AWS Subnet to our Subnet struct
func convertSubnet(subnet types.Subnet) Subnet {
	result := Subnet{
		Tags: make(map[string]string),
	}

	// Set required fields
	if subnet.SubnetId != nil {
		result.SubnetID = *subnet.SubnetId
	}

	if subnet.VpcId != nil {
		result.VpcID = *subnet.VpcId
	}

	if subnet.CidrBlock != nil {
		result.CidrBlock = *subnet.CidrBlock
	}

	if subnet.AvailabilityZone != nil {
		result.AvailabilityZone = *subnet.AvailabilityZone
	}

	if subnet.State != "" {
		result.State = string(subnet.State)
	}

	// Convert tags
	for _, tag := range subnet.Tags {
		if tag.Key != nil && tag.Value != nil {
			result.Tags[*tag.Key] = *tag.Value
		}
	}

	return result
}
