package writefile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTool_Metadata(t *testing.T) {
	tool := New()
	if tool.Name() != "write_file" {
		t.Errorf("Name() = %q, want write_file", tool.Name())
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
	if _, ok := params.Properties["content"]; !ok {
		t.Error("Parameters() missing content property")
	}
}

func TestExecute(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name        string
		setup       func(t *testing.T) (*Tool, map[string]any)
		wantErr     bool
		errContains string
		validate    func(t *testing.T, result any)
	}{
		{
			name: "missing path parameter",
			setup: func(t *testing.T) (*Tool, map[string]any) {
				return New(), map[string]any{"content": "hello"}
			},
			wantErr:     true,
			errContains: "path parameter",
		},
		{
			name: "empty path",
			setup: func(t *testing.T) (*Tool, map[string]any) {
				return New(), map[string]any{"path": "", "content": "hello"}
			},
			wantErr: true,
		},
		{
			name: "invalid content type",
			setup: func(t *testing.T) (*Tool, map[string]any) {
				return New(), map[string]any{"path": "./file.txt", "content": 123}
			},
			wantErr: true,
		},
		{
			name: "writes inside allowed directory",
			setup: func(t *testing.T) (*Tool, map[string]any) {
				allowedDir := t.TempDir()
				return New(allowedDir), map[string]any{
					"path":    filepath.Join(allowedDir, "allowed.txt"),
					"content": "allowed",
				}
			},
			validate: func(t *testing.T, result any) {
				if result.(map[string]any)["created"] != true {
					t.Error("want created=true")
				}
			},
		},
		{
			name: "blocks write outside allowed directory",
			setup: func(t *testing.T) (*Tool, map[string]any) {
				allowedDir := t.TempDir()
				outsideDir := t.TempDir()
				return New(allowedDir), map[string]any{
					"path":    filepath.Join(outsideDir, "blocked.txt"),
					"content": "should not write",
				}
			},
			wantErr:     true,
			errContains: "path_sandbox",
		},
		{
			name: "no allowed dirs means no sandbox",
			setup: func(t *testing.T) (*Tool, map[string]any) {
				return New(), map[string]any{
					"path":    filepath.Join(t.TempDir(), "anywhere.txt"),
					"content": "unrestricted",
				}
			},
		},
		{
			name: "blocks ~/.joe/config.yaml",
			setup: func(t *testing.T) (*Tool, map[string]any) {
				return New(), map[string]any{
					"path":    filepath.Join(home, ".joe", "config.yaml"),
					"content": "malicious content",
				}
			},
			wantErr:     true,
			errContains: "self-protection",
		},
		{
			name: "blocks ~/.joe/safety-policy.yaml",
			setup: func(t *testing.T) (*Tool, map[string]any) {
				return New(), map[string]any{
					"path":    filepath.Join(home, ".joe", "safety-policy.yaml"),
					"content": "enabled: false",
				}
			},
			wantErr:     true,
			errContains: "self-protection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, args := tt.setup(t)
			got, err := tool.Execute(context.Background(), args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errContains != "" && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Errorf("Execute() error = %v, want error containing %q", err, tt.errContains)
			}
			if tt.validate != nil && err == nil {
				tt.validate(t, got)
			}
		})
	}
}

func TestExecute_InvalidPath(t *testing.T) {
	// A path with a null byte triggers an ExpandPath / stat error.
	tool := New()
	_, err := tool.Execute(context.Background(), map[string]any{
		"path":    string([]byte{0}),
		"content": "data",
	})
	if err == nil {
		t.Fatal("expected error for path with null byte")
	}
}

func TestExecute_ContentIsEmpty(t *testing.T) {
	// Empty string is a valid content value — should write an empty file.
	path := filepath.Join(t.TempDir(), "empty.txt")
	tool := New()
	result, err := tool.Execute(context.Background(), map[string]any{
		"path":    path,
		"content": "",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	m := result.(map[string]any)
	if m["bytes_written"].(int) != 0 {
		t.Errorf("bytes_written = %v, want 0", m["bytes_written"])
	}
}

func TestExecute_CreateAndOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "file.txt")
	tool := New()

	// First write: should create.
	resultRaw, err := tool.Execute(context.Background(), map[string]any{"path": path, "content": "first"})
	if err != nil {
		t.Fatalf("first Execute() error: %v", err)
	}
	if resultRaw.(map[string]any)["created"] != true {
		t.Error("first write: want created=true")
	}
	if data, _ := os.ReadFile(path); string(data) != "first" {
		t.Errorf("content after create = %q, want %q", string(data), "first")
	}

	// Second write: should overwrite.
	resultRaw, err = tool.Execute(context.Background(), map[string]any{"path": path, "content": "second"})
	if err != nil {
		t.Fatalf("second Execute() error: %v", err)
	}
	if resultRaw.(map[string]any)["created"] != false {
		t.Error("second write: want created=false")
	}
	if data, _ := os.ReadFile(path); string(data) != "second" {
		t.Errorf("content after overwrite = %q, want %q", string(data), "second")
	}
}
