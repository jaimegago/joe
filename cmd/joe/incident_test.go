package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/config"
)

// regimeServer is a mock joe HTTP API for the `joe incident` tests. The
// regime mode it reports is controlled by `incident`; declare/resolve echo a
// principal back the way the real handlers do and flip the in-memory mode.
func regimeServer(t *testing.T, incident bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/regime":
			if incident {
				// Mirrors the no-json-tag marshal of sessionmodel.Regime.
				fmt.Fprint(w, `{"Mode":"incident","DeclaredAt":"2026-06-04T12:00:00Z","DeclaredByPrincipal":"user:alice","DeclaredKind":"human"}`)
			} else {
				fmt.Fprint(w, `{"Mode":"normal","DeclaredAt":null,"DeclaredByPrincipal":null,"DeclaredKind":null}`)
			}
		case r.Method == "POST" && r.URL.Path == "/api/v1/regime/declare":
			incident = true
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"session_id":"sess-1","captain_id":"cap-1","declared_by":"user:alice"}`)
		case r.Method == "POST" && r.URL.Path == "/api/v1/regime/resolve":
			incident = false
			fmt.Fprint(w, `{"session_id":"sess-1","resolved_by":"user:alice"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// incidentDeps wires runDeps so the incident command (which reads
// paths.DefaultConfigPath()) loads a test config pointing at the mock server.
func incidentDeps(t *testing.T, server *httptest.Server) runDeps {
	t.Helper()
	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "info")
	deps := testDeps(t.TempDir())
	deps.loadConfig = func(string) (*config.Config, error) { return config.Load(cfgPath) }
	return deps
}

func TestRunIncidentStatus_Normal(t *testing.T) {
	server := regimeServer(t, false)
	deps := incidentDeps(t, server)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"incident", "status"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "System operating normally") {
		t.Errorf("expected normal status, got %q", stdout.String())
	}
}

func TestRunIncidentStatus_Active(t *testing.T) {
	server := regimeServer(t, true)
	deps := incidentDeps(t, server)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"incident", "status"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "INCIDENT MODE ACTIVE") {
		t.Errorf("expected active banner, got %q", out)
	}
	if !strings.Contains(out, "user:alice") || !strings.Contains(out, "kind: human") {
		t.Errorf("expected declarer + kind in output, got %q", out)
	}
}

func TestRunIncidentDeclare(t *testing.T) {
	server := regimeServer(t, false)
	deps := incidentDeps(t, server)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"incident", "declare", "--session", "sess-1", "--reason", "db outage"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Incident declared by user:alice") {
		t.Errorf("expected declare confirmation with caller, got %q", out)
	}
	if !strings.Contains(out, "Reason: db outage") {
		t.Errorf("expected reason echoed, got %q", out)
	}
}

func TestRunIncidentResolve(t *testing.T) {
	server := regimeServer(t, true)
	deps := incidentDeps(t, server)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"incident", "resolve"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Incident resolved by user:alice") {
		t.Errorf("expected resolve confirmation, got %q", stdout.String())
	}
}

func TestRunIncidentList_UnsupportedNonZero(t *testing.T) {
	// list needs no server — it reports the v1 limitation and exits non-zero.
	deps := testDeps(t.TempDir())
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"incident", "list"}, &stdout, &stderr, deps)
	if code == 0 {
		t.Fatalf("expected non-zero exit for the unsupported list subcommand, got 0")
	}
	if !strings.Contains(stdout.String(), "audit log") {
		t.Errorf("expected audit-log guidance, got %q", stdout.String())
	}
}

func TestRunIncidentNoSubcommand(t *testing.T) {
	deps := testDeps(t.TempDir())
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"incident"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage: joe incident") {
		t.Errorf("expected usage text, got %q", stderr.String())
	}
}

func TestRunIncidentUnknownSubcommand(t *testing.T) {
	server := regimeServer(t, false)
	deps := incidentDeps(t, server)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"incident", "bogus"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Unknown incident subcommand") {
		t.Errorf("expected unknown-subcommand message, got %q", stderr.String())
	}
}
