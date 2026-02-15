package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/paths"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/internal/tools"
	coretools "github.com/jaimegago/joe/internal/tools/core"
	_ "github.com/mattn/go-sqlite3"
)

// MockAWSAdapter for integration testing
type MockAWSAdapter struct {
	instances []awsadapter.EC2Instance
	clusters  []awsadapter.EKSCluster
	error     error
}

func (m *MockAWSAdapter) Connect(source store.Source) error { return m.error }
func (m *MockAWSAdapter) Disconnect() error                 { return nil }
func (m *MockAWSAdapter) Status() adapters.Status {
	return adapters.Status{Connected: m.error == nil, Message: "Mock AWS adapter"}
}

func (m *MockAWSAdapter) ListEC2Instances(ctx context.Context) ([]awsadapter.EC2Instance, error) {
	if m.error != nil {
		return nil, m.error
	}
	return m.instances, nil
}

func (m *MockAWSAdapter) GetEC2Instance(ctx context.Context, instanceID string) (*awsadapter.EC2Instance, error) {
	if m.error != nil {
		return nil, m.error
	}
	for _, instance := range m.instances {
		if instance.InstanceID == instanceID {
			return &instance, nil
		}
	}
	return nil, nil
}

func (m *MockAWSAdapter) ListEKSClusters(ctx context.Context) ([]awsadapter.EKSCluster, error) {
	if m.error != nil {
		return nil, m.error
	}
	return m.clusters, nil
}

func (m *MockAWSAdapter) GetEKSCluster(ctx context.Context, clusterName string) (*awsadapter.EKSCluster, error) {
	if m.error != nil {
		return nil, m.error
	}
	for _, cluster := range m.clusters {
		if cluster.Name == clusterName {
			return &cluster, nil
		}
	}
	return nil, nil
}

func (m *MockAWSAdapter) ListRDSInstances(ctx context.Context) ([]awsadapter.RDSInstance, error) {
	return []awsadapter.RDSInstance{}, nil
}

func (m *MockAWSAdapter) GetRDSInstance(ctx context.Context, dbInstanceID string) (*awsadapter.RDSInstance, error) {
	return nil, nil
}

func (m *MockAWSAdapter) ListVPCs(ctx context.Context) ([]awsadapter.VPC, error) {
	return []awsadapter.VPC{}, nil
}

func (m *MockAWSAdapter) GetVPC(ctx context.Context, vpcID string) (*awsadapter.VPC, error) {
	return nil, nil
}

