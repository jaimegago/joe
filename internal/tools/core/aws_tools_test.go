package core_test

import (
	"context"
	"errors"
	"testing"

	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	"github.com/jaimegago/joe/internal/client"
	coretools "github.com/jaimegago/joe/internal/tools/core"
)

type fakeEKSRDSVPCClient struct {
	AWSEKSListClustersFunc  func(ctx context.Context, sourceID string) ([]*awsadapter.EKSCluster, error)
	AWSEKSGetClusterFunc    func(ctx context.Context, sourceID, clusterName string) (*awsadapter.EKSCluster, error)
	AWSRDSListInstancesFunc func(ctx context.Context, sourceID string) ([]*awsadapter.RDSInstance, error)
	AWSRDSGetInstanceFunc   func(ctx context.Context, sourceID, instanceID string) (*awsadapter.RDSInstance, error)
	AWSVPCListFunc          func(ctx context.Context, sourceID string) ([]*awsadapter.VPC, error)
	AWSVPCGetFunc           func(ctx context.Context, sourceID, vpcID string) (*awsadapter.VPC, error)
}

func (f *fakeEKSRDSVPCClient) AWSEKSListClusters(ctx context.Context, sourceID string) ([]*awsadapter.EKSCluster, error) {
	return f.AWSEKSListClustersFunc(ctx, sourceID)
}
func (f *fakeEKSRDSVPCClient) AWSEKSGetCluster(ctx context.Context, sourceID, clusterName string) (*awsadapter.EKSCluster, error) {
	return f.AWSEKSGetClusterFunc(ctx, sourceID, clusterName)
}
func (f *fakeEKSRDSVPCClient) AWSRDSListInstances(ctx context.Context, sourceID string) ([]*awsadapter.RDSInstance, error) {
	return f.AWSRDSListInstancesFunc(ctx, sourceID)
}
func (f *fakeEKSRDSVPCClient) AWSRDSGetInstance(ctx context.Context, sourceID, instanceID string) (*awsadapter.RDSInstance, error) {
	return f.AWSRDSGetInstanceFunc(ctx, sourceID, instanceID)
}
func (f *fakeEKSRDSVPCClient) AWSVPCList(ctx context.Context, sourceID string) ([]*awsadapter.VPC, error) {
	return f.AWSVPCListFunc(ctx, sourceID)
}
func (f *fakeEKSRDSVPCClient) AWSVPCGet(ctx context.Context, sourceID, vpcID string) (*awsadapter.VPC, error) {
	return f.AWSVPCGetFunc(ctx, sourceID, vpcID)
}

func TestAWSEKSTool_Execute(t *testing.T) {
	fake := &fakeEKSRDSVPCClient{
		AWSEKSListClustersFunc: func(ctx context.Context, sourceID string) ([]*awsadapter.EKSCluster, error) {
			if sourceID == "src" {
				return []*awsadapter.EKSCluster{{Name: "eks-1"}, {Name: "eks-2"}}, nil
			}
			return nil, errors.New("bad source")
		},
		AWSEKSGetClusterFunc: func(ctx context.Context, sourceID, clusterName string) (*awsadapter.EKSCluster, error) {
			if sourceID == "src" && clusterName == "eks-1" {
				return &awsadapter.EKSCluster{Name: "eks-1"}, nil
			}
			return nil, errors.New("not found")
		},
	}
	tool := coretools.NewAWSEKSTool(fake)
	tests := []struct {
		name     string
		args     map[string]any
		wantErr  bool
		wantList bool
		wantGet  bool
	}{
		{"missing source_id", map[string]any{}, true, false, false},
		{"list success", map[string]any{"source_id": "src"}, false, true, false},
		{"list error", map[string]any{"source_id": "bad"}, true, false, false},
		{"get success", map[string]any{"source_id": "src", "cluster_name": "eks-1"}, false, false, true},
		{"get error", map[string]any{"source_id": "src", "cluster_name": "bad"}, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				m := res.(map[string]any)
				if tt.wantList {
					clusters := m["clusters"].([]*awsadapter.EKSCluster)
					if len(clusters) != 2 {
						t.Errorf("expected 2 clusters, got %v", len(clusters))
					}
				}
				if tt.wantGet {
					cl := m["cluster"].(*awsadapter.EKSCluster)
					if cl.Name != "eks-1" {
						t.Errorf("expected cluster name eks-1, got %v", cl.Name)
					}
				}
			}
		})
	}
}

