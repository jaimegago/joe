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

func TestExecute_AllowedDirectories_Inside(t *testing.T) {
	allowedDir := t.TempDir()
	tool := New(allowedDir)

	path := filepath.Join(allowedDir, "allowed.txt")
	_, err := tool.Execute(context.Background(), map[string]any{
		"path":    path,
		"content": "allowed",
	})
	if err != nil {
		t.Fatalf("Execute() inside allowed dir: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "allowed" {
		t.Errorf("content = %q, want %q", string(data), "allowed")
	}
}

func TestExecute_AllowedDirectories_Outside(t *testing.T) {
	allowedDir := t.TempDir()
	outsideDir := t.TempDir()
	tool := New(allowedDir)

	path := filepath.Join(outsideDir, "blocked.txt")
	_, err := tool.Execute(context.Background(), map[string]any{
		"path":    path,
		"content": "should not write",
	})
	if err == nil {
		t.Fatal("expected error when writing outside allowed directories")
	}
	if !strings.Contains(err.Error(), "path_sandbox") {
		t.Errorf("expected path_sandbox error, got: %v", err)
	}

	// Verify file was NOT created
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("file should not have been created outside allowed directory")
	}
}

func TestExecute_AllowedDirectories_Empty_AllowsAll(t *testing.T) {
	tmpDir := t.TempDir()
	tool := New() // no allowed directories — no sandbox

	path := filepath.Join(tmpDir, "anywhere.txt")
	_, err := tool.Execute(context.Background(), map[string]any{
		"path":    path,
		"content": "unrestricted",
	})
	if err != nil {
		t.Fatalf("Execute() without sandbox: %v", err)
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
