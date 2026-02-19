package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/store"
	"github.com/jaimegago/joe/test/mocks"
)

// MockAWSAdapter implements the AWS adapter interface for testing
type MockAWSAdapter struct {
	EC2Instances []awsadapter.EC2Instance
	EKSClusters  []awsadapter.EKSCluster
	RDSInstances []awsadapter.RDSInstance
	VPCs         []awsadapter.VPC
	Error        error
}

func (m *MockAWSAdapter) Status() adapters.Status {
	return adapters.Status{Connected: true}
}

func (m *MockAWSAdapter) Connect(_ context.Context, _ store.Source) error        { return nil }
func (m *MockAWSAdapter) Disconnect() error                                      { return nil }
func (m *MockAWSAdapter) Type() string                                           { return "aws" }
func (m *MockAWSAdapter) SupportsQuery(query string) bool                        { return false }
func (m *MockAWSAdapter) Query(ctx context.Context, query string) ([]any, error) { return nil, nil }

func (m *MockAWSAdapter) ListEC2Instances(ctx context.Context) ([]awsadapter.EC2Instance, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	return m.EC2Instances, nil
}

func (m *MockAWSAdapter) GetEC2Instance(ctx context.Context, instanceID string) (*awsadapter.EC2Instance, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	for _, instance := range m.EC2Instances {
		if instance.InstanceID == instanceID {
			return &instance, nil
		}
	}
	return nil, nil // Return nil instance when not found (no error)
}

func (m *MockAWSAdapter) ListEKSClusters(ctx context.Context) ([]awsadapter.EKSCluster, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	return m.EKSClusters, nil
}

func (m *MockAWSAdapter) GetEKSCluster(ctx context.Context, clusterName string) (*awsadapter.EKSCluster, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	for _, cluster := range m.EKSClusters {
		if cluster.Name == clusterName {
			return &cluster, nil
		}
	}
	return nil, awsadapter.ErrClusterNotFound
}

func (m *MockAWSAdapter) ListRDSInstances(ctx context.Context) ([]awsadapter.RDSInstance, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	return m.RDSInstances, nil
}

func (m *MockAWSAdapter) GetRDSInstance(ctx context.Context, dbInstanceID string) (*awsadapter.RDSInstance, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	for _, instance := range m.RDSInstances {
		if instance.DBInstanceID == dbInstanceID {
			return &instance, nil
		}
	}
	return nil, awsadapter.ErrDBInstanceNotFound
}

func (m *MockAWSAdapter) ListVPCs(ctx context.Context) ([]awsadapter.VPC, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	return m.VPCs, nil
}

func (m *MockAWSAdapter) GetVPC(ctx context.Context, vpcID string) (*awsadapter.VPC, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	for _, vpc := range m.VPCs {
		if vpc.VpcID == vpcID {
			return &vpc, nil
		}
	}
	return nil, awsadapter.ErrVPCNotFound
}

func setupAWSTestServer(t *testing.T, mock *MockAWSAdapter) (*api.Server, *http.ServeMux) {
	t.Helper()

	registry := adapters.NewRegistry()
	registry.Register("test-aws", mock)

	services := &core.Services{
		Config:   &config.Config{},
		Adapters: registry,
	}

	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	return server, mux
}

func TestHandleAWSEC2ListInstances(t *testing.T) {
	tests := []struct {
		name       string
		instances  []awsadapter.EC2Instance
		error      error
		wantStatus int
		wantCount  int
	}{
		{
			name: "list ec2 instances",
			instances: []awsadapter.EC2Instance{
				{
					InstanceID:   "i-1234567890abcdef0",
					InstanceType: "t3.micro",
					State:        "running",
					Tags:         map[string]string{"Name": "test-instance"},
				},
				{
					InstanceID:   "i-0987654321fedcba1",
					InstanceType: "t3.small",
					State:        "stopped",
					Tags:         map[string]string{"Name": "test-instance-2"},
				},
			},
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name:       "empty list",
			instances:  []awsadapter.EC2Instance{},
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "adapter error",
			error:      fmt.Errorf("AWS error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockAWSAdapter{
				EC2Instances: tt.instances,
				Error:        tt.error,
			}

			_, mux := setupAWSTestServer(t, mock)

			req := httptest.NewRequest("GET", "/api/v1/aws/test-aws/ec2/instances", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var response map[string]any
				if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
					t.Fatalf("decode response: %v", err)
				}

				count, ok := response["count"].(float64)
				if !ok {
					t.Fatal("count field missing or wrong type")
				}
				if int(count) != tt.wantCount {
					t.Errorf("count: got %d, want %d", int(count), tt.wantCount)
				}

				instances, ok := response["instances"].([]any)
				if !ok {
					t.Fatal("instances field missing or wrong type")
				}
				if len(instances) != tt.wantCount {
					t.Errorf("instances length: got %d, want %d", len(instances), tt.wantCount)
				}
			}
		})
	}
}