func TestAWSRDSTool_Execute(t *testing.T) {
	fake := &fakeEKSRDSVPCClient{
		AWSRDSListInstancesFunc: func(ctx context.Context, sourceID string) ([]*awsadapter.RDSInstance, error) {
			if sourceID == "src" {
				return []*awsadapter.RDSInstance{{DBInstanceID: "rds-1"}, {DBInstanceID: "rds-2"}}, nil
			}
			return nil, errors.New("bad source")
		},
		AWSRDSGetInstanceFunc: func(ctx context.Context, sourceID, instanceID string) (*awsadapter.RDSInstance, error) {
			if sourceID == "src" && instanceID == "rds-1" {
				return &awsadapter.RDSInstance{DBInstanceID: "rds-1"}, nil
			}
			return nil, errors.New("not found")
		},
	}
	tool := coretools.NewAWSRDSTool(fake)
	tests := []struct {
		name     string
		args     map[string]any
		wantErr  bool
		wantList bool
		wantGet  bool
	}{
		{"missing source_id", map[string]any{}, true, false, false},
		{"list success", map[string]any{"source_id": "src"}, false, true, false},
		{"list error", map[string]any{"source_id": "bad"}, true, false, false},
		{"get success", map[string]any{"source_id": "src", "db_instance_id": "rds-1"}, false, false, true},
		{"get error", map[string]any{"source_id": "src", "db_instance_id": "bad"}, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tool.Execute(context.Background(), tt.args)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			m := res.(map[string]any)
			if tt.wantList {
				instances := m["instances"].([]*awsadapter.RDSInstance)
				if len(instances) != 2 {
					t.Errorf("expected 2 instances, got %v", len(instances))
				}
			}
			if tt.wantGet {
				instanceVal := m["instance"]
				if instanceVal == nil {
					t.Errorf("expected instance, got nil")
					return
				}
				inst, ok := instanceVal.(*awsadapter.RDSInstance)
				if !ok {
					t.Errorf("expected *RDSInstance, got %T", instanceVal)
					return
				}
				if inst.DBInstanceID != "rds-1" {
					t.Errorf("expected instance id rds-1, got %v", inst.DBInstanceID)
				}
			}
		})
	}
}

func TestAWSVPCTool_Execute(t *testing.T) {
	fake := &fakeEKSRDSVPCClient{
		AWSVPCListFunc: func(ctx context.Context, sourceID string) ([]*awsadapter.VPC, error) {
			if sourceID == "src" {
				return []*awsadapter.VPC{{VpcID: "vpc-1"}, {VpcID: "vpc-2"}}, nil
			}
			return nil, errors.New("bad source")
		},
		AWSVPCGetFunc: func(ctx context.Context, sourceID, vpcID string) (*awsadapter.VPC, error) {
			if sourceID == "src" && vpcID == "vpc-1" {
				return &awsadapter.VPC{VpcID: "vpc-1"}, nil
			}
			return nil, errors.New("not found")
		},
	}
	tool := coretools.NewAWSVPCTool(fake)
	tests := []struct {
		name     string
		args     map[string]any
		wantErr  bool
		wantList bool
		wantGet  bool
	}{
		{"missing source_id", map[string]any{}, true, false, false},
		{"list success", map[string]any{"source_id": "src"}, false, true, false},
		{"list error", map[string]any{"source_id": "bad"}, true, false, false},
		{"get success", map[string]any{"source_id": "src", "vpc_id": "vpc-1"}, false, false, true},
		{"get error", map[string]any{"source_id": "src", "vpc_id": "bad"}, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				m := res.(map[string]any)
				if tt.wantList {
					vpcs := m["vpcs"].([]*awsadapter.VPC)
					if len(vpcs) != 2 {
						t.Errorf("expected 2 vpcs, got %v", len(vpcs))
					}
				}
				if tt.wantGet {
					vpc := m["vpc"].(*awsadapter.VPC)
					if vpc.VpcID != "vpc-1" {
						t.Errorf("expected vpc id vpc-1, got %v", vpc.VpcID)
					}
				}
			}
		})
	}
}

func TestAWSEC2Tool(t *testing.T) {
	tool := coretools.NewAWSEC2Tool(&client.Client{})

	if tool.Name() != "aws_ec2" {
		t.Errorf("tool name: got %q, want %q", tool.Name(), "aws_ec2")
	}

	desc := tool.Description()
	if desc == "" {
		t.Error("tool description should not be empty")
	}

	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("parameter type: got %q, want %q", params.Type, "object")
	}
}

func TestAWSToolsRegistration(t *testing.T) {
	// Test that all AWS tools are correctly named

}
