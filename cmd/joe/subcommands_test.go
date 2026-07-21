package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/jaimegago/joe/internal/config"
	"strings"
	"testing"
)

// fakePanicRowStore is an in-memory panicRowStore for unlock tests. It records
// whether ClearPanicked was called so a test can assert the conditional behavior.
type fakePanicRowStore struct {
	panicked bool
	cleared  bool
	readErr  error
}

func (f *fakePanicRowStore) IsPanicked(context.Context) (bool, error) {
	return f.panicked, f.readErr
}

func (f *fakePanicRowStore) ClearPanicked(context.Context) error {
	f.cleared = true
	f.panicked = false
	return nil
}

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

// TestRunUnlockCommand_PanicPresent confirms unlock acts on the panic DB row,
// clears it when present, reports cleared, exits 0, and NEVER contacts a process
// (the deps.newClient stub is never reached). It opens the row store directly.
func TestRunUnlockCommand_PanicPresent(t *testing.T) {
	fake := &fakePanicRowStore{panicked: true}
	deps := testDeps(t.TempDir())
	deps.openPanicStore = func(*config.Config) (panicRowStore, func() error, error) {
		return fake, func() error { return nil }, nil
	}

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"unlock", "-reason", "incident resolved"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}
	if !fake.cleared {
		t.Error("expected ClearPanicked to be called when a panic row is present")
	}
	if !strings.Contains(stdout.String(), "has been cleared") {
		t.Errorf("expected cleared message, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "restart") {
		t.Errorf("expected restart hint, got %q", stdout.String())
	}
}

// TestRunUnlockCommand_NoPanic confirms idempotency: with no panic row, unlock
// clears nothing, reports nothing-to-clear, and exits 0 — safe to re-run or run
// on a healthy Joe.
func TestRunUnlockCommand_NoPanic(t *testing.T) {
	fake := &fakePanicRowStore{panicked: false}
	deps := testDeps(t.TempDir())
	deps.openPanicStore = func(*config.Config) (panicRowStore, func() error, error) {
		return fake, func() error { return nil }, nil
	}

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"unlock"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0 with no panic row, got %d (stderr: %s)", code, stderr.String())
	}
	if fake.cleared {
		t.Error("expected ClearPanicked NOT to be called when no panic row is present")
	}
	if !strings.Contains(stdout.String(), "not in a panicked state") {
		t.Errorf("expected nothing-to-clear message, got %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "resume writes") {
		t.Errorf("no-panic message must not promise writes resume, got %q", stdout.String())
	}
}
