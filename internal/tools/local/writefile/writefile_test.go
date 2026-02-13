package writefile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExecute_CreateAndOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nested", "file.txt")

	tool := New()
	resultRaw, err := tool.Execute(context.Background(), map[string]any{
		"path":    path,
		"content": "first",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	result := resultRaw.(map[string]any)
	if result["created"] != true {
		t.Fatalf("created = %v, want true", result["created"])
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "first" {
		t.Fatalf("content = %q, want %q", string(data), "first")
	}

	resultRaw, err = tool.Execute(context.Background(), map[string]any{
		"path":    path,
		"content": "second",
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	result = resultRaw.(map[string]any)
	if result["created"] != false {
		t.Fatalf("created = %v, want false", result["created"])
	}

	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "second" {
		t.Fatalf("content = %q, want %q", string(data), "second")
	}
}

func TestExecute_InvalidContent(t *testing.T) {
	tool := New()
	_, err := tool.Execute(context.Background(), map[string]any{
		"path":    "./file.txt",
		"content": 123,
	})
	if err == nil {
		t.Fatal("expected error for invalid content")
	}
}
