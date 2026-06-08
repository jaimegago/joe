package coreagent

import (
	"context"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/store"
)

type fakeAWSAdapter struct {
	ec2Instances []awsadapter.EC2Instance
	eksClusters  []awsadapter.EKSCluster
	rdsInstances []awsadapter.RDSInstance
	vpcs         []awsadapter.VPC
}

func (f *fakeAWSAdapter) Connect(_ context.Context, _ store.Component) error { return nil }

func (f *fakeAWSAdapter) Disconnect() error { return nil }

func (f *fakeAWSAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true, Message: "connected"}
}

func (f *fakeAWSAdapter) ListEC2Instances(_ context.Context) ([]awsadapter.EC2Instance, error) {
	return f.ec2Instances, nil
}

func (f *fakeAWSAdapter) GetEC2Instance(_ context.Context, _ string) (*awsadapter.EC2Instance, error) {
	return nil, nil
}

func (f *fakeAWSAdapter) ListEKSClusters(_ context.Context) ([]awsadapter.EKSCluster, error) {
	return f.eksClusters, nil
}

func (f *fakeAWSAdapter) GetEKSCluster(_ context.Context, _ string) (*awsadapter.EKSCluster, error) {
	return nil, nil
}

func (f *fakeAWSAdapter) ListRDSInstances(_ context.Context) ([]awsadapter.RDSInstance, error) {
	return f.rdsInstances, nil
}

func (f *fakeAWSAdapter) GetRDSInstance(_ context.Context, _ string) (*awsadapter.RDSInstance, error) {
	return nil, nil
}

func (f *fakeAWSAdapter) ListVPCs(_ context.Context) ([]awsadapter.VPC, error) {
	return f.vpcs, nil
}

func (f *fakeAWSAdapter) GetVPC(_ context.Context, _ string) (*awsadapter.VPC, error) {
	return nil, nil
}

func TestRefreshAWSComponentMapping(t *testing.T) {
	graphStore := setupGraphStore(t)
	refresher := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}

	source := &store.Component{
		ID:     "src-aws-1",
		Type:   store.ComponentTypeAWS,
		Name:   "test-aws",
		Config: []byte(`{"region":"us-west-2"}`),
	}
	adapter := &fakeAWSAdapter{
		vpcs: []awsadapter.VPC{
			{VpcID: "vpc-1", CidrBlock: "10.0.0.0/16", State: "available", IsDefault: false},
		},
		ec2Instances: []awsadapter.EC2Instance{
			{InstanceID: "i-1", InstanceType: "t3.small", State: "running", VpcID: "vpc-1"},
		},
		eksClusters: []awsadapter.EKSCluster{
			{Name: "cluster-1", ARN: "arn:aws:eks:cluster-1", Version: "1.29", Status: "ACTIVE", VpcID: "vpc-1"},
		},
		rdsInstances: []awsadapter.RDSInstance{
			{DBInstanceID: "db-1", Engine: "postgres", EngineVersion: "15.4", Status: "available", VpcID: "vpc-1"},
		},
	}

	if err := refresher.refreshAWSComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshAWSComponent error: %v", err)
	}

	nodes, edges, err := LoadGraphStateForComponent(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent error: %v", err)
	}

	if len(nodes) != 4 {
		t.Fatalf("nodes count = %d, want 4", len(nodes))
	}

	requireEdge(t, edges, "aws/src-aws-1/ec2/i-1", "aws/src-aws-1/vpc/vpc-1", "in_vpc")
	requireEdge(t, edges, "aws/src-aws-1/eks/cluster-1", "aws/src-aws-1/vpc/vpc-1", "in_vpc")
	requireEdge(t, edges, "aws/src-aws-1/rds/db-1", "aws/src-aws-1/vpc/vpc-1", "in_vpc")
}

func TestRefreshAWSComponent_IsK8sNodeEdge(t *testing.T) {
	graphStore := setupGraphStore(t)

	// Pre-seed a K8s node with an InternalIP matching the EC2 instance.
	k8sNode := graph.Node{
		ID:          "k8s/src-k8s/node/ip-10-0-1-5",
		Type:        "node",
		ComponentID: "src-k8s",
		Metadata:    map[string]any{"name": "ip-10-0-1-5", "internal_ip": "10.0.1.5"},
	}
	if err := graphStore.AddNode(context.Background(), k8sNode); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	refresher := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-aws-k8s", Type: store.ComponentTypeAWS, Name: "aws"}
	adapter := &fakeAWSAdapter{
		ec2Instances: []awsadapter.EC2Instance{
			{InstanceID: "i-abc", InstanceType: "m5.large", State: "running", PrivateIP: "10.0.1.5"},
		},
	}

	if err := refresher.refreshAWSComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshAWSComponent error: %v", err)
	}

	// Cross-source edges span both endpoints; query with both node IDs.
	ec2NodeID := "aws/src-aws-k8s/ec2/i-abc"
	k8sNodeID := "k8s/src-k8s/node/ip-10-0-1-5"
	edges, err := graphStore.ListEdgesForNodes(context.Background(), []string{ec2NodeID, k8sNodeID})
	if err != nil {
		t.Fatalf("ListEdgesForNodes error: %v", err)
	}
	requireEdge(t, edges, ec2NodeID, k8sNodeID, "is_k8s_node")
}

func TestRefreshAWSComponent_IsK8sNodeEdge_NoMatch(t *testing.T) {
	graphStore := setupGraphStore(t)

	refresher := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}
	source := &store.Component{ID: "src-aws-nomatch", Type: store.ComponentTypeAWS, Name: "aws"}
	adapter := &fakeAWSAdapter{
		ec2Instances: []awsadapter.EC2Instance{
			{InstanceID: "i-xyz", InstanceType: "t3.micro", State: "running", PrivateIP: "192.168.1.1"},
		},
	}

	// No K8s nodes in graph → no is_k8s_node edges, no error.
	if err := refresher.refreshAWSComponent(context.Background(), source, adapter); err != nil {
		t.Fatalf("refreshAWSComponent error: %v", err)
	}
}

var _ awsadapter.AWSAdapter = (*fakeAWSAdapter)(nil)