func TestHandleAWSEC2GetInstance(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		instances  []awsadapter.EC2Instance
		error      error
		wantStatus int
		wantFound  bool
	}{
		{
			name:       "get existing instance",
			instanceID: "i-1234567890abcdef0",
			instances: []awsadapter.EC2Instance{
				{
					InstanceID:   "i-1234567890abcdef0",
					InstanceType: "t3.micro",
					State:        "running",
					Tags:         map[string]string{"Name": "test-instance"},
				},
			},
			wantStatus: http.StatusOK,
			wantFound:  true,
		},
		{
			name:       "instance not found",
			instanceID: "i-nonexistent",
			instances: []awsadapter.EC2Instance{
				{
					InstanceID:   "i-1234567890abcdef0",
					InstanceType: "t3.micro",
					State:        "running",
					Tags:         map[string]string{"Name": "test-instance"},
				},
			},
			wantStatus: http.StatusNotFound,
			wantFound:  false,
		},
		{
			name:       "adapter error",
			instanceID: "i-1234567890abcdef0",
			error:      fmt.Errorf("AWS error"),
			wantStatus: http.StatusInternalServerError,
			wantFound:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockAWSAdapter{
				EC2Instances: tt.instances,
				Error:        tt.error,
			}

			_, mux := setupAWSTestServer(t, mock)

			url := fmt.Sprintf("/api/v1/aws/test-aws/ec2/instances/%s", tt.instanceID)
			req := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantFound && w.Code == http.StatusOK {
				var response map[string]any
				if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
					t.Fatalf("decode response: %v", err)
				}

				instanceResp, ok := response["instance"].(map[string]any)
				if !ok {
					t.Fatal("instance field missing or wrong type")
				}
				if instanceResp["instance_id"] != tt.instanceID {
					t.Errorf("instance_id: got %v, want %s", instanceResp["instance_id"], tt.instanceID)
				}
			}
		})
	}
}

func TestHandleAWSMissingSource(t *testing.T) {
	// Don't register any sources
	registry := adapters.NewRegistry()

	services := &core.Services{
		Config:   &config.Config{},
		Adapters: registry,
	}

	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	endpoints := []string{
		"/api/v1/aws/nonexistent/ec2/instances",
		"/api/v1/aws/nonexistent/eks/clusters",
		"/api/v1/aws/nonexistent/rds/instances",
		"/api/v1/aws/nonexistent/vpc/vpcs",
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			req := httptest.NewRequest("GET", endpoint, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
			}
		})
	}
}

func TestHandleAWSWrongAdapterType(t *testing.T) {
	// Register a mock K8s adapter instead of AWS
	registry := adapters.NewRegistry()
	registry.Register("test-k8s", &mocks.MockK8sAdapter{})

	services := &core.Services{
		Config:   &config.Config{},
		Adapters: registry,
	}

	server := api.New(services)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/v1/aws/test-k8s/ec2/instances", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}

	var response struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error != "invalid_source" {
		t.Errorf("error code: got %q, want %q", response.Error, "invalid_source")
	}
	if response.Message != "source is not an AWS adapter" {
		t.Errorf("error message: got %q, want %q", response.Message, "source is not an AWS adapter")
	}
}

func TestHandleAWSEKSListClusters(t *testing.T) {
	tests := []struct {
		name       string
		clusters   []awsadapter.EKSCluster
		err        error
		wantStatus int
		wantCount  int
	}{
		{
			name: "list eks clusters",
			clusters: []awsadapter.EKSCluster{
				{Name: "prod-cluster", Status: "ACTIVE"},
				{Name: "dev-cluster", Status: "ACTIVE"},
			},
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name:       "empty list",
			clusters:   []awsadapter.EKSCluster{},
			wantStatus: http.StatusOK,
			wantCount:  0,
		},
		{
			name:       "adapter error",
			err:        fmt.Errorf("EKS error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockAWSAdapter{EKSClusters: tt.clusters, Error: tt.err}
			_, mux := setupAWSTestServer(t, mock)

			req := httptest.NewRequest("GET", "/api/v1/aws/test-aws/eks/clusters", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if int(resp["count"].(float64)) != tt.wantCount {
					t.Errorf("count: got %v, want %d", resp["count"], tt.wantCount)
				}
			}
		})
	}
}

