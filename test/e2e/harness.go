//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// e2eServiceAccountKey is the bearer token for the "e2e" service account
// configured in setupTestConfig. Joe refuses to boot without an identity
// source, so the harness registers a service account and authenticates its
// API requests with this key.
const e2eServiceAccountKey = "e2e-service-account-key-not-a-real-credential"

// JoeTestHarness manages the joe server for E2E testing
type JoeTestHarness struct {
	t            *testing.T
	joeCore      *exec.Cmd
	joeCoreLog   *os.File
	apiURL       string
	httpClient   *http.Client
	configPath   string
	tmpDir       string
	testPort     string
	metricsPort  string
	cleanupFuncs []func()

	// Responder is the scripted LLM joe is pointed at. It runs DefaultScript
	// until a test calls Responder.SetScript.
	Responder *Responder
}

// ServiceAccount is a machine identity the harness adds to joe's config
// beside the "e2e" account. Configuring any service account enforces RBAC,
// so every account but one an admin bootstraps is a grantless non-admin.
type ServiceAccount struct {
	Name string
	Key  string
}

// harnessConfig is what HarnessOptions change in the generated config.
type harnessConfig struct {
	serviceAccounts        []ServiceAccount
	refreshIntervalMinutes int
	extraYAML              string
}

// HarnessOption changes the config joe boots with.
type HarnessOption func(*harnessConfig)

// WithServiceAccount adds a service account beside "e2e".
func WithServiceAccount(name, key string) HarnessOption {
	return func(c *harnessConfig) {
		c.serviceAccounts = append(c.serviceAccounts, ServiceAccount{Name: name, Key: key})
	}
}

// WithRefreshIntervalMinutes sets the Core Agent's graph refresh interval. The
// refresher ticks on it and never at boot, so it is how long a test waits for
// a registered component's nodes to appear in the graph.
func WithRefreshIntervalMinutes(m int) HarnessOption {
	return func(c *harnessConfig) { c.refreshIntervalMinutes = m }
}

// WithConfigYAML appends top-level YAML sections to the config. They must not
// repeat a section the harness writes (llm, database, server, logging,
// refresh).
func WithConfigYAML(yaml string) HarnessOption {
	return func(c *harnessConfig) { c.extraYAML = yaml }
}

// NewTestHarness creates a new test harness
func NewTestHarness(t *testing.T, opts ...HarnessOption) *JoeTestHarness {
	cfg := harnessConfig{refreshIntervalMinutes: 60}
	for _, o := range opts {
		o(&cfg)
	}
	tmpDir := t.TempDir()
	// An OS-assigned port per joe instance, so tests do not collide with each
	// other or with a joe the developer has running.
	testPort := freePort(t)
	responder, err := NewResponder(t)
	if err != nil {
		t.Fatalf("failed to start the scripted responder: %v", err)
	}
	// Setup test config
	configPath := filepath.Join(tmpDir, "config.yaml")
	setupTestConfig(t, configPath, testPort, responder.BaseURL(), tmpDir, cfg)
	return &JoeTestHarness{
		t:           t,
		apiURL:      fmt.Sprintf("http://localhost:%s", testPort),
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		configPath:  configPath,
		tmpDir:      tmpDir,
		testPort:    testPort,
		metricsPort: freePort(t),
		Responder:   responder,
	}
}

// freePort asks the OS for a free loopback port and releases it for joe to
// bind. The window between release and joe's bind is a race in principle;
// nothing else in the suite asks for ports, so it does not lose in practice.
func freePort(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find a free port: %v", err)
	}
	defer ln.Close()
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

// joeEnv is the environment joe runs under: the test process's own, with every
// JOE_ variable removed so a developer's shell cannot override the harness's
// config (JOE_LLM_PROVIDER and JOE_LLM_MODEL apply after the file is read).
func (h *JoeTestHarness) joeEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "JOE_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		fmt.Sprintf("JOE_CONFIG=%s", h.configPath),
		fmt.Sprintf("JOE_SERVER_ADDRESS=localhost:%s", h.testPort),
		// The Prometheus listener defaults to a fixed :9090, which every joe
		// instance would otherwise contend for.
		fmt.Sprintf("OTEL_METRICS_PORT=%s", h.metricsPort),
	)
}

