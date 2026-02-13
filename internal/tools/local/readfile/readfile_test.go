package readfile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