func TestHandleAWSEKSGetCluster(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		clusters    []awsadapter.EKSCluster
		err         error
		wantStatus  int
	}{
		{
			name:        "get existing cluster",
			clusterName: "prod-cluster",
			clusters:    []awsadapter.EKSCluster{{Name: "prod-cluster", Status: "ACTIVE"}},
			wantStatus:  http.StatusOK,
		},
		{
			name:        "cluster not found",
			clusterName: "missing-cluster",
			clusters:    []awsadapter.EKSCluster{{Name: "prod-cluster", Status: "ACTIVE"}},
			wantStatus:  http.StatusNotFound,
		},
		{
			name:        "adapter error",
			clusterName: "prod-cluster",
			err:         fmt.Errorf("EKS error"),
			wantStatus:  http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockAWSAdapter{EKSClusters: tt.clusters, Error: tt.err}
			_, mux := setupAWSTestServer(t, mock)

			url := fmt.Sprintf("/api/v1/aws/test-aws/eks/clusters/%s", tt.clusterName)
			req := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleAWSRDSListInstances(t *testing.T) {
	tests := []struct {
		name       string
		instances  []awsadapter.RDSInstance
		err        error
		wantStatus int
		wantCount  int
	}{
		{
			name: "list rds instances",
			instances: []awsadapter.RDSInstance{
				{DBInstanceID: "db-1", Engine: "postgres", Status: "available"},
			},
			wantStatus: http.StatusOK,
			wantCount:  1,
		},
		{
			name:       "adapter error",
			err:        fmt.Errorf("RDS error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockAWSAdapter{RDSInstances: tt.instances, Error: tt.err}
			_, mux := setupAWSTestServer(t, mock)

			req := httptest.NewRequest("GET", "/api/v1/aws/test-aws/rds/instances", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if int(resp["count"].(float64)) != tt.wantCount {
					t.Errorf("count: got %v, want %d", resp["count"], tt.wantCount)
				}
			}
		})
	}
}

func TestHandleAWSRDSGetInstance(t *testing.T) {
	tests := []struct {
		name         string
		dbInstanceID string
		instances    []awsadapter.RDSInstance
		err          error
		wantStatus   int
	}{
		{
			name:         "get existing instance",
			dbInstanceID: "db-1",
			instances:    []awsadapter.RDSInstance{{DBInstanceID: "db-1", Engine: "postgres"}},
			wantStatus:   http.StatusOK,
		},
		{
			name:         "instance not found",
			dbInstanceID: "missing-db",
			instances:    []awsadapter.RDSInstance{{DBInstanceID: "db-1", Engine: "postgres"}},
			wantStatus:   http.StatusNotFound,
		},
		{
			name:         "adapter error",
			dbInstanceID: "db-1",
			err:          fmt.Errorf("RDS error"),
			wantStatus:   http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockAWSAdapter{RDSInstances: tt.instances, Error: tt.err}
			_, mux := setupAWSTestServer(t, mock)

			url := fmt.Sprintf("/api/v1/aws/test-aws/rds/instances/%s", tt.dbInstanceID)
			req := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleAWSVPCListVPCs(t *testing.T) {
	tests := []struct {
		name       string
		vpcs       []awsadapter.VPC
		err        error
		wantStatus int
		wantCount  int
	}{
		{
			name: "list vpcs",
			vpcs: []awsadapter.VPC{
				{VpcID: "vpc-1", CidrBlock: "10.0.0.0/16", State: "available"},
				{VpcID: "vpc-2", CidrBlock: "10.1.0.0/16", State: "available"},
			},
			wantStatus: http.StatusOK,
			wantCount:  2,
		},
		{
			name:       "adapter error",
			err:        fmt.Errorf("VPC error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockAWSAdapter{VPCs: tt.vpcs, Error: tt.err}
			_, mux := setupAWSTestServer(t, mock)

			req := httptest.NewRequest("GET", "/api/v1/aws/test-aws/vpc/vpcs", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if int(resp["count"].(float64)) != tt.wantCount {
					t.Errorf("count: got %v, want %d", resp["count"], tt.wantCount)
				}
			}
		})
	}
}

func TestHandleAWSVPCGetVPC(t *testing.T) {
	tests := []struct {
		name       string
		vpcID      string
		vpcs       []awsadapter.VPC
		err        error
		wantStatus int
	}{
		{
			name:       "get existing vpc",
			vpcID:      "vpc-1",
			vpcs:       []awsadapter.VPC{{VpcID: "vpc-1", CidrBlock: "10.0.0.0/16"}},
			wantStatus: http.StatusOK,
		},
		{
			name:       "vpc not found",
			vpcID:      "vpc-missing",
			vpcs:       []awsadapter.VPC{{VpcID: "vpc-1", CidrBlock: "10.0.0.0/16"}},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "adapter error",
			vpcID:      "vpc-1",
			err:        fmt.Errorf("VPC error"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockAWSAdapter{VPCs: tt.vpcs, Error: tt.err}
			_, mux := setupAWSTestServer(t, mock)

			url := fmt.Sprintf("/api/v1/aws/test-aws/vpc/vpcs/%s", tt.vpcID)
			req := httptest.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}