// Start builds and starts joe
func (h *JoeTestHarness) Start() error {
	// Build joe if needed
	if err := h.buildBinaries(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	// Start joe
	logFile, err := os.Create(filepath.Join(h.tmpDir, "joe.log"))
	if err != nil {
		return err
	}
	h.joeCoreLog = logFile
	// Find repository root and joe binary
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return fmt.Errorf("failed to find repo root: %w", err)
	}
	joeCorePath := filepath.Join(repoRoot, "joe")
	h.joeCore = exec.Command(joeCorePath)
	// No LLM key of any kind: the config points joe's openai-compat provider
	// at the harness's scripted responder, and that provider sends no
	// Authorization header without one.
	h.joeCore.Env = append(h.joeEnv(), "JOE_LOG_LEVEL=debug")
	h.joeCore.Stdout = logFile
	h.joeCore.Stderr = logFile
	if err := h.joeCore.Start(); err != nil {
		return fmt.Errorf("failed to start joe: %w", err)
	}
	h.t.Logf("Started joe (PID: %d) on port %s", h.joeCore.Process.Pid, h.testPort)
	// Wait for API to be ready
	if err := h.waitForAPI(30 * time.Second); err != nil {
		h.Stop()
		return err
	}
	return nil
}

// Stop stops joe and cleans up
func (h *JoeTestHarness) Stop() {
	if h.joeCore != nil && h.joeCore.Process != nil {
		h.t.Logf("Stopping joe (PID: %d)", h.joeCore.Process.Pid)
		h.joeCore.Process.Kill()
		h.joeCore.Wait()
	}
	if h.joeCoreLog != nil {
		h.joeCoreLog.Close()
		// Print logs on failure
		if h.t.Failed() {
			h.t.Log("=== joe logs ===")
			content, _ := os.ReadFile(h.joeCoreLog.Name())
			h.t.Log(string(content))
		}
	}
	if h.Responder != nil {
		h.Responder.Close()
		for _, c := range h.Responder.Rejections() {
			h.t.Errorf("scripted responder rejected a request joe sent: %s", c.Rejected)
		}
		h.Responder = nil
	}
	for _, cleanup := range h.cleanupFuncs {
		cleanup()
	}
}

