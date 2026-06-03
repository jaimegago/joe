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
	h.joeCore.Env = append(os.Environ(),
		fmt.Sprintf("JOE_CONFIG=%s", h.configPath),
		fmt.Sprintf("JOE_SERVER_ADDRESS=localhost:%s", h.testPort),
		"JOE_LOG_LEVEL=debug",
		// Dummy LLM key: must clear gemini's minAPIKeyLength (20) placeholder
		// check so joe boots. The e2e tests drive their own client-side
		// mock LLM and never make a real Gemini call, so the value only needs
		// to pass the format check — it is never used to authenticate.
		"GEMINI_API_KEY=e2e-dummy-key-not-a-real-credential",
	)
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
				h.t.Logf("joe logs:\n%s", content)
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

// buildBinaries builds the joe binary
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
  current: gemini-flash
  available:
    gemini-flash:
      provider: gemini
      model: gemini-2.0-flash
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
