package access

import (
	"context"

	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	"github.com/jaimegago/joe/internal/rbac"
)

func (a *Accessor) AWSListEC2Instances(ctx context.Context, principal rbac.Principal, sourceID string) ([]awsadapter.EC2Instance, error) {
	ad, err := guard[awsadapter.AWSAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "aws")
	if err != nil {
		return nil, err
	}
	return ad.ListEC2Instances(ctx)
}

func (a *Accessor) AWSGetEC2Instance(ctx context.Context, principal rbac.Principal, sourceID, instanceID string) (*awsadapter.EC2Instance, error) {
	ad, err := guard[awsadapter.AWSAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "aws")
	if err != nil {
		return nil, err
	}
	return ad.GetEC2Instance(ctx, instanceID)
}

func (a *Accessor) AWSListEKSClusters(ctx context.Context, principal rbac.Principal, sourceID string) ([]awsadapter.EKSCluster, error) {
	ad, err := guard[awsadapter.AWSAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "aws")
	if err != nil {
		return nil, err
	}
	return ad.ListEKSClusters(ctx)
}

func (a *Accessor) AWSGetEKSCluster(ctx context.Context, principal rbac.Principal, sourceID, clusterName string) (*awsadapter.EKSCluster, error) {
	ad, err := guard[awsadapter.AWSAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "aws")
	if err != nil {
		return nil, err
	}
	return ad.GetEKSCluster(ctx, clusterName)
}

func (a *Accessor) AWSListRDSInstances(ctx context.Context, principal rbac.Principal, sourceID string) ([]awsadapter.RDSInstance, error) {
	ad, err := guard[awsadapter.AWSAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "aws")
	if err != nil {
		return nil, err
	}
	return ad.ListRDSInstances(ctx)
}

func (a *Accessor) AWSGetRDSInstance(ctx context.Context, principal rbac.Principal, sourceID, dbInstanceID string) (*awsadapter.RDSInstance, error) {
	ad, err := guard[awsadapter.AWSAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "aws")
	if err != nil {
		return nil, err
	}
	return ad.GetRDSInstance(ctx, dbInstanceID)
}

func (a *Accessor) AWSListVPCs(ctx context.Context, principal rbac.Principal, sourceID string) ([]awsadapter.VPC, error) {
	ad, err := guard[awsadapter.AWSAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "aws")
	if err != nil {
		return nil, err
	}
	return ad.ListVPCs(ctx)
}

func (a *Accessor) AWSGetVPC(ctx context.Context, principal rbac.Principal, sourceID, vpcID string) (*awsadapter.VPC, error) {
	ad, err := guard[awsadapter.AWSAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "aws")
	if err != nil {
		return nil, err
	}
	return ad.GetVPC(ctx, vpcID)
}
