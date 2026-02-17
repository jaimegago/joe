package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/env"
	"github.com/jaimegago/joe/internal/llm"
	"github.com/jaimegago/joe/internal/observability"
	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/useragent"
)

type fakeAdapter struct{}

func (f *fakeAdapter) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (f *fakeAdapter) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, fmt.Errorf("not implemented")
}

type fakeRepl struct {
	called bool
	err    error
}

func (f *fakeRepl) Run(ctx context.Context) error {
	f.called = true
	return f.err
}

func testDeps(t *testing.T, repl *fakeRepl, joeDir string) runDeps {
	deps := defaultRunDeps()
	deps.setupOTel = func(ctx context.Context, cfg observability.Config) (func(context.Context) error, error) {
		return func(context.Context) error { return nil }, nil
	}
	deps.newAdapter = func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error) {
		return &fakeAdapter{}, nil
	}
	deps.joeDirPath = func() (string, error) {
		return joeDir, nil
	}
	deps.loadPolicy = func(configDir string) (*safety.SafetyPolicy, error) {
		return safety.DefaultPolicy(), nil
	}
	deps.newRepl = func(agent *useragent.Agent, cfg *config.Config, session *useragent.Session) replRunner {
		return repl
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

func statusServer(t *testing.T, statusCode int) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/status" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(statusCode)
			if statusCode == http.StatusOK {
				fmt.Fprint(w, `{"status":"ok","version":"test","time":"now"}`)
			}
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRun_Success(t *testing.T) {
	server := statusServer(t, http.StatusOK)

	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "info")

	t.Setenv(env.AnthropicAPIKey, "test-key")

	fake := &fakeRepl{}
	deps := testDeps(t, fake, t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", cfgPath}, &stdout, &stderr, deps)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", exitCode, stderr.String())
	}
	if !fake.called {
		t.Fatalf("expected repl to run")
	}
}

func TestRun_InvalidConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("llm: ["), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fake := &fakeRepl{}
	deps := testDeps(t, fake, t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", cfgPath}, &stdout, &stderr, deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if fake.called {
		t.Fatalf("did not expect repl to run")
	}
}

func TestRun_InvalidFlag(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-unknown"}, &stdout, &stderr, defaultRunDeps())
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", exitCode)
	}
}

func TestRun_ConfigLoadError(t *testing.T) {
	fake := &fakeRepl{}
	deps := testDeps(t, fake, t.TempDir())
	deps.loadConfig = func(path string) (*config.Config, error) {
		return nil, fmt.Errorf("boom")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", "missing.yaml"}, &stdout, &stderr, deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if fake.called {
		t.Fatalf("did not expect repl to run")
	}
}

func TestRun_MissingAPIKey(t *testing.T) {
	server := statusServer(t, http.StatusOK)
	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "info")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", cfgPath}, &stdout, &stderr, defaultRunDeps())
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), env.AnthropicAPIKey) {
		t.Fatalf("expected missing API key message, got %q", stderr.String())
	}
}

func TestRun_PingFailure(t *testing.T) {
	server := statusServer(t, http.StatusInternalServerError)
	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "info")

	t.Setenv(env.AnthropicAPIKey, "test-key")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", cfgPath}, &stdout, &stderr, defaultRunDeps())
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "Cannot connect to joecored") {
		t.Fatalf("expected ping failure message, got %q", stderr.String())
	}
}

