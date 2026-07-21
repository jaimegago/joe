package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jaimegago/joe/internal/client"
)

// envMap builds a getenv stub from a literal map, so a test states exactly the
// environment it assumes rather than mutating the real one.
func envMap(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

// TestRunSlackCommand_MissingBotToken pins the first refusal: without
// SLACK_BOT_TOKEN the command exits 1 and names the variable, before
// constructing a client or dialing Slack.
func TestRunSlackCommand_MissingBotToken(t *testing.T) {
	deps := testDeps(t.TempDir())
	deps.getenv = envMap(map[string]string{"SLACK_APP_TOKEN": "xapp-test"})
	deps.newClient = func(string, ...client.ClientOption) *client.Client {
		t.Fatal("client must not be constructed when SLACK_BOT_TOKEN is missing")
		return nil
	}

	var stderr bytes.Buffer
	code := runSlackCommand(context.Background(), nil, &stderr, deps)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "SLACK_BOT_TOKEN is required") {
		t.Errorf("stderr must name the missing variable, got %q", stderr.String())
	}
}

// TestRunSlackCommand_MissingAppToken pins the second refusal, which is
// distinct from the first: a bot token alone is not enough, because Socket Mode
// needs the app-level token to open the websocket.
func TestRunSlackCommand_MissingAppToken(t *testing.T) {
	deps := testDeps(t.TempDir())
	deps.getenv = envMap(map[string]string{"SLACK_BOT_TOKEN": "xoxb-test"})
	deps.newClient = func(string, ...client.ClientOption) *client.Client {
		t.Fatal("client must not be constructed when SLACK_APP_TOKEN is missing")
		return nil
	}

	var stderr bytes.Buffer
	code := runSlackCommand(context.Background(), nil, &stderr, deps)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "SLACK_APP_TOKEN is required") {
		t.Errorf("stderr must name the missing variable, got %q", stderr.String())
	}
}

// TestRunMCPCommand_DefaultsServerURL pins the documented default: with
// JOE_SERVER unset the command targets localhost:7777 rather than an empty
// base URL, which would fail later and less legibly.
func TestRunMCPCommand_DefaultsServerURL(t *testing.T) {
	deps := testDeps(t.TempDir())
	deps.getenv = envMap(nil)
	var gotURL string
	deps.newClient = func(baseURL string, opts ...client.ClientOption) *client.Client {
		gotURL = baseURL
		return client.New(baseURL, opts...)
	}
	deps.serveMCP = func(*mcpserver.MCPServer) error { return nil }

	var stderr bytes.Buffer
	code := runMCPCommand(context.Background(), nil, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if gotURL != "http://localhost:7777" {
		t.Errorf("base URL = %q, want the documented localhost default", gotURL)
	}
}

// TestRunMCPCommand_HonorsServerEnv confirms JOE_SERVER overrides the default,
// so the documented env var actually reaches the client.
func TestRunMCPCommand_HonorsServerEnv(t *testing.T) {
	deps := testDeps(t.TempDir())
	deps.getenv = envMap(map[string]string{"JOE_SERVER": "https://joe.internal:8443"})
	var gotURL string
	deps.newClient = func(baseURL string, opts ...client.ClientOption) *client.Client {
		gotURL = baseURL
		return client.New(baseURL, opts...)
	}
	deps.serveMCP = func(*mcpserver.MCPServer) error { return nil }

	var stderr bytes.Buffer
	if code := runMCPCommand(context.Background(), nil, &stderr, deps); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if gotURL != "https://joe.internal:8443" {
		t.Errorf("base URL = %q, want the JOE_SERVER value", gotURL)
	}
}

// TestRunMCPCommand_ServeErrorExitsNonZero pins the failure path: a serve error
// must surface as exit 1 with the cause on stderr, not a silent exit 0 that
// would read to a supervisor as a clean shutdown.
func TestRunMCPCommand_ServeErrorExitsNonZero(t *testing.T) {
	deps := testDeps(t.TempDir())
	deps.getenv = envMap(nil)
	deps.serveMCP = func(*mcpserver.MCPServer) error {
		return errors.New("stdio closed unexpectedly")
	}

	var stderr bytes.Buffer
	code := runMCPCommand(context.Background(), nil, &stderr, deps)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "stdio closed unexpectedly") {
		t.Errorf("stderr must carry the underlying cause, got %q", stderr.String())
	}
}
