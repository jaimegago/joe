package aws_test

import (
	"context"
	"testing"

	awsadapter "github.com/jaimegago/joe/internal/adapters/aws"
	"github.com/jaimegago/joe/internal/store"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name        string
		input       map[string]any
		expected    awsadapter.Config
		expectError bool
	}{
		{
			name: "valid minimal config",
			input: map[string]any{
				"region": "us-west-2",
			},
			expected: awsadapter.Config{
				Region: "us-west-2",
			},
			expectError: false,
		},
		{
			name: "valid full config",
			input: map[string]any{
				"region":     "us-east-1",
				"profile":    "production",
				"access_key": "AKIAIOSFODNN7EXAMPLE",
				"secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				"role_arn":   "arn:aws:iam::123456789012:role/MyRole",
			},
			expected: awsadapter.Config{
				Region:    "us-east-1",
				Profile:   "production",
				AccessKey: "AKIAIOSFODNN7EXAMPLE",
				SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				RoleARN:   "arn:aws:iam::123456789012:role/MyRole",
			},
			expectError: false,
		},
		{
			name: "missing region",
			input: map[string]any{
				"profile": "default",
			},
			expected:    awsadapter.Config{},
			expectError: true,
		},
		{
			name:        "empty config",
			input:       map[string]any{},
			expected:    awsadapter.Config{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := awsadapter.ParseConfig(tt.input)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if config.Region != tt.expected.Region {
				t.Errorf("region: got %q, want %q", config.Region, tt.expected.Region)
			}
			if config.Profile != tt.expected.Profile {
				t.Errorf("profile: got %q, want %q", config.Profile, tt.expected.Profile)
			}
			if config.AccessKey != tt.expected.AccessKey {
				t.Errorf("access_key: got %q, want %q", config.AccessKey, tt.expected.AccessKey)
			}
			if config.SecretKey != tt.expected.SecretKey {
				t.Errorf("secret_key: got %q, want %q", config.SecretKey, tt.expected.SecretKey)
			}
			if config.RoleARN != tt.expected.RoleARN {
				t.Errorf("role_arn: got %q, want %q", config.RoleARN, tt.expected.RoleARN)
			}
		})
	}
}

func TestAdapter_Status(t *testing.T) {
	// Test disconnected status
	adapter := awsadapter.New()
	status := adapter.Status()

	if status.Connected {
		t.Errorf("expected disconnected, got connected")
	}
	if status.Message != "Not connected to AWS" {
		t.Errorf("message: got %q, want %q", status.Message, "Not connected to AWS")
	}
}

func TestAdapter_ConnectValidatesConfig(t *testing.T) {
	adapter := awsadapter.New()

	// Test with invalid source config (missing region)
	source := store.Source{
		ID:     "test-aws",
		Name:   "Test AWS",
		Type:   "aws",
		Config: []byte(`{"profile": "default"}`),
	}
	err := adapter.Connect(context.Background(), source)
	if err == nil {
		t.Error("expected error for missing region")
	}

	// Test with valid config but no AWS credentials (will fail at STS call)
	source.Config = []byte(`{"region": "us-west-2"}`)
	err = adapter.Connect(context.Background(), source)
	// This will fail due to missing credentials, but that's expected in test environment
	if err == nil {
		t.Log("Connection succeeded (AWS credentials available in test environment)")
	} else {
		t.Logf("Connection failed as expected: %v", err)
	}
}

func TestAdapter_DisconnectOperations(t *testing.T) {
	adapter := awsadapter.New()
	ctx := context.Background()

	// Test operations on disconnected adapter
	_, err := adapter.ListEC2Instances(ctx)
	if err == nil {
		t.Error("expected error for ListEC2Instances on disconnected adapter")
	}

	_, err = adapter.GetEC2Instance(ctx, "i-1234567890abcdef0")
	if err == nil {
		t.Error("expected error for GetEC2Instance on disconnected adapter")
	}

	_, err = adapter.ListEKSClusters(ctx)
	if err == nil {
		t.Error("expected error for ListEKSClusters on disconnected adapter")
	}

	_, err = adapter.GetEKSCluster(ctx, "test-cluster")
	if err == nil {
		t.Error("expected error for GetEKSCluster on disconnected adapter")
	}

	_, err = adapter.ListRDSInstances(ctx)
	if err == nil {
		t.Error("expected error for ListRDSInstances on disconnected adapter")
	}

	_, err = adapter.GetRDSInstance(ctx, "test-db")
	if err == nil {
		t.Error("expected error for GetRDSInstance on disconnected adapter")
	}

	_, err = adapter.ListVPCs(ctx)
	if err == nil {
		t.Error("expected error for ListVPCs on disconnected adapter")
	}

	_, err = adapter.GetVPC(ctx, "vpc-1234567890abcdef0")
	if err == nil {
		t.Error("expected error for GetVPC on disconnected adapter")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	// Test disconnect on already disconnected adapter
	adapter := awsadapter.New()
	err := adapter.Disconnect()
	if err != nil {
		t.Errorf("unexpected error on disconnect: %v", err)
	}

	// Verify status after disconnect
	status := adapter.Status()
	if status.Connected {
		t.Error("expected disconnected status after disconnect")
	}
}