func TestAWSIntegration(t *testing.T) {
	// Create test database
	db, err := store.New(":memory:"+paths.DatabaseFlags, nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	// Create adapter registry with mock AWS adapter
	registry := adapters.NewRegistry()
	mockAdapter := &MockAWSAdapter{
		instances: []awsadapter.EC2Instance{
			{
				InstanceID:       "i-1234567890abcdef0",
				InstanceType:     "t3.micro",
				State:            "running",
				PublicIP:         "1.2.3.4",
				PrivateIP:        "10.0.1.100",
				VpcID:            "vpc-1234567890abcdef0",
				AvailabilityZone: "us-west-2a",
				Tags: map[string]string{
					"Name":        "test-instance",
					"Environment": "testing",
				},
			},
		},
		clusters: []awsadapter.EKSCluster{
			{
				Name:    "test-cluster",
				Status:  "ACTIVE",
				Version: "1.25",
				Tags: map[string]string{
					"Environment": "testing",
				},
			},
		},
	}

	// Create core services
	cfg := &config.Config{}
	services := core.New(cfg, db, db.DB(), registry, nil)

	// Set up HTTP server
	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	// Create client
	coreClient := client.New(testServer.URL)

	t.Run("AddAWSSource", func(t *testing.T) {
		// Create AWS source
		source := store.Source{
			ID:     "test-aws-source",
			Name:   "Test AWS Account",
			Type:   "aws",
			Config: []byte(`{"region":"us-west-2","profile":"default"}`),
		}

		err := db.Sources.Create(context.Background(), &source)
		if err != nil {
			t.Fatalf("create source: %v", err)
		}

		// Register the mock adapter for this source
		registry.Register(source.ID, mockAdapter)

		// Test connection status via API
		sources, err := coreClient.ListSources(context.Background())
		if err != nil {
			t.Fatalf("list sources: %v", err)
		}

		if len(sources) == 0 {
			t.Fatal("expected at least one source")
		}

		found := false
		for _, s := range sources {
			if s.ID == source.ID {
				found = true
				break
			}
		}
		if !found {
			t.Error("AWS source not found in source list")
		}
	})

	t.Run("QueryEC2viaTool", func(t *testing.T) {
		// Test AWS EC2 tool
		tool := coretools.NewAWSEC2Tool(coreClient)

		result, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "test-aws-source",
		})
		if err != nil {
			t.Fatalf("execute aws_ec2 tool: %v", err)
		}

		// Verify result structure
		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatal("result should be a map")
		}

		instances, ok := resultMap["instances"].([]awsadapter.EC2Instance)
		if !ok {
			t.Fatal("instances field should be a slice of EC2Instance")
		}

		if len(instances) != 1 {
			t.Errorf("expected 1 instance, got %d", len(instances))
		}

		instance := instances[0]
		if instance.InstanceID != "i-1234567890abcdef0" {
			t.Errorf("instance ID: got %q, want %q", instance.InstanceID, "i-1234567890abcdef0")
		}

		if instance.State != "running" {
			t.Errorf("instance state: got %q, want %q", instance.State, "running")
		}
	})

	t.Run("QueryEKSviaTool", func(t *testing.T) {
		// Test AWS EKS tool
		tool := coretools.NewAWSEKSTool(coreClient)

		result, err := tool.Execute(context.Background(), map[string]any{
			"source_id": "test-aws-source",
		})
		if err != nil {
			t.Fatalf("execute aws_eks tool: %v", err)
		}

		// Verify result structure
		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatal("result should be a map")
		}

		clusters, ok := resultMap["clusters"].([]awsadapter.EKSCluster)
		if !ok {
			t.Fatal("clusters field should be a slice of EKSCluster")
		}

		if len(clusters) != 1 {
			t.Errorf("expected 1 cluster, got %d", len(clusters))
		}

		cluster := clusters[0]
		if cluster.Name != "test-cluster" {
			t.Errorf("cluster name: got %q, want %q", cluster.Name, "test-cluster")
		}

		if cluster.Status != "ACTIVE" {
			t.Errorf("cluster status: got %q, want %q", cluster.Status, "ACTIVE")
		}
	})

	t.Run("QuerySpecificInstance", func(t *testing.T) {
		// Test getting specific EC2 instance
		tool := coretools.NewAWSEC2Tool(coreClient)

		result, err := tool.Execute(context.Background(), map[string]any{
			"source_id":   "test-aws-source",
			"instance_id": "i-1234567890abcdef0",
		})
		if err != nil {
			t.Fatalf("execute aws_ec2 tool for specific instance: %v", err)
		}

		// Verify result structure
		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatal("result should be a map")
		}

		instancePtr, ok := resultMap["instance"].(*awsadapter.EC2Instance)
		if !ok {
			t.Fatal("instance field should be a pointer to EC2Instance")
		}

		if instancePtr.InstanceID != "i-1234567890abcdef0" {
			t.Errorf("instance ID: got %q, want %q", instancePtr.InstanceID, "i-1234567890abcdef0")
		}
	})

	t.Run("ToolRegistry", func(t *testing.T) {
		// Test that AWS tools are properly registered
		registry := tools.NewDefaultRegistryWithClient(coreClient, nil)

		expectedTools := []string{"aws_ec2", "aws_eks", "aws_rds", "aws_vpc"}
		for _, toolName := range expectedTools {
			tool, err := registry.Get(toolName)
			if err != nil {
				t.Errorf("tool %q not found: %v", toolName, err)
				continue
			}
			if tool == nil {
				t.Errorf("tool %q is nil", toolName)
			}
		}
	})
}
