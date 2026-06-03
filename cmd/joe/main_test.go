package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testDeps builds a runDeps for subcommand tests, overriding only the Joe
// config directory so tests stay off the real filesystem. The interactive REPL
// was removed in the deletion pass; the no-subcommand default branch now just
// prints usage and exits non-zero.
func testDeps(joeDir string) runDeps {
	deps := defaultRunDeps()
	deps.joeDirPath = func() (string, error) {
		return joeDir, nil
	}
	return deps
}

func writeConfig(t *testing.T, addr, logLevel string) string {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if logLevel == "" {
		logLevel = "info"
	}
	cfg := fmt.Sprintf("llm:\n  current: test\n  available:\n    test:\n      provider: claude\n      model: test-model\nserver:\n  address: %s\nlogging:\n  level: %s\n  file: \"\"\n", addr, logLevel)
	if err := os.WriteFile(configPath, []byte(cfg), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

// TestRun_NoSubcommand verifies the no-subcommand default branch prints usage
// and exits non-zero now that the interactive REPL has been removed.
func TestRun_NoSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), nil, &stdout, &stderr, defaultRunDeps())
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit for bare invocation, got 0")
	}
	if !strings.Contains(stderr.String(), "Usage: joe") {
		t.Errorf("expected usage on stderr, got %q", stderr.String())
	}
}

// TestRun_UnknownSubcommand verifies an unrecognized subcommand falls through
// to usage and exits non-zero.
func TestRun_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"bogus"}, &stdout, &stderr, defaultRunDeps())
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
}

// TestRun_InvalidFlag verifies a flag-like first argument is treated as an
// unknown command and exits non-zero with usage.
func TestRun_InvalidFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-unknown"}, &stdout, &stderr, defaultRunDeps())
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
}

// TestRun_DirectCall covers the run() wrapper so its body is counted as executed.
func TestRun_DirectCall(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"-unknown-flag-xyz"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
}
