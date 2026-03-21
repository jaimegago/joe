//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// JoeTestHarness manages joe and joe-core for E2E testing
type JoeTestHarness struct {
	t            *testing.T
	joeCore      *exec.Cmd
	joeCoreLog   *os.File
	apiURL       string
	httpClient   *http.Client
	configPath   string
	tmpDir       string
	testPort     string
	cleanupFuncs []func()
}

// NewTestHarness creates a new test harness
func NewTestHarness(t *testing.T) *JoeTestHarness {
	tmpDir := t.TempDir()
	// Use unique port for each test to avoid conflicts
	testPort := "7778"
	// Setup test config
	configPath := filepath.Join(tmpDir, "config.yaml")
	setupTestConfig(t, configPath, testPort)
	return &JoeTestHarness{
		t:          t,
		apiURL:     fmt.Sprintf("http://localhost:%s", testPort),
		httpClient: &http.Client{Timeout: 5 * time.Second},
		configPath: configPath,
		tmpDir:     tmpDir,
		testPort:   testPort,
	}
}

// Start builds and starts joe-core
func (h *JoeTestHarness) Start() error {
	// Build joe-core if needed
	if err := h.buildBinaries(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	// Start joe-core
	logFile, err := os.Create(filepath.Join(h.tmpDir, "joe-core.log"))
	if err != nil {
		return err
	}
	h.joeCoreLog = logFile
	// Find repository root and joe-core binary
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return fmt.Errorf("failed to find repo root: %w", err)
	}
	joeCorePath := filepath.Join(repoRoot, "joe-core")
	h.joeCore = exec.Command(joeCorePath)
	h.joeCore.Env = append(os.Environ(),
		fmt.Sprintf("JOE_CONFIG=%s", h.configPath),
		fmt.Sprintf("JOE_SERVER_ADDRESS=localhost:%s", h.testPort),
		"JOE_LOG_LEVEL=debug",
		// Use mock/test API keys if needed
		"GEMINI_API_KEY=test-key-for-e2e",
	)
	h.joeCore.Stdout = logFile
	h.joeCore.Stderr = logFile
	if err := h.joeCore.Start(); err != nil {
		return fmt.Errorf("failed to start joe-core: %w", err)
	}
	h.t.Logf("Started joe-core (PID: %d) on port %s", h.joeCore.Process.Pid, h.testPort)
	// Wait for API to be ready
	if err := h.waitForAPI(30 * time.Second); err != nil {
		h.Stop()
		return err
	}
	return nil
}

// Stop stops joe-core and cleans up
func (h *JoeTestHarness) Stop() {
	if h.joeCore != nil && h.joeCore.Process != nil {
		h.t.Logf("Stopping joe-core (PID: %d)", h.joeCore.Process.Pid)
		h.joeCore.Process.Kill()
		h.joeCore.Wait()
	}
	if h.joeCoreLog != nil {
		h.joeCoreLog.Close()
		// Print logs on failure
		if h.t.Failed() {
			h.t.Log("=== joe-core logs ===")
			content, _ := os.ReadFile(h.joeCoreLog.Name())
			h.t.Log(string(content))
		}
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

// RunCommand runs the joe CLI and returns output
func (h *JoeTestHarness) RunCommand(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "./joe", args...)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("JOE_CONFIG=%s", h.configPath),
		fmt.Sprintf("JOE_SERVER_ADDRESS=localhost:%s", h.testPort),
	)
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
				h.t.Logf("joe-core logs:\n%s", content)
			}
			return fmt.Errorf("timeout waiting for API to start")
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/status", h.apiURL), nil)
			if err != nil {
				continue
			}
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

// buildBinaries builds joe and joe-core
func (h *JoeTestHarness) buildBinaries() error {
	h.t.Log("Building binaries...")
	// Find repository root (go up two directories from test/e2e)
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return fmt.Errorf("failed to find repo root: %w", err)
	}
	cmd := exec.Command("make", "build")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed: %s", output)
	}
	h.t.Log("Build complete")
	return nil
}

// setupTestConfig creates a test configuration file
func setupTestConfig(t *testing.T, path, port string) {
	config := fmt.Sprintf(`llm:
  provider: gemini
  current_model: gemini-2.0-flash
  available_models:
    - gemini-2.0-flash
server:
  address: localhost:%s
logging:
  level: debug
refresh:
  interval_minutes: 60
`, port)
	if err := os.WriteFile(path, []byte(config), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
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
