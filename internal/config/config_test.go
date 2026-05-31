package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/env"
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
	if mc.Provider != providerClaude {
		t.Errorf("default model provider = %s, want %s", mc.Provider, providerClaude)
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

// ---- ValidateAPIKeys ----

func TestValidateAPIKeys_ClaudeSuccess(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	if err := ValidateAPIKeys(ModelConfig{Provider: "claude", Model: "claude-sonnet"}); err != nil {
		t.Errorf("ValidateAPIKeys() unexpected error: %v", err)
	}
}

func TestValidateAPIKeys_ClaudeMissingKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	err := ValidateAPIKeys(ModelConfig{Provider: "claude", Model: "claude-sonnet"})
	if err == nil {
		t.Error("ValidateAPIKeys() expected error for missing ANTHROPIC_API_KEY")
	}
}

func TestValidateAPIKeys_GeminiSuccess_GeminiKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("GOOGLE_API_KEY", "")
	if err := ValidateAPIKeys(ModelConfig{Provider: "gemini", Model: "gemini-flash"}); err != nil {
		t.Errorf("ValidateAPIKeys() unexpected error: %v", err)
	}
}

func TestValidateAPIKeys_GeminiSuccess_GoogleKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "test-key")
	if err := ValidateAPIKeys(ModelConfig{Provider: "gemini", Model: "gemini-flash"}); err != nil {
		t.Errorf("ValidateAPIKeys() unexpected error: %v", err)
	}
}

func TestValidateAPIKeys_GeminiMissingBothKeys(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	err := ValidateAPIKeys(ModelConfig{Provider: "gemini", Model: "gemini-flash"})
	if err == nil {
		t.Error("ValidateAPIKeys() expected error when both Gemini keys are missing")
	}
}