func TestRun_JoeDirError(t *testing.T) {
	server := statusServer(t, http.StatusOK)
	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "info")

	t.Setenv(env.AnthropicAPIKey, "test-key")

	fake := &fakeRepl{}
	deps := testDeps(t, fake, t.TempDir())
	deps.joeDirPath = func() (string, error) {
		return "", fmt.Errorf("no home")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", cfgPath}, &stdout, &stderr, deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_LoadPolicyError(t *testing.T) {
	server := statusServer(t, http.StatusOK)
	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "info")

	t.Setenv(env.AnthropicAPIKey, "test-key")

	fake := &fakeRepl{}
	deps := testDeps(t, fake, t.TempDir())
	deps.loadPolicy = func(configDir string) (*safety.SafetyPolicy, error) {
		return nil, fmt.Errorf("bad policy")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", cfgPath}, &stdout, &stderr, deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_NewAdapterError(t *testing.T) {
	server := statusServer(t, http.StatusOK)
	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "info")

	t.Setenv(env.AnthropicAPIKey, "test-key")

	fake := &fakeRepl{}
	deps := testDeps(t, fake, t.TempDir())
	deps.newAdapter = func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error) {
		return nil, fmt.Errorf("adapter failed")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", cfgPath}, &stdout, &stderr, deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_ReplError(t *testing.T) {
	server := statusServer(t, http.StatusOK)
	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "info")

	t.Setenv(env.AnthropicAPIKey, "test-key")

	fake := &fakeRepl{err: fmt.Errorf("repl failed")}
	deps := testDeps(t, fake, t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", cfgPath}, &stdout, &stderr, deps)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_DebugOutputAndOTelFailure(t *testing.T) {
	server := statusServer(t, http.StatusOK)
	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "debug")

	t.Setenv(env.AnthropicAPIKey, "test-key")

	fake := &fakeRepl{}
	deps := testDeps(t, fake, t.TempDir())
	deps.setupOTel = func(ctx context.Context, cfg observability.Config) (func(context.Context) error, error) {
		return nil, fmt.Errorf("otel down")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", cfgPath}, &stdout, &stderr, deps)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !strings.Contains(stdout.String(), "Debug mode enabled") {
		t.Fatalf("expected debug output, got %q", stdout.String())
	}
}

func TestRun_CurrentModelMissing(t *testing.T) {
	server := statusServer(t, http.StatusOK)
	addr := strings.TrimPrefix(server.URL, "http://")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf("llm:\n  current: missing\n  available:\n    test:\n      provider: claude\n      model: test-model\nserver:\n  address: %s\nlogging:\n  level: info\n  file: \"\"\n", addr)
	if err := os.WriteFile(configPath, []byte(cfg), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", configPath}, &stdout, &stderr, defaultRunDeps())
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "current model") {
		t.Fatalf("expected current model error, got %q", stderr.String())
	}
}

func TestRun_APIKeyConfigApplied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/status" {
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("expected auth header, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"ok","version":"test","time":"now"}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	addr := strings.TrimPrefix(server.URL, "http://")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf("llm:\n  current: test\n  available:\n    test:\n      provider: claude\n      model: test-model\nserver:\n  address: %s\n  api_key: test-token\nlogging:\n  level: info\n  file: \"\"\n", addr)
	if err := os.WriteFile(configPath, []byte(cfg), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv(env.AnthropicAPIKey, "test-key")

	fake := &fakeRepl{}
	deps := testDeps(t, fake, t.TempDir())

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", configPath}, &stdout, &stderr, deps)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", exitCode, stderr.String())
	}
}

func TestRun_AdapterWithCloser(t *testing.T) {
	server := statusServer(t, http.StatusOK)
	addr := strings.TrimPrefix(server.URL, "http://")
	cfgPath := writeConfig(t, addr, "info")

	t.Setenv(env.AnthropicAPIKey, "test-key")

	closingAdapterInstance := &closingFakeAdapter{}

	fake := &fakeRepl{}
	deps := testDeps(t, fake, t.TempDir())
	deps.newAdapter = func(ctx context.Context, mc config.ModelConfig) (llm.LLMAdapter, error) {
		return closingAdapterInstance, nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runWithDeps(context.Background(), []string{"-config", cfgPath}, &stdout, &stderr, deps)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", exitCode, stderr.String())
	}
	if !fake.called {
		t.Fatalf("expected repl to run")
	}
}

type closingFakeAdapter struct {
	fakeAdapter
	closed bool
}

func (c *closingFakeAdapter) Close() error {
	c.closed = true
	return nil
}
