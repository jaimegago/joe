package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	if cfg == nil {
		t.Fatal("defaultConfig() returned nil")
	}

	if cfg.LLM.Provider != "claude" {
		t.Errorf("default LLM provider = %s, want claude", cfg.LLM.Provider)
	}

	if cfg.Logging.Level != "info" {
		t.Errorf("default logging level = %s, want info", cfg.Logging.Level)
	}
}

func TestLoad_NoFile(t *testing.T) {
	// Load with non-existent file should return defaults
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("Load() with non-existent file returned error: %v", err)
	}

	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}

	// Should have defaults
	if cfg.LLM.Provider != "claude" {
		t.Errorf("LLM provider = %s, want claude", cfg.LLM.Provider)
	}
}

func TestLoad_WithFile(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `llm:
  provider: gemini
  model: gemini-2.0-flash-exp

logging:
  level: debug
`

	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Load config
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.LLM.Provider != "gemini" {
		t.Errorf("LLM provider = %s, want gemini", cfg.LLM.Provider)
	}

	if cfg.LLM.Model != "gemini-2.0-flash-exp" {
		t.Errorf("LLM model = %s, want gemini-2.0-flash-exp", cfg.LLM.Model)
	}

	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging level = %s, want debug", cfg.Logging.Level)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	// Set env variables
	os.Setenv("JOE_LLM_PROVIDER", "gemini")
	os.Setenv("JOE_LLM_MODEL", "test-model")
	defer func() {
		os.Unsetenv("JOE_LLM_PROVIDER")
		os.Unsetenv("JOE_LLM_MODEL")
	}()

	// Load config (no file)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.LLM.Provider != "gemini" {
		t.Errorf("LLM provider = %s, want gemini (from env)", cfg.LLM.Provider)
	}

	if cfg.LLM.Model != "test-model" {
		t.Errorf("LLM model = %s, want test-model (from env)", cfg.LLM.Model)
	}
}

func TestLoad_ComputedFields(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	// Check that Interval is computed from IntervalMinutes
	expectedInterval := time.Duration(cfg.Refresh.IntervalMinutes) * time.Minute
	if cfg.Refresh.Interval != expectedInterval {
		t.Errorf("Refresh interval = %v, want %v", cfg.Refresh.Interval, expectedInterval)
	}

	// Check that BatchTimeout is computed from BatchTimeoutSec
	expectedTimeout := time.Duration(cfg.Refresh.LLMBudget.BatchTimeoutSec) * time.Second
	if cfg.Refresh.LLMBudget.BatchTimeout != expectedTimeout {
		t.Errorf("Batch timeout = %v, want %v", cfg.Refresh.LLMBudget.BatchTimeout, expectedTimeout)
	}
}

func TestSave(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create config
	cfg := defaultConfig()
	cfg.LLM.Provider = "gemini"
	cfg.Logging.Level = "debug"

	// Save config
	if err := Save(cfg, configPath); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	// Load it back
	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() after Save() returned error: %v", err)
	}

	if loadedCfg.LLM.Provider != "gemini" {
		t.Errorf("Loaded config LLM provider = %s, want gemini", loadedCfg.LLM.Provider)
	}

	if loadedCfg.Logging.Level != "debug" {
		t.Errorf("Loaded config logging level = %s, want debug", loadedCfg.Logging.Level)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write invalid YAML
	if err := os.WriteFile(configPath, []byte("not: valid: yaml:"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Load should return error
	_, err := Load(configPath)
	if err == nil {
		t.Error("Load() with invalid YAML should return error")
	}
}

func TestLoad_HomeDirectory(t *testing.T) {
	// This test just verifies that ~ expansion doesn't error
	// We can't test the actual expansion without mocking the home directory
	_, err := Load("~/nonexistent.yaml")
	if err != nil {
		t.Errorf("Load() with ~ path returned unexpected error: %v", err)
	}
}

func TestLoad_FullConfig(t *testing.T) {
	// Create temp config file with all fields
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `llm:
  provider: gemini
  model: gemini-2.0-flash-exp

refresh:
  interval_minutes: 10
  llm_budget:
    max_calls_per_hour: 200
    batch_threshold: 20
    batch_timeout_sec: 60

notifications:
  desktop:
    enabled: true
    priority_threshold: high
  slack:
    enabled: true
    priority_threshold: urgent
  quiet_hours:
    enabled: true
    start: "23:00"
    end: "07:00"
    timezone: UTC

logging:
  level: debug
  file: /var/log/joe.log
`

	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Load config
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	// Verify all fields loaded correctly
	if cfg.LLM.Provider != "gemini" {
		t.Errorf("LLM provider = %s, want gemini", cfg.LLM.Provider)
	}

	if cfg.Refresh.IntervalMinutes != 10 {
		t.Errorf("Refresh interval = %d, want 10", cfg.Refresh.IntervalMinutes)
	}

	if cfg.Refresh.LLMBudget.MaxCallsPerHour != 200 {
		t.Errorf("Max calls per hour = %d, want 200", cfg.Refresh.LLMBudget.MaxCallsPerHour)
	}

	if !cfg.Notifications.Desktop.Enabled {
		t.Error("Desktop notifications should be enabled")
	}

	if cfg.Notifications.Desktop.PriorityThreshold != "high" {
		t.Errorf("Desktop priority = %s, want high", cfg.Notifications.Desktop.PriorityThreshold)
	}

	if cfg.Logging.File != "/var/log/joe.log" {
		t.Errorf("Log file = %s, want /var/log/joe.log", cfg.Logging.File)
	}
}
