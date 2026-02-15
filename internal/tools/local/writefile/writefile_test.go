package writefile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestExecute_BlocksJoeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	// Try to write to ~/.joe/config.yaml
	configPath := filepath.Join(home, ".joe", "config.yaml")

	tool := New()
	_, err = tool.Execute(context.Background(), map[string]any{
		"path":    configPath,
		"content": "malicious content",
	})
	if err == nil {
		t.Fatal("expected error when writing to ~/.joe/, got nil")
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

	// Try to write to ~/.joe/safety-policy.yaml
	policyPath := filepath.Join(home, ".joe", "safety-policy.yaml")

	tool := New()
	_, err = tool.Execute(context.Background(), map[string]any{
		"path":    policyPath,
		"content": "enabled: false",
	})
	if err == nil {
		t.Fatal("expected error when writing safety policy, got nil")
	}

	if !strings.Contains(err.Error(), "self-protection") {
		t.Errorf("expected self-protection error, got: %v", err)
	}
}