func TestValidateAPIKeys_UnsupportedProvider(t *testing.T) {
	err := ValidateAPIKeys(ModelConfig{Provider: "ollama", Model: "llama3"})
	if err == nil {
		t.Error("ValidateAPIKeys() expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("ValidateAPIKeys() error = %v, want error containing 'unsupported'", err)
	}
}

// ---- ValidateAPIKeysWithUserMessage ----

func TestValidateAPIKeysWithUserMessage_UnsupportedProvider(t *testing.T) {
	err := ValidateAPIKeysWithUserMessage(ModelConfig{Provider: "ollama", Model: "llama3"})
	if err == nil {
		t.Error("expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error = %v, want 'not supported'", err)
	}
}

// ---- TLS config ----

func TestServerConfig_TLSConfigured(t *testing.T) {
	tests := []struct {
		name string
		cfg  ServerConfig
		want bool
	}{
		{"both set", ServerConfig{TLSCertFile: "/etc/joe/tls.crt", TLSKeyFile: "/etc/joe/tls.key"}, true},
		{"only cert", ServerConfig{TLSCertFile: "/etc/joe/tls.crt"}, false},
		{"only key", ServerConfig{TLSKeyFile: "/etc/joe/tls.key"}, false},
		{"neither", ServerConfig{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.TLSConfigured(); got != tt.want {
				t.Errorf("TLSConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoad_TLSConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yaml := `server:
  address: ":8443"
  tls_cert_file: /etc/joe/tls.crt
  tls_key_file: /etc/joe/tls.key
  tls_enabled: true
`
	if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.TLSCertFile != "/etc/joe/tls.crt" {
		t.Errorf("TLSCertFile = %q, want /etc/joe/tls.crt", cfg.Server.TLSCertFile)
	}
	if cfg.Server.TLSKeyFile != "/etc/joe/tls.key" {
		t.Errorf("TLSKeyFile = %q, want /etc/joe/tls.key", cfg.Server.TLSKeyFile)
	}
	if !cfg.Server.TLSEnabled {
		t.Error("TLSEnabled = false, want true")
	}
	if !cfg.Server.TLSConfigured() {
		t.Error("TLSConfigured() = false, want true")
	}
}

func TestValidateAPIKeysWithUserMessage_ClaudeMissingKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	err := ValidateAPIKeysWithUserMessage(ModelConfig{Provider: "claude", Model: "claude-sonnet"})
	if err == nil {
		t.Error("expected error for missing ANTHROPIC_API_KEY")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("error = %v, want mention of ANTHROPIC_API_KEY", err)
	}
}

func TestValidateAPIKeysWithUserMessage_GeminiMissingKeys(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	err := ValidateAPIKeysWithUserMessage(ModelConfig{Provider: "gemini", Model: "gemini-flash"})
	if err == nil {
		t.Error("expected error for missing Gemini keys")
	}
	if !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Errorf("error = %v, want mention of GEMINI_API_KEY", err)
	}
}

func TestValidateAPIKeysWithUserMessage_ClaudeSuccess(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	if err := ValidateAPIKeysWithUserMessage(ModelConfig{Provider: "claude", Model: "claude-sonnet"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateAPIKeysWithUserMessage_GeminiSuccess(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")
	if err := ValidateAPIKeysWithUserMessage(ModelConfig{Provider: "gemini", Model: "gemini-flash"}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// ---- applyEnvOverrides additional branches ----

func TestLoad_EnvOverrides_LogLevel(t *testing.T) {
	t.Setenv("JOE_LOG_LEVEL", "debug")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %s, want debug (from JOE_LOG_LEVEL)", cfg.Logging.Level)
	}
}

func TestLoad_EnvOverrides_ServerAddress(t *testing.T) {
	t.Setenv("JOE_SERVER_ADDRESS", "localhost:9999")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Server.Address != "localhost:9999" {
		t.Errorf("Server.Address = %s, want localhost:9999", cfg.Server.Address)
	}
}

// TestLoad_EnvOverrides_APIKey verifies JOE_API_KEY folds into the reserved
// "server" service account (Identity Phase D): the env key becomes the key of
// the svc:server account, which is what co-located clients present via
// LoopbackKey. This is the env equivalent of the old single server.api_key.
func TestLoad_EnvOverrides_APIKey(t *testing.T) {
	t.Setenv("JOE_API_KEY", "super-secret")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got := cfg.Server.LoopbackKey(); got != "super-secret" {
		t.Errorf("LoopbackKey() = %q, want super-secret", got)
	}
	var found bool
	for _, sa := range cfg.Server.ServiceAccounts {
		if sa.Name == "server" {
			found = true
			if sa.Key != "super-secret" {
				t.Errorf("server service account key = %q, want super-secret", sa.Key)
			}
		}
	}
	if !found {
		t.Error("JOE_API_KEY did not create a \"server\" service account")
	}
}

func TestLoad_EnvOverrides_DatabaseDSN(t *testing.T) {
	t.Setenv("JOE_DATABASE_DSN", "/tmp/custom-joe.db")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.Database.DSN != "/tmp/custom-joe.db" {
		t.Errorf("Database.DSN = %s, want /tmp/custom-joe.db", cfg.Database.DSN)
	}
}

func TestLoad_EnvOverrides_ModelOnly(t *testing.T) {
	// Override only model, not provider — exercises the partial-override branch
	t.Setenv("JOE_LLM_PROVIDER", "")
	t.Setenv("JOE_LLM_MODEL", "claude-opus-4")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	mc, err := cfg.LLM.CurrentModel()
	if err != nil {
		t.Fatalf("CurrentModel() error: %v", err)
	}
	if mc.Model != "claude-opus-4" {
		t.Errorf("model = %s, want claude-opus-4 (from JOE_LLM_MODEL)", mc.Model)
	}
}

// ---- Save tilde expansion ----

func TestSave_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	tmpDir, err := os.MkdirTemp(home, "joe-test-save-*")
	if err != nil {
		t.Skip("cannot create temp dir in home dir")
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	subdir := filepath.Base(tmpDir)
	tildeConfigPath := "~/" + subdir + "/config.yaml"

	cfg := defaultConfig()
	if err := Save(cfg, tildeConfigPath); err != nil {
		t.Fatalf("Save() with tilde path returned error: %v", err)
	}

	expected := filepath.Join(home, subdir, "config.yaml")
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Errorf("Save() with tilde did not create file at %s", expected)
	}
}

// ---- AutoSelectProvider ----

// loadNoExplicit loads a defaults-only config with no explicit provider
// preference (JOE_LLM_PROVIDER / JOE_LLM_MODEL cleared), so AutoSelectProvider
// is free to choose from whichever key is present.
func loadNoExplicit(t *testing.T) *Config {
	t.Helper()
	t.Setenv("JOE_LLM_PROVIDER", "")
	t.Setenv("JOE_LLM_MODEL", "")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	return cfg
}

func TestAutoSelectProvider(t *testing.T) {
	tests := []struct {
		name         string
		anthropicKey string
		geminiKey    string
		googleKey    string
		wantErr      bool
		wantProvider string
		wantModel    string
	}{
		{
			name:         "claude only",
			anthropicKey: "sk-ant-test",
			wantProvider: providerClaude,
			wantModel:    defaultLLMModel,
		},
		{
			name:         "gemini only",
			geminiKey:    "gemini-test",
			wantProvider: providerGemini,
			wantModel:    defaultGeminiModel,
		},
		{
			name:         "both keys keep claude",
			anthropicKey: "sk-ant-test",
			geminiKey:    "gemini-test",
			wantProvider: providerClaude,
			wantModel:    defaultLLMModel,
		},
		{
			name:    "neither key errors",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ANTHROPIC_API_KEY", tt.anthropicKey)
			t.Setenv("GEMINI_API_KEY", tt.geminiKey)
			t.Setenv("GOOGLE_API_KEY", tt.googleKey)

			cfg := loadNoExplicit(t)
			err := cfg.AutoSelectProvider()

			if tt.wantErr {
				if err == nil {
					t.Fatal("AutoSelectProvider() = nil error, want actionable error")
				}
				// The message must name both providers, both env vars, and the
				// override path so a stranger knows exactly what to do.
				for _, want := range []string{
					env.AnthropicAPIKey, env.GeminiAPIKey, env.GoogleAPIKey,
					"JOE_LLM_PROVIDER", "JOE_LLM_MODEL",
				} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error message missing %q:\n%s", want, err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("AutoSelectProvider() returned error: %v", err)
			}

			mc, err := cfg.LLM.CurrentModel()
			if err != nil {
				t.Fatalf("CurrentModel() error: %v", err)
			}
			if mc.Provider != tt.wantProvider {
				t.Errorf("provider = %s, want %s", mc.Provider, tt.wantProvider)
			}
			if mc.Model != tt.wantModel {
				t.Errorf("model = %s, want %s", mc.Model, tt.wantModel)
			}
			if mc.Model == "" {
				t.Error("auto-selected model is empty; want a real default model")
			}
		})
	}
}

func TestAutoSelectProvider_ExplicitEnvWins(t *testing.T) {
	// User explicitly asks for Gemini via env, but only the Claude key is
	// present. Auto-select must NOT override the explicit choice (it would
	// otherwise pick Claude from the only available key).
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("JOE_LLM_PROVIDER", "gemini")
	t.Setenv("JOE_LLM_MODEL", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if err := cfg.AutoSelectProvider(); err != nil {
		t.Fatalf("AutoSelectProvider() returned error: %v", err)
	}

	mc, err := cfg.LLM.CurrentModel()
	if err != nil {
		t.Fatalf("CurrentModel() error: %v", err)
	}
	if mc.Provider != providerGemini {
		t.Errorf("provider = %s, want %s (explicit JOE_LLM_PROVIDER must win)", mc.Provider, providerGemini)
	}
}

func TestAutoSelectProvider_ConfigCurrentWins(t *testing.T) {
	// An explicit llm.current in the config file is a provider preference too:
	// auto-select must leave it alone even when only the other key is present.
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("JOE_LLM_PROVIDER", "")
	t.Setenv("JOE_LLM_MODEL", "")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configYAML := `llm:
  current: gemini-flash
  available:
    gemini-flash:
      provider: gemini
      model: gemini-2.0-flash-exp
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if err := cfg.AutoSelectProvider(); err != nil {
		t.Fatalf("AutoSelectProvider() returned error: %v", err)
	}

	mc, err := cfg.LLM.CurrentModel()
	if err != nil {
		t.Fatalf("CurrentModel() error: %v", err)
	}
	if mc.Provider != providerGemini || mc.Model != "gemini-2.0-flash-exp" {
		t.Errorf("model = %+v, want gemini/gemini-2.0-flash-exp (explicit llm.current must win)", mc)
	}
}

// TestLoad_RejectsNonUSDCurrencyWithZeroFXRate is the Stream-G-G2
// boundary-validation test: a config with a non-USD currency and a
// zero/unset USDToConfiguredRate must fail to load, NOT boot silently
// mispriced as if the rate were 1.0. The error must surface through
// Load's normal return-error path so runWithDeps' existing
// "failed to load config" branch fails the boot sequence.
func TestLoad_RejectsNonUSDCurrencyWithZeroFXRate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `llm:
  current: claude-sonnet
  available:
    claude-sonnet:
      provider: claude
      model: claude-sonnet-4-20250514
  currency: EUR
  # usd_to_configured_rate intentionally omitted (zero value)
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err == nil {
		t.Fatalf("Load returned nil error for non-USD currency + zero FX; want failure (cfg=%+v)", cfg)
	}
	if !strings.Contains(err.Error(), "usd_to_configured_rate") {
		t.Errorf("Load error = %v; want a message mentioning usd_to_configured_rate", err)
	}
	if !strings.Contains(err.Error(), "cost-currency") {
		t.Errorf("Load error = %v; want the wrap phrase 'cost-currency' from Load's wrapper", err)
	}
}

// TestLoad_RejectsNonUSDCurrencyWithNegativeFXRate — same invariant
// from the other side: an explicit negative rate is just as broken as
// a zero rate (it inverts the sign of every cost). The validator
// rejects any non-positive rate; the loader surfaces that rejection.
func TestLoad_RejectsNonUSDCurrencyWithNegativeFXRate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `llm:
  current: claude-sonnet
  available:
    claude-sonnet:
      provider: claude
      model: claude-sonnet-4-20250514
  currency: EUR
  usd_to_configured_rate: -0.9
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(configPath); err == nil {
		t.Fatalf("Load returned nil error for non-USD currency + negative FX; want failure")
	}
}

// TestLoad_AcceptsNonUSDCurrencyWithPositiveFXRate — the happy path
// for the launch EUR configuration. A positive FX rate satisfies the
// validator and Load returns the config intact.
func TestLoad_AcceptsNonUSDCurrencyWithPositiveFXRate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `llm:
  current: claude-sonnet
  available:
    claude-sonnet:
      provider: claude
      model: claude-sonnet-4-20250514
  currency: EUR
  usd_to_configured_rate: 0.92
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error for non-USD currency + positive FX: %v", err)
	}
	if cfg.LLM.Currency != "EUR" {
		t.Errorf("LLM.Currency = %q, want EUR", cfg.LLM.Currency)
	}
	if cfg.LLM.USDToConfiguredRate != 0.92 {
		t.Errorf("LLM.USDToConfiguredRate = %g, want 0.92", cfg.LLM.USDToConfiguredRate)
	}
}

// TestLoad_AcceptsUSDCurrencyWithZeroFXRate — the validator must NOT
// require an FX rate when the configured currency IS USD (the rate is
// implicitly 1.0 in that case; see EstimateCostNano's zero-rate
// branch). This is the most common shape — a vanilla US-billed Joe
// with no `usd_to_configured_rate` line in the file — and it must
// load cleanly. Covers both the implicit case (no Currency line; the
// default applies) and any FX value, including zero.
func TestLoad_AcceptsUSDCurrencyWithZeroFXRate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configYAML := `llm:
  current: claude-sonnet
  available:
    claude-sonnet:
      provider: claude
      model: claude-sonnet-4-20250514
  currency: USD
  # usd_to_configured_rate intentionally omitted — ignored for USD.
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error for USD currency + zero FX: %v", err)
	}
	if cfg.LLM.Currency != "USD" {
		t.Errorf("LLM.Currency = %q, want USD", cfg.LLM.Currency)
	}
	if cfg.LLM.USDToConfiguredRate != 0 {
		t.Errorf("LLM.USDToConfiguredRate = %g, want 0", cfg.LLM.USDToConfiguredRate)
	}
}

// TestLoad_AcceptsDefaultsWhenNoFile — the no-file/defaults path was
// already exercised by TestLoad_NoFile; this is a regression test
// targeting the new validator hook specifically: defaultConfig sets
// Currency=USD, so the validator must NOT reject the defaults. If a
// future change ever flips defaultCurrency to a non-USD value
// without populating USDToConfiguredRate, this test will trip.
func TestLoad_AcceptsDefaultsWhenNoFile(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() with no file returned error: %v", err)
	}
	if cfg.LLM.Currency == "" {
		t.Errorf("defaults left Currency empty; defaultConfig must populate it so the validator's empty-treated-as-USD shim isn't load-bearing here")
	}
}
