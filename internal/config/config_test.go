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

	if cfg.LLM.Current != "claude-sonnet" {
		t.Errorf("default LLM current = %s, want claude-sonnet", cfg.LLM.Current)
	}

	mc, err := cfg.LLM.CurrentModel()
	if err != nil {
		t.Fatalf("CurrentModel() error: %v", err)
	}
	if mc.Provider != "claude" {
		t.Errorf("default model provider = %s, want claude", mc.Provider)
	}
	if mc.Model != "claude-sonnet-4-20250514" {
		t.Errorf("default model = %s, want claude-sonnet-4-20250514", mc.Model)
	}

	if cfg.Logging.Level != "info" {
		t.Errorf("default logging level = %s, want info", cfg.Logging.Level)
	}
}

func TestCurrentModel(t *testing.T) {
	llm := LLMConfig{
		Current: "gf",
		Available: map[string]ModelConfig{
			"gf":  {Provider: "gemini", Model: "gemini-2.0-flash-lite"},
			"cs4": {Provider: "claude", Model: "claude-sonnet-4-20250514"},
		},
	}

	mc, err := llm.CurrentModel()
	if err != nil {
		t.Fatalf("CurrentModel() error: %v", err)
	}
	if mc.Provider != "gemini" || mc.Model != "gemini-2.0-flash-lite" {
		t.Errorf("CurrentModel() = %+v, want gemini/gemini-2.0-flash-lite", mc)
	}
}

func TestCurrentModel_NotFound(t *testing.T) {
	llm := LLMConfig{
		Current:   "missing",
		Available: map[string]ModelConfig{},
	}

	_, err := llm.CurrentModel()
	if err == nil {
		t.Error("CurrentModel() should return error for missing key")
	}
}

func TestModelNames(t *testing.T) {
	llm := LLMConfig{
		Available: map[string]ModelConfig{
			"zulu":  {Provider: "claude", Model: "c"},
			"alpha": {Provider: "gemini", Model: "g"},
			"mike":  {Provider: "claude", Model: "c2"},
		},
	}

	names := llm.ModelNames()
	if len(names) != 3 {
		t.Fatalf("ModelNames() returned %d names, want 3", len(names))
	}
	if names[0] != "alpha" || names[1] != "mike" || names[2] != "zulu" {
		t.Errorf("ModelNames() = %v, want [alpha mike zulu]", names)
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
	mc, err := cfg.LLM.CurrentModel()
	if err != nil {
		t.Fatalf("CurrentModel() error: %v", err)
	}
	if mc.Provider != "claude" {
		t.Errorf("LLM provider = %s, want claude", mc.Provider)
	}
}

func TestLoad_WithFile(t *testing.T) {
	// Create temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `llm:
  current: gemini-flash
  available:
    gemini-flash:
      provider: gemini
      model: gemini-2.0-flash-exp

logging:
  level: debug
`

	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	mc, err := cfg.LLM.CurrentModel()
	if err != nil {
		t.Fatalf("CurrentModel() error: %v", err)
	}
	if mc.Provider != "gemini" {
		t.Errorf("LLM provider = %s, want gemini", mc.Provider)
	}
	if mc.Model != "gemini-2.0-flash-exp" {
		t.Errorf("LLM model = %s, want gemini-2.0-flash-exp", mc.Model)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging level = %s, want debug", cfg.Logging.Level)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	os.Setenv("JOE_LLM_PROVIDER", "gemini")
	os.Setenv("JOE_LLM_MODEL", "test-model")
	defer func() {
		os.Unsetenv("JOE_LLM_PROVIDER")
		os.Unsetenv("JOE_LLM_MODEL")
	}()

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	mc, err := cfg.LLM.CurrentModel()
	if err != nil {
		t.Fatalf("CurrentModel() error: %v", err)
	}
	if mc.Provider != "gemini" {
		t.Errorf("LLM provider = %s, want gemini (from env)", mc.Provider)
	}
	if mc.Model != "test-model" {
		t.Errorf("LLM model = %s, want test-model (from env)", mc.Model)
	}
}

func TestLoad_ComputedFields(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	expectedInterval := time.Duration(cfg.Refresh.IntervalMinutes) * time.Minute
	if cfg.Refresh.Interval != expectedInterval {
		t.Errorf("Refresh interval = %v, want %v", cfg.Refresh.Interval, expectedInterval)
	}

	expectedTimeout := time.Duration(cfg.Refresh.LLMBudget.BatchTimeoutSec) * time.Second
	if cfg.Refresh.LLMBudget.BatchTimeout != expectedTimeout {
		t.Errorf("Batch timeout = %v, want %v", cfg.Refresh.LLMBudget.BatchTimeout, expectedTimeout)
	}
}

func TestSave(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := defaultConfig()
	cfg.LLM.Current = "gemini-flash"
	cfg.LLM.Available["gemini-flash"] = ModelConfig{Provider: "gemini", Model: "gemini-2.0-flash-lite"}
	cfg.Logging.Level = "debug"

	if err := Save(cfg, configPath); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() after Save() returned error: %v", err)
	}

	mc, err := loadedCfg.LLM.CurrentModel()
	if err != nil {
		t.Fatalf("CurrentModel() error: %v", err)
	}
	if mc.Provider != "gemini" {
		t.Errorf("Loaded config LLM provider = %s, want gemini", mc.Provider)
	}
	if loadedCfg.Logging.Level != "debug" {
		t.Errorf("Loaded config logging level = %s, want debug", loadedCfg.Logging.Level)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	if err := os.WriteFile(configPath, []byte("not: valid: yaml:"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Error("Load() with invalid YAML should return error")
	}
}

func TestLoad_HomeDirectory(t *testing.T) {
	_, err := Load("~/nonexistent.yaml")
	if err != nil {
		t.Errorf("Load() with ~ path returned unexpected error: %v", err)
	}
}

func TestLoad_FullConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `llm:
  current: gemini-flash
  available:
    gemini-flash:
      provider: gemini
      model: gemini-2.0-flash-exp
    claude-sonnet:
      provider: claude
      model: claude-sonnet-4-20250514

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

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	mc, err := cfg.LLM.CurrentModel()
	if err != nil {
		t.Fatalf("CurrentModel() error: %v", err)
	}
	if mc.Provider != "gemini" {
		t.Errorf("LLM provider = %s, want gemini", mc.Provider)
	}

	if len(cfg.LLM.Available) != 2 {
		t.Errorf("Available models = %d, want 2", len(cfg.LLM.Available))
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
