package terraform

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/jaimegago/joe/internal/store"
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

func TestConnect_Success(t *testing.T) {
	stateJSON := stateWithResources(nil)
	reader := &mockReader{
		files: map[string]string{"/state/tfstate": stateJSON},
	}
	a := &Adapter{reader: reader}
	src := store.Source{
		Config: []byte(`{"state_path":"/state/tfstate"}`),
	}
	if err := a.Connect(context.Background(), src); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !a.Status().Connected {
		t.Error("expected connected after Connect()")
	}
}

func TestConnect_BadConfig(t *testing.T) {
	a := &Adapter{reader: &mockReader{}}
	src := store.Source{Config: []byte(`{}`)} // missing state_path
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error for missing state_path")
	}
}

func TestConnect_ResolvePathError(t *testing.T) {
	reader := &mockReader{err: errors.New("path not found")}
	a := &Adapter{reader: reader}
	src := store.Source{Config: []byte(`{"state_path":"/state/tfstate"}`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error for resolve path failure")
	}
}

func TestConnect_InvalidStateFile(t *testing.T) {
	reader := &mockReader{
		files: map[string]string{"/state/tfstate": `{not valid json`},
	}
	a := &Adapter{reader: reader}
	src := store.Source{Config: []byte(`{"state_path":"/state/tfstate"}`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error for invalid state JSON")
	}
}

func TestStatus_Connected(t *testing.T) {
	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{files: map[string]string{"/state/tfstate": stateWithResources(nil)}}
	a := NewWithReader(reader, cfg, "/state/tfstate")
	s := a.Status()
	if !s.Connected {
		t.Error("expected connected status")
	}
	if s.Message == "" {
		t.Error("expected non-empty status message")
	}
}

func TestResourceAddress(t *testing.T) {
	// Test the resourceAddress function via GetResource.
	ec2 := managedResource("aws_instance", "web", map[string]any{"id": "i-123"}, nil)
	stateJSON := stateWithResources([]map[string]any{ec2})
	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{files: map[string]string{"/state/tfstate": stateJSON}}
	a := NewWithReader(reader, cfg, "/state/tfstate")

	// This exercises resourceAddress internally.
	r, err := a.GetResource(context.Background(), "aws_instance.web")
	if err != nil {
		t.Fatalf("GetResource() error = %v", err)
	}
	if r == nil {
		t.Error("expected non-nil resource")
	}
}

func TestResourceAddress_ModuleProvider(t *testing.T) {
	// Exercise the module branch in resourceAddress.
	res := map[string]any{
		"mode":     "managed",
		"type":     "aws_instance",
		"name":     "app",
		"provider": `module.vpc.provider["registry.io/hashicorp/aws"]`,
		"instances": []map[string]any{
			{"attributes": map[string]any{"id": "i-456"}, "sensitive_attributes": []string{}},
		},
	}
	stateJSON := stateWithResources([]map[string]any{res})
	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{files: map[string]string{"/state/tfstate": stateJSON}}
	a := NewWithReader(reader, cfg, "/state/tfstate")

	r, err := a.GetResource(context.Background(), "aws_instance.app")
	if err != nil {
		t.Fatalf("GetResource() error = %v", err)
	}
	if r.Address != "aws_instance.app" {
		t.Errorf("Address = %q, want aws_instance.app", r.Address)
	}
}

// --- Tests for osReader (real filesystem) ---

func TestOsReader_ReadFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.tfstate"
	content := `{"format_version":"1.0"}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	r := osReader{}
	data, err := r.readFile(path)
	if err != nil {
		t.Fatalf("readFile() error = %v", err)
	}
	if string(data) != content {
		t.Errorf("readFile() = %q, want %q", string(data), content)
	}
}

func TestOsReader_ReadFile_NotFound(t *testing.T) {
	r := osReader{}
	_, err := r.readFile("/nonexistent/path/file.tfstate")
	if err == nil {
		t.Error("readFile() expected error for nonexistent file")
	}
}

func TestOsReader_ResolvePath_File(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/terraform.tfstate"
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	r := osReader{}
	resolved, err := r.resolvePath(path)
	if err != nil {
		t.Fatalf("resolvePath() error = %v", err)
	}
	if resolved != path {
		t.Errorf("resolvePath() = %q, want %q", resolved, path)
	}
}

func TestOsReader_ResolvePath_Dir(t *testing.T) {
	dir := t.TempDir()
	tfstate := dir + "/terraform.tfstate"
	if err := os.WriteFile(tfstate, []byte("{}"), 0644); err != nil {
		t.Fatalf("write tfstate: %v", err)
	}

	r := osReader{}
	resolved, err := r.resolvePath(dir)
	if err != nil {
		t.Fatalf("resolvePath(dir) error = %v", err)
	}
	if resolved != tfstate {
		t.Errorf("resolvePath(dir) = %q, want %q", resolved, tfstate)
	}
}

func TestOsReader_ResolvePath_DirWithoutTfstate(t *testing.T) {
	dir := t.TempDir()
	r := osReader{}
	_, err := r.resolvePath(dir)
	if err == nil {
		t.Error("resolvePath() expected error for dir without terraform.tfstate")
	}
}

func TestOsReader_ResolvePath_NonExistent(t *testing.T) {
	r := osReader{}
	_, err := r.resolvePath("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("resolvePath() expected error for nonexistent path")
	}
}

// --- Tests for Connect error paths ---

func TestConnect_ReadFileError(t *testing.T) {
	// resolvePath succeeds but readFile fails.
	reader := &mockReadFileErrReader{
		resolvedPath: "/state/tfstate",
	}
	a := &Adapter{reader: reader}
	src := store.Source{Config: []byte(`{"state_path":"/state/tfstate"}`)}
	if err := a.Connect(context.Background(), src); err == nil {
		t.Error("expected error when readFile fails during Connect")
	}
}

// mockReadFileErrReader resolves paths fine but fails on readFile.
type mockReadFileErrReader struct {
	resolvedPath string
}

func (m *mockReadFileErrReader) readFile(_ string) ([]byte, error) {
	return nil, errors.New("permission denied")
}
func (m *mockReadFileErrReader) resolvePath(_ string) (string, error) {
	return m.resolvedPath, nil
}

// --- Tests for loadState error paths ---

func TestAdapter_GetResource_LoadStateError(t *testing.T) {
	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{err: errors.New("disk read error")}
	a := NewWithReader(reader, cfg, "/state/tfstate")

	_, err := a.GetResource(context.Background(), "aws_instance.web")
	if err == nil {
		t.Error("GetResource() expected error when loadState fails")
	}
}

func TestAdapter_Outputs_LoadStateError(t *testing.T) {
	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{err: errors.New("disk read error")}
	a := NewWithReader(reader, cfg, "/state/tfstate")

	_, err := a.Outputs(context.Background())
	if err == nil {
		t.Error("Outputs() expected error when loadState fails")
	}
}

func TestAdapter_Outputs_NilOutputs(t *testing.T) {
	// State with null outputs (triggers nil branch in Outputs).
	stateJSON := `{"format_version":"1.0","terraform_version":"1.5.0","resources":[],"outputs":null}`
	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{files: map[string]string{"/state/tfstate": stateJSON}}
	a := NewWithReader(reader, cfg, "/state/tfstate")

	out, err := a.Outputs(context.Background())
	if err != nil {
		t.Fatalf("Outputs() error = %v", err)
	}
	if out == nil {
		t.Error("Outputs() should return empty map, not nil")
	}
	if len(out) != 0 {
		t.Errorf("Outputs() count = %d, want 0", len(out))
	}
}

func TestAdapter_LoadState_InvalidJSON(t *testing.T) {
	cfg := Config{StatePath: "/state/tfstate"}
	reader := &mockReader{files: map[string]string{"/state/tfstate": `{not valid json`}}
	a := NewWithReader(reader, cfg, "/state/tfstate")

	_, err := a.Resources(context.Background(), "")
	if err == nil {
		t.Error("Resources() expected error for invalid JSON in loadState")
	}
}
