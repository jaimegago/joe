package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/safety"
)

func panicServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/status":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"ok","version":"test","time":"now"}`)
		case "/api/v1/panic":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"acknowledged":true,"message":"emergency shutdown initiated"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRunPanicCommand_Success(t *testing.T) {
	server := panicServer(t)
	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "info")

	deps := testDeps(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"panic", "-config", cfgPath, "-reason", "test emergency"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Emergency shutdown triggered") {
		t.Errorf("expected shutdown message, got %q", stdout.String())
	}
}

func TestRunPanicCommand_BadConfig(t *testing.T) {
	deps := testDeps(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"panic", "-config", "/nonexistent/config.yaml"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

// TestRunUnlockCommand_Success confirms unlock is a LOCAL-FILE-ONLY op: it clears
// the persisted panic state and reports restart-required, WITHOUT contacting any
// server (the deps.newClient stub would never be reached).
func TestRunUnlockCommand_Success(t *testing.T) {
	joeDir := t.TempDir()
	if err := safety.WritePanicState(joeDir, safety.PanicState{TriggerSource: safety.PanicSourceCLI}); err != nil {
		t.Fatalf("seed panic state: %v", err)
	}

	deps := testDeps(joeDir)

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"unlock", "-reason", "incident resolved"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "restart joe to resume writes") {
		t.Errorf("expected restart-required message, got %q", stdout.String())
	}
	// The panic state file must be gone.
	state, err := safety.ReadPanicState(joeDir)
	if err != nil {
		t.Fatalf("read panic state: %v", err)
	}
	if state != nil {
		t.Error("expected panic state cleared after unlock")
	}
}

func TestRunUnlockCommand_MissingReason(t *testing.T) {
	deps := testDeps(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"unlock"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing reason, got %d", code)
	}
	if !strings.Contains(stderr.String(), "--reason") {
		t.Errorf("expected --reason in error message, got %q", stderr.String())
	}
}

// TestRunUnlockCommand_NoPanicState confirms clearing is idempotent — clearing a
// non-existent panic state still succeeds (recovery is safe to re-run).
func TestRunUnlockCommand_NoPanicState(t *testing.T) {
	deps := testDeps(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"unlock", "-reason", "no-op"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0 clearing absent state, got %d (stderr: %s)", code, stderr.String())
	}
}
