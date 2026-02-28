package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		case "/api/v1/unlock":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"ok","message":"safe mode lifted"}`)
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

	deps := testDeps(&fakeRepl{}, t.TempDir())

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
	deps := testDeps(&fakeRepl{}, t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"panic", "-config", "/nonexistent/config.yaml"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}

func TestRunUnlockCommand_Success(t *testing.T) {
	server := panicServer(t)
	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "info")

	deps := testDeps(&fakeRepl{}, t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"unlock", "-config", cfgPath, "-reason", "incident resolved"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Safe mode lifted") {
		t.Errorf("expected unlock message, got %q", stdout.String())
	}
}

func TestRunUnlockCommand_MissingReason(t *testing.T) {
	server := panicServer(t)
	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "info")

	deps := testDeps(&fakeRepl{}, t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"unlock", "-config", cfgPath}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("expected exit code 1 for missing reason, got %d", code)
	}
	if !strings.Contains(stderr.String(), "--reason") {
		t.Errorf("expected --reason in error message, got %q", stderr.String())
	}
}

func TestRunUnlockCommand_BadConfig(t *testing.T) {
	deps := testDeps(&fakeRepl{}, t.TempDir())

	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"unlock", "-config", "/nonexistent/config.yaml", "-reason", "test"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
}
