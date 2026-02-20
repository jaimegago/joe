package readfile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTool_Metadata(t *testing.T) {
	tool := New()
	if tool.Name() != "read_file" {
		t.Errorf("Name() = %q, want read_file", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["path"]; !ok {
		t.Error("Parameters() missing path property")
	}
}

func TestExecute(t *testing.T) {
	tmpDir := t.TempDir()
	home, _ := os.UserHomeDir()

	textFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(textFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	bigFile := filepath.Join(tmpDir, "big.txt")
	if err := os.WriteFile(bigFile, []byte(strings.Repeat("a", 1024*1024+1)), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	binFile := filepath.Join(tmpDir, "bin.dat")
	if err := os.WriteFile(binFile, []byte{0, 1, 2}, 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tests := []struct {
		name        string
		args        map[string]any
		wantErr     bool
		errContains string
		wantContent string
	}{
		{
			name:        "reads text file",
			args:        map[string]any{"path": textFile},
			wantContent: "hello",
		},
		{
			name:        "missing path parameter",
			args:        map[string]any{},
			wantErr:     true,
			errContains: "path parameter",
		},
		{
			name:    "empty path",
			args:    map[string]any{"path": ""},
			wantErr: true,
		},
		{
			name:        "file not found",
			args:        map[string]any{"path": filepath.Join(tmpDir, "missing.txt")},
			wantErr:     true,
			errContains: "file not found",
		},
		{
			name:        "directory path rejected",
			args:        map[string]any{"path": tmpDir},
			wantErr:     true,
			errContains: "directory",
		},
		{
			name:        "file too large",
			args:        map[string]any{"path": bigFile},
			wantErr:     true,
			errContains: "file too large",
		},
		{
			name:        "binary file rejected",
			args:        map[string]any{"path": binFile},
			wantErr:     true,
			errContains: "binary",
		},
		{
			name:        "blocks ~/.joe/config.yaml",
			args:        map[string]any{"path": filepath.Join(home, ".joe", "config.yaml")},
			wantErr:     true,
			errContains: "self-protection",
		},
		{
			name:        "blocks ~/.joe/safety-policy.yaml",
			args:        map[string]any{"path": filepath.Join(home, ".joe", "safety-policy.yaml")},
			wantErr:     true,
			errContains: "self-protection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := New()
			got, err := tool.Execute(context.Background(), tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errContains != "" && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Errorf("Execute() error = %v, want error containing %q", err, tt.errContains)
			}
			if tt.wantContent != "" {
				result := got.(map[string]any)
				if result["content"] != tt.wantContent {
					t.Errorf("content = %q, want %q", result["content"], tt.wantContent)
				}
			}
		})
	}
}
