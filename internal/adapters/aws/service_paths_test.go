package aws

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

type fakeEC2Client struct {
	describeInstances func(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	describeVpcs      func(context.Context, *ec2.DescribeVpcsInput, ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	describeSubnets   func(context.Context, *ec2.DescribeSubnetsInput, ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
}

func (f fakeEC2Client) DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if f.describeInstances == nil {
		return &ec2.DescribeInstancesOutput{}, nil
	}
	return f.describeInstances(ctx, in, optFns...)
}

func (f fakeEC2Client) DescribeVpcs(ctx context.Context, in *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	if f.describeVpcs == nil {
		return &ec2.DescribeVpcsOutput{}, nil
	}
	return f.describeVpcs(ctx, in, optFns...)
}

func (f fakeEC2Client) DescribeSubnets(ctx context.Context, in *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	if f.describeSubnets == nil {
		return &ec2.DescribeSubnetsOutput{}, nil
	}
	return f.describeSubnets(ctx, in, optFns...)
}

type fakeEKSClient struct {
	listClusters    func(context.Context, *eks.ListClustersInput, ...func(*eks.Options)) (*eks.ListClustersOutput, error)
	describeCluster func(context.Context, *eks.DescribeClusterInput, ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
}

func (f fakeEKSClient) ListClusters(ctx context.Context, in *eks.ListClustersInput, optFns ...func(*eks.Options)) (*eks.ListClustersOutput, error) {
	if f.listClusters == nil {
		return &eks.ListClustersOutput{}, nil
	}
	return f.listClusters(ctx, in, optFns...)
}

func (f fakeEKSClient) DescribeCluster(ctx context.Context, in *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	if f.describeCluster == nil {
		return &eks.DescribeClusterOutput{}, nil
	}
	return f.describeCluster(ctx, in, optFns...)
}

type fakeRDSClient struct {
	describeDBInstances func(context.Context, *rds.DescribeDBInstancesInput, ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error)
}

func (f fakeRDSClient) DescribeDBInstances(ctx context.Context, in *rds.DescribeDBInstancesInput, optFns ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
	if f.describeDBInstances == nil {
		return &rds.DescribeDBInstancesOutput{}, nil
	}
	return f.describeDBInstances(ctx, in, optFns...)
}

func TestNewWithClients_SetsConnected(t *testing.T) {
	adapter := NewWithClients(nil, nil, nil)
	if !adapter.connected {
		t.Fatal("expected adapter to be connected")
	}
}

func TestListAndGetEC2(t *testing.T) {
	ctx := context.Background()

	t.Run("list success", func(t *testing.T) {
		adapter := &Adapter{connected: true, ec2Client: fakeEC2Client{describeInstances: func(_ context.Context, _ *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
			return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{{InstanceId: strPtr("i-1")}}}}}, nil
		}}}

		instances, err := adapter.ListEC2Instances(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(instances) != 1 || instances[0].InstanceID != "i-1" {
			t.Fatalf("unexpected list result: %+v", instances)
		}
	})

	t.Run("list wraps error", func(t *testing.T) {
		adapter := &Adapter{connected: true, ec2Client: fakeEC2Client{describeInstances: func(_ context.Context, _ *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
			return nil, errors.New("boom")
		}}}

		_, err := adapter.ListEC2Instances(ctx)
		if err == nil || !strings.Contains(err.Error(), "describe EC2 instances") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("get success", func(t *testing.T) {
		adapter := &Adapter{connected: true, ec2Client: fakeEC2Client{describeInstances: func(_ context.Context, _ *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
			return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{{InstanceId: strPtr("i-42")}}}}}, nil
		}}}

		instance, err := adapter.GetEC2Instance(ctx, "i-42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if instance == nil || instance.InstanceID != "i-42" {
			t.Fatalf("unexpected instance: %+v", instance)
		}
	})

	t.Run("get wraps error", func(t *testing.T) {
		adapter := &Adapter{connected: true, ec2Client: fakeEC2Client{describeInstances: func(_ context.Context, _ *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
			return nil, errors.New("failed")
		}}}

		_, err := adapter.GetEC2Instance(ctx, "i-42")
		if err == nil || !strings.Contains(err.Error(), "describe EC2 instance i-42") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("get not found", func(t *testing.T) {
		adapter := &Adapter{connected: true, ec2Client: fakeEC2Client{describeInstances: func(_ context.Context, _ *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
			return &ec2.DescribeInstancesOutput{}, nil
		}}}

		_, err := adapter.GetEC2Instance(ctx, "i-missing")
		if !errors.Is(err, ErrInstanceNotFound) {
			t.Fatalf("expected ErrInstanceNotFound, got %v", err)
		}
	})
}

func TestListAndGetEKS(t *testing.T) {
	ctx := context.Background()

	t.Run("list wraps list error", func(t *testing.T) {
		adapter := &Adapter{connected: true, eksClient: fakeEKSClient{listClusters: func(_ context.Context, _ *eks.ListClustersInput, _ ...func(*eks.Options)) (*eks.ListClustersOutput, error) {
			return nil, errors.New("failed")
		}}}

		_, err := adapter.ListEKSClusters(ctx)
		if err == nil || !strings.Contains(err.Error(), "list EKS clusters") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("list continues on describe error", func(t *testing.T) {
		adapter := &Adapter{connected: true, eksClient: fakeEKSClient{
			listClusters: func(_ context.Context, _ *eks.ListClustersInput, _ ...func(*eks.Options)) (*eks.ListClustersOutput, error) {
				return &eks.ListClustersOutput{Clusters: []string{"bad", "good"}}, nil
			},
			describeCluster: func(_ context.Context, in *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
				if in.Name != nil && *in.Name == "bad" {
					return nil, errors.New("describe failed")
				}
				return &eks.DescribeClusterOutput{Cluster: &ekstypes.Cluster{Name: strPtr("good")}}, nil
			},
		}}

		clusters, err := adapter.ListEKSClusters(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(clusters) != 1 || clusters[0].Name != "good" {
			t.Fatalf("unexpected clusters result: %+v", clusters)
		}
	})

	t.Run("get wraps describe error", func(t *testing.T) {
		adapter := &Adapter{connected: true, eksClient: fakeEKSClient{describeCluster: func(_ context.Context, _ *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
			return nil, errors.New("boom")
		}}}

		_, err := adapter.GetEKSCluster(ctx, "c1")
		if err == nil || !strings.Contains(err.Error(), "describe EKS cluster c1") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("get not found", func(t *testing.T) {
		adapter := &Adapter{connected: true, eksClient: fakeEKSClient{describeCluster: func(_ context.Context, _ *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
			return &eks.DescribeClusterOutput{Cluster: nil}, nil
		}}}

		_, err := adapter.GetEKSCluster(ctx, "missing")
		if !errors.Is(err, ErrClusterNotFound) {
			t.Fatalf("expected ErrClusterNotFound, got %v", err)
		}
	})
}

func TestListAndGetRDS(t *testing.T) {
	ctx := context.Background()

	t.Run("list success", func(t *testing.T) {
		adapter := &Adapter{connected: true, rdsClient: fakeRDSClient{describeDBInstances: func(_ context.Context, _ *rds.DescribeDBInstancesInput, _ ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
			return &rds.DescribeDBInstancesOutput{DBInstances: []rdstypes.DBInstance{{DBInstanceIdentifier: strPtr("db-1")}}}, nil
		}}}

		instances, err := adapter.ListRDSInstances(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(instances) != 1 || instances[0].DBInstanceID != "db-1" {
			t.Fatalf("unexpected instances: %+v", instances)
		}
	})

	t.Run("list wraps error", func(t *testing.T) {
		adapter := &Adapter{connected: true, rdsClient: fakeRDSClient{describeDBInstances: func(_ context.Context, _ *rds.DescribeDBInstancesInput, _ ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
			return nil, errors.New("nope")
		}}}

		_, err := adapter.ListRDSInstances(ctx)
		if err == nil || !strings.Contains(err.Error(), "describe RDS instances") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("get wraps error", func(t *testing.T) {
		adapter := &Adapter{connected: true, rdsClient: fakeRDSClient{describeDBInstances: func(_ context.Context, _ *rds.DescribeDBInstancesInput, _ ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
			return nil, errors.New("failed")
		}}}

		_, err := adapter.GetRDSInstance(ctx, "db-x")
		if err == nil || !strings.Contains(err.Error(), "describe RDS instance db-x") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("get not found", func(t *testing.T) {
		adapter := &Adapter{connected: true, rdsClient: fakeRDSClient{describeDBInstances: func(_ context.Context, _ *rds.DescribeDBInstancesInput, _ ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
			return &rds.DescribeDBInstancesOutput{}, nil
		}}}

		_, err := adapter.GetRDSInstance(ctx, "missing")
		if !errors.Is(err, ErrDBInstanceNotFound) {
			t.Fatalf("expected ErrDBInstanceNotFound, got %v", err)
		}
	})
}

func TestListAndGetVPCAndSubnets(t *testing.T) {
	ctx := context.Background()

	t.Run("list wraps vpc error", func(t *testing.T) {
		adapter := &Adapter{connected: true, ec2Client: fakeEC2Client{describeVpcs: func(_ context.Context, _ *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
			return nil, errors.New("failed")
		}}}

		_, err := adapter.ListVPCs(ctx)
		if err == nil || !strings.Contains(err.Error(), "describe VPCs") {
			t.Fatalf("expected wrapped error, got %v", err)
		}
	})

	t.Run("list skips vpc when subnet lookup fails", func(t *testing.T) {
		adapter := &Adapter{connected: true, ec2Client: fakeEC2Client{
			describeVpcs: func(_ context.Context, _ *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
				return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{{VpcId: strPtr("vpc-1")}, {VpcId: strPtr("vpc-2")}}}, nil
			},
			describeSubnets: func(_ context.Context, in *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
				if len(in.Filters) > 0 && len(in.Filters[0].Values) > 0 && in.Filters[0].Values[0] == "vpc-1" {
					return nil, errors.New("subnet fail")
				}
				return &ec2.DescribeSubnetsOutput{}, nil
			},
		}}

		vpcs, err := adapter.ListVPCs(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(vpcs) != 1 || vpcs[0].VpcID != "vpc-2" {
			t.Fatalf("unexpected VPC list: %+v", vpcs)
		}
	})

	t.Run("get not found", func(t *testing.T) {
		adapter := &Adapter{connected: true, ec2Client: fakeEC2Client{describeVpcs: func(_ context.Context, _ *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
			return &ec2.DescribeVpcsOutput{}, nil
		}}}

		_, err := adapter.GetVPC(ctx, "vpc-missing")
		if !errors.Is(err, ErrVPCNotFound) {
			t.Fatalf("expected ErrVPCNotFound, got %v", err)
		}
	})

	t.Run("get wraps subnet error", func(t *testing.T) {
		adapter := &Adapter{connected: true, ec2Client: fakeEC2Client{
			describeVpcs: func(_ context.Context, _ *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
				return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{{VpcId: strPtr("vpc-1")}}}, nil
			},
			describeSubnets: func(_ context.Context, _ *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
				return nil, errors.New("subnets failed")
			},
		}}

		_, err := adapter.GetVPC(ctx, "vpc-1")
		if err == nil || !strings.Contains(err.Error(), "get subnets for VPC vpc-1") {
			t.Fatalf("expected wrapped subnet error, got %v", err)
		}
	})

	t.Run("get success with subnets", func(t *testing.T) {
		adapter := &Adapter{connected: true, ec2Client: fakeEC2Client{
			describeVpcs: func(_ context.Context, _ *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
				return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{{VpcId: strPtr("vpc-1")}}}, nil
			},
			describeSubnets: func(_ context.Context, _ *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
				return &ec2.DescribeSubnetsOutput{Subnets: []ec2types.Subnet{{SubnetId: strPtr("subnet-1"), VpcId: strPtr("vpc-1")}}}, nil
			},
		}}

		vpc, err := adapter.GetVPC(ctx, "vpc-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if vpc == nil || len(vpc.Subnets) != 1 || vpc.Subnets[0].SubnetID != "subnet-1" {
			t.Fatalf("unexpected vpc result: %+v", vpc)
		}
	})

	t.Run("get subnets helper wraps error", func(t *testing.T) {
		adapter := &Adapter{connected: true, ec2Client: fakeEC2Client{describeSubnets: func(_ context.Context, _ *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
			return nil, errors.New("subnet boom")
		}}}

		_, err := adapter.getSubnetsForVPC(ctx, "vpc-1")
		if err == nil || !strings.Contains(err.Error(), "describe subnets for VPC vpc-1") {
			t.Fatalf("expected wrapped subnet error, got %v", err)
		}
	})
}
