package terraform

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// mockReader simulates filesystem operations.
type mockReader struct {
	files map[string]string
	paths map[string]string // stat path -> resolved path
	err   error
}

func (m *mockReader) readFile(path string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	if content, ok := m.files[path]; ok {
		return []byte(content), nil
	}
	return nil, errors.New("file not found: " + path)
}

func (m *mockReader) resolvePath(path string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if resolved, ok := m.paths[path]; ok {
		return resolved, nil
	}
	return path, nil
}

func stateWithResources(resources []map[string]any) string {
	state := map[string]any{
		"format_version":    "1.0",
		"terraform_version": "1.5.0",
		"resources":         resources,
		"outputs":           map[string]any{},
	}
	b, _ := json.Marshal(state)
	return string(b)
}

func managedResource(rtype, name string, attrs map[string]any, sensitive []string) map[string]any {
	return map[string]any{
		"mode":     "managed",
		"type":     rtype,
		"name":     name,
		"provider": `provider["registry.io/hashicorp/aws"]`,
		"instances": []map[string]any{
			{
				"attributes":           attrs,
				"sensitive_attributes": sensitive,
			},
		},
	}
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantErr  bool
		wantPath string
	}{
		{
			name:     "valid config",
			raw:      `{"state_path":"/path/to/tfstate"}`,
			wantPath: "/path/to/tfstate",
		},
		{
			name:    "missing state_path",
			raw:     `{}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			raw:     `{bad}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg.StatePath != tt.wantPath {
				t.Errorf("StatePath = %q, want %q", cfg.StatePath, tt.wantPath)
			}
		})
	}
}

func TestAdapter_Status_NotConnected(t *testing.T) {
	a := New()
	s := a.Status()
	if s.Connected {
		t.Error("expected not connected")
	}
}

func TestAdapter_Resources(t *testing.T) {
	ec2 := managedResource("aws_instance", "web",
		map[string]any{"id": "i-123", "ami": "ami-abc", "password": "secret"},
		[]string{"password"},
	)
	rds := managedResource("aws_db_instance", "main",
		map[string]any{"id": "db-1", "engine": "postgres"},
		nil,
	)
	dataRes := map[string]any{
		"mode": "data",
		"type": "aws_ami",
		"name": "ubuntu",
		"instances": []map[string]any{
			{"attributes": map[string]any{"id": "ami-xyz"}},
		},
	}
	stateJSON := stateWithResources([]map[string]any{ec2, rds, dataRes})

	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{
		files: map[string]string{"/state/tfstate": stateJSON},
	}
	a := NewWithReader(reader, cfg, "/state/tfstate")

	tests := []struct {
		name         string
		resourceType string
		wantCount    int
		wantErr      bool
	}{
		{name: "all managed", resourceType: "", wantCount: 2},
		{name: "filter by type", resourceType: "aws_instance", wantCount: 1},
		{name: "no match", resourceType: "aws_elb", wantCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resources, err := a.Resources(context.Background(), tt.resourceType)
			if (err != nil) != tt.wantErr {
				t.Errorf("Resources() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(resources) != tt.wantCount {
				t.Errorf("Resources() count = %d, want %d", len(resources), tt.wantCount)
			}
		})
	}
}

func TestAdapter_Resources_SensitiveRedaction(t *testing.T) {
	ec2 := managedResource("aws_instance", "web",
		map[string]any{"id": "i-123", "password": "supersecret"},
		[]string{"password"},
	)
	stateJSON := stateWithResources([]map[string]any{ec2})
	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{files: map[string]string{"/state/tfstate": stateJSON}}
	a := NewWithReader(reader, cfg, "/state/tfstate")

	resources, err := a.Resources(context.Background(), "")
	if err != nil {
		t.Fatalf("Resources() error = %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	attrs := resources[0].Instances[0].Attributes
	if attrs["password"] != "[redacted]" {
		t.Errorf("expected password to be redacted, got %v", attrs["password"])
	}
	if attrs["id"] != "i-123" {
		t.Errorf("expected id to be preserved, got %v", attrs["id"])
	}
}

func TestAdapter_GetResource(t *testing.T) {
	ec2 := managedResource("aws_instance", "web", map[string]any{"id": "i-123"}, nil)
	stateJSON := stateWithResources([]map[string]any{ec2})
	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{files: map[string]string{"/state/tfstate": stateJSON}}
	a := NewWithReader(reader, cfg, "/state/tfstate")

	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{name: "found", address: "aws_instance.web"},
		{name: "not found", address: "aws_instance.missing", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := a.GetResource(context.Background(), tt.address)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetResource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && r == nil {
				t.Error("expected non-nil resource")
			}
		})
	}
}

func TestAdapter_Outputs(t *testing.T) {
	state := map[string]any{
		"format_version":    "1.0",
		"terraform_version": "1.5.0",
		"resources":         []map[string]any{},
		"outputs": map[string]any{
			"public_ip":   map[string]any{"value": "1.2.3.4", "type": "string", "sensitive": false},
			"db_password": map[string]any{"value": "secret", "type": "string", "sensitive": true},
		},
	}
	stateJSON, _ := json.Marshal(state)
	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{files: map[string]string{"/state/tfstate": string(stateJSON)}}
	a := NewWithReader(reader, cfg, "/state/tfstate")

	outputs, err := a.Outputs(context.Background())
	if err != nil {
		t.Fatalf("Outputs() error = %v", err)
	}

	if outputs["public_ip"].Value != "1.2.3.4" {
		t.Errorf("public_ip = %v, want 1.2.3.4", outputs["public_ip"].Value)
	}
	if outputs["db_password"].Value != "[redacted]" {
		t.Errorf("db_password should be redacted, got %v", outputs["db_password"].Value)
	}
}

func TestAdapter_NotConnected(t *testing.T) {
	a := New()
	ctx := context.Background()

	if _, err := a.Resources(ctx, ""); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Resources(): expected ErrNotConnected, got %v", err)
	}
	if _, err := a.GetResource(ctx, "aws_instance.web"); !errors.Is(err, ErrNotConnected) {
		t.Errorf("GetResource(): expected ErrNotConnected, got %v", err)
	}
	if _, err := a.Outputs(ctx); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Outputs(): expected ErrNotConnected, got %v", err)
	}
}

func TestAdapter_ReadError(t *testing.T) {
	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{err: errors.New("permission denied")}
	a := NewWithReader(reader, cfg, "/state/tfstate")

	_, err := a.Resources(context.Background(), "")
	if err == nil {
		t.Error("expected error for unreadable state file")
	}
}

func TestAdapter_Disconnect(t *testing.T) {
	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{files: map[string]string{"/state/tfstate": stateWithResources(nil)}}
	a := NewWithReader(reader, cfg, "/state/tfstate")

	if err := a.Disconnect(); err != nil {
		t.Errorf("Disconnect() error = %v", err)
	}
	if a.Status().Connected {
		t.Error("expected not connected after Disconnect")
	}
}
