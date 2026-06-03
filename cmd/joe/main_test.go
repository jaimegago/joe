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
// config directory so tests stay off the real filesystem. The no-subcommand
// default branch runs the server; runServer is stubbed to a no-op so a test
// that accidentally reaches it never binds a port or opens a database.
func testDeps(joeDir string) runDeps {
	deps := defaultRunDeps()
	deps.joeDirPath = func() (string, error) {
		return joeDir, nil
	}
	deps.runServer = func(context.Context) int {
		return 0
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

// TestRun_NoSubcommand verifies a bare invocation routes to the server — Joe's
// default behavior now that the server is folded into the joe binary.
func TestRun_NoSubcommand(t *testing.T) {
	deps := defaultRunDeps()
	ran := false
	deps.runServer = func(context.Context) int {
		ran = true
		return 0
	}
	var stdout, stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), nil, &stdout, &stderr, deps)
	if !ran {
		t.Fatalf("expected bare invocation to run the server")
	}
	if exitCode != 0 {
		t.Fatalf("expected server exit code 0, got %d", exitCode)
	}
}

// TestRun_ServerFlags verifies a leading server flag (no subcommand) routes to
// the server rather than being treated as an unknown command.
func TestRun_ServerFlags(t *testing.T) {
	deps := defaultRunDeps()
	ran := false
	deps.runServer = func(context.Context) int {
		ran = true
		return 0
	}
	var stdout, stderr bytes.Buffer
	runWithDeps(context.Background(), []string{"--config", "/tmp/x.yaml"}, &stdout, &stderr, deps)
	if !ran {
		t.Fatalf("expected leading --config flag to route to the server")
	}
}

// TestRun_UnknownSubcommand verifies an unrecognized (non-flag) subcommand
// prints usage and exits non-zero without touching the server.
func TestRun_UnknownSubcommand(t *testing.T) {
	deps := defaultRunDeps()
	deps.runServer = func(context.Context) int {
		t.Fatalf("server must not run for an unknown subcommand")
		return 0
	}
	var stdout, stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"bogus"}, &stdout, &stderr, deps)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "Usage: joe") {
		t.Errorf("expected usage on stderr, got %q", stderr.String())
	}
}

// TestRun_DirectCall covers the run() wrapper so its body is counted as
// executed. An unknown subcommand exits non-zero through the real wrapper
// without booting the server.
func TestRun_DirectCall(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := run(context.Background(), []string{"bogus"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
}