// GetStatus checks the API status endpoint
func (h *JoeTestHarness) GetStatus() (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("%s/api/v1/status", h.apiURL), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e2eServiceAccountKey)
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status=%d, body=%s", resp.StatusCode, body)
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// TaskResponse is the part of POST /api/v1/tasks's response the e2e tests
// assert on. It is decoded independently of internal/api, so a change to the
// wire shape breaks a test rather than following silently.
type TaskResponse struct {
	TaskID      string   `json:"task_id"`
	SessionID   string   `json:"session_id"`
	Status      string   `json:"status"`
	Iterations  int      `json:"iterations"`
	FinalAnswer string   `json:"final_answer"`
	ToolsUsed   []string `json:"tools_used"`
	Error       string   `json:"error"`
	TotalTokens struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"total_tokens"`
	Steps []struct {
		ToolResults []struct {
			Name  string `json:"name"`
			Error string `json:"error"`
		} `json:"tool_results"`
	} `json:"steps"`
}

// RunTask posts one message to POST /api/v1/tasks and decodes the turn.
func (h *JoeTestHarness) RunTask(message string) (*TaskResponse, error) {
	payload, err := json.Marshal(map[string]string{"message": message})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, fmt.Sprintf("%s/api/v1/tasks", h.apiURL), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e2eServiceAccountKey)
	req.Header.Set("Content-Type", "application/json")
	// A turn makes several LLM round trips and a tool call, so it gets more
	// time than a status probe.
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status=%d, body=%s", resp.StatusCode, body)
	}
	var out TaskResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode %s: %w", body, err)
	}
	return &out, nil
}

// RunCommand runs the joe CLI and returns output
func (h *JoeTestHarness) RunCommand(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "./joe", args...)
	cmd.Env = h.joeEnv()
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// waitForAPI polls the status endpoint until it responds
func (h *JoeTestHarness) waitForAPI(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Print logs for debugging
			if h.joeCoreLog != nil {
				content, _ := os.ReadFile(h.joeCoreLog.Name())
				h.t.Logf("joe logs:\n%s", content)
			}
			return fmt.Errorf("timeout waiting for API to start")
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/status", h.apiURL), nil)
			if err != nil {
				continue
			}
			req.Header.Set("Authorization", "Bearer "+e2eServiceAccountKey)
			resp, err := h.httpClient.Do(req)
			if err == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				h.t.Log("API is ready")
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}
}

// buildOnce builds joe once per test process. Every test runs the same
// binary, and `make build` runs `npm ci` and the UI build, so rebuilding per
// test cost ~25s each and bought nothing.
var (
	buildOnce sync.Once
	buildErr  error
)

// buildBinaries builds the joe binary, once per test process
func (h *JoeTestHarness) buildBinaries() error {
	buildOnce.Do(func() {
		h.t.Log("Building binaries...")
		// Find repository root (go up two directories from test/e2e)
		repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			buildErr = fmt.Errorf("failed to find repo root: %w", err)
			return
		}
		cmd := exec.Command("make", "build")
		cmd.Dir = repoRoot
		output, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("build failed: %s", output)
			return
		}
		h.t.Log("Build complete")
	})
	return buildErr
}

// setupTestConfig creates a test configuration file. Setting llm.current
// skips auto-selection, which never picks openai-compat; base_url is the only
// way to point that provider anywhere, since no env var carries it.
//
// The database, its encryption key and the session archive live in the test's
// temp dir, so tests share no state with each other or with the developer's
// own joe. HOME cannot do this: joe resolves ~/.joe through user.Current() on
// purpose (internal/paths), so ~/.joe/skills is still read from the real home.
func setupTestConfig(t *testing.T, path, port, llmBaseURL, dataDir string, hc harnessConfig) {
	var accounts strings.Builder
	for _, sa := range hc.serviceAccounts {
		fmt.Fprintf(&accounts, "    - name: %s\n      key: %s\n", sa.Name, sa.Key)
	}
	config := fmt.Sprintf(`llm:
  current: e2e-responder
  available:
    e2e-responder:
      provider: openai-compat
      model: e2e-responder
      base_url: %s
database:
  driver: sqlite
  dsn: %s
  encryption_key_path: %s
server:
  session_archive_dir: %s
  address: localhost:%s
  service_accounts:
    - name: e2e
      key: e2e-service-account-key-not-a-real-credential
%slogging:
  level: debug
refresh:
  interval_minutes: %d
%s`, llmBaseURL, filepath.Join(dataDir, "joe.db"), filepath.Join(dataDir, "encryption.key"), filepath.Join(dataDir, "session-archive"), port,
		accounts.String(), hc.refreshIntervalMinutes, hc.extraYAML)
	if err := os.WriteFile(path, []byte(config), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
}

// DBPath is the sqlite database joe writes, in the test's temp dir.
func (h *JoeTestHarness) DBPath() string {
	return filepath.Join(h.tmpDir, "joe.db")
}

// BootstrapAdmin grants principal admin through `joe admin bootstrap`, the
// offline first-admin command, against the harness's config and database. It
// builds joe if needed and must run before Start: the command is the
// deployment's own bootstrap path, so no admin row is written by the test.
func (h *JoeTestHarness) BootstrapAdmin(principal string) error {
	if err := h.buildBinaries(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(repoRoot, "joe"), "admin", "bootstrap", principal, "--config", h.configPath)
	cmd.Env = h.joeEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("joe admin bootstrap %s: %w: %s", principal, err, out)
	}
	return nil
}

// Do sends one API request authenticated with key, JSON-encoding body when it
// is non-nil, and returns the status and the raw response body.
func (h *JoeTestHarness) Do(method, path, key string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, h.apiURL+path, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	return resp.StatusCode, out, err
}

// CreateTestFile creates a file in the test directory
func (h *JoeTestHarness) CreateTestFile(filename, content string) (string, error) {
	path := filepath.Join(h.tmpDir, filename)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// TmpDir returns the temporary directory for this test
func (h *JoeTestHarness) TmpDir() string {
	return h.tmpDir
}
