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

func TestExecute_MissingPath(t *testing.T) {
	tool := New()
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing path parameter")
	}
	if !strings.Contains(err.Error(), "path parameter") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExecute_EmptyPath(t *testing.T) {
	tool := New()
	_, err := tool.Execute(context.Background(), map[string]any{"path": ""})
	if err == nil {
		t.Fatal("expected error for empty path parameter")
	}
}

func TestExecute_ReadsFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := New()
	resultRaw, err := tool.Execute(context.Background(), map[string]any{"path": path})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	result := resultRaw.(map[string]any)
	if result["content"] != "hello" {
		t.Fatalf("content = %q, want %q", result["content"], "hello")
	}
}

func TestExecute_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "missing.txt")

	tool := New()
	_, err := tool.Execute(context.Background(), map[string]any{"path": path})
	if err == nil || !strings.Contains(err.Error(), "file not found") {
		t.Fatalf("expected file not found error, got: %v", err)
	}
}

func TestExecute_DirectoryPath(t *testing.T) {
	tmpDir := t.TempDir()

	tool := New()
	_, err := tool.Execute(context.Background(), map[string]any{"path": tmpDir})
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("expected directory error, got: %v", err)
	}
}

func TestExecute_TooLarge(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "big.txt")
	big := strings.Repeat("a", 1024*1024+1)
	if err := os.WriteFile(path, []byte(big), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := New()
	_, err := tool.Execute(context.Background(), map[string]any{"path": path})
	if err == nil || !strings.Contains(err.Error(), "file too large") {
		t.Fatalf("expected too large error, got: %v", err)
	}
}

func TestExecute_BinaryFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "bin.dat")
	if err := os.WriteFile(path, []byte{0, 1, 2}, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool := New()
	_, err := tool.Execute(context.Background(), map[string]any{"path": path})
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("expected binary error, got: %v", err)
	}
}

func TestExecute_BlocksJoeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	// Try to read ~/.joe/config.yaml
	configPath := filepath.Join(home, ".joe", "config.yaml")

	tool := New()
	_, err = tool.Execute(context.Background(), map[string]any{"path": configPath})
	if err == nil {
		t.Fatal("expected error when reading from ~/.joe/, got nil")
	}

	if !strings.Contains(err.Error(), "self-protection") {
		t.Errorf("expected self-protection error, got: %v", err)
	}
}

func TestExecute_BlocksSafetyPolicy(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	// Try to read ~/.joe/safety-policy.yaml
	policyPath := filepath.Join(home, ".joe", "safety-policy.yaml")

	tool := New()
	_, err = tool.Execute(context.Background(), map[string]any{"path": policyPath})
	if err == nil {
		t.Fatal("expected error when reading safety policy, got nil")
	}

	if !strings.Contains(err.Error(), "self-protection") {
		t.Errorf("expected self-protection error, got: %v", err)
	}
}
