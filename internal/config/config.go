package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jaimegago/joe/internal/paths"
	"gopkg.in/yaml.v3"
)

// Config represents the Joe configuration
type Config struct {
	LLM           LLMConfig          `yaml:"llm"`
	Server        ServerConfig       `yaml:"server"`
	Refresh       RefreshConfig      `yaml:"refresh"`
	Notifications NotificationConfig `yaml:"notifications"`
	Logging       LoggingConfig      `yaml:"logging"`
	Knowledge     KnowledgeConfig    `yaml:"knowledge"`
}

// KnowledgeConfig configures the Phase 7 Knowledge Store.
type KnowledgeConfig struct {
	// EmbeddingModel is the model key (from LLM.Available) used for embeddings.
	// Defaults to the LLM.Current model when empty.
	EmbeddingModel string `yaml:"embedding_model"`
	// SemanticTopK is the default number of results returned by semantic search.
	SemanticTopK int `yaml:"semantic_top_k"`
	// DerivedMinConfidence is the minimum confidence for Tier 3 (derived) entries
	// to appear in semantic search results. 0 means include all.
	DerivedMinConfidence float32 `yaml:"derived_min_confidence"`
	// SyncEnabled controls whether background sync of external knowledge sources runs.
	SyncEnabled bool `yaml:"sync_enabled"`
}

// ServerConfig holds joecored server settings
type ServerConfig struct {
	Address        string  `yaml:"address"`          // e.g., ":7777" or "localhost:7777"
	APIKey         string  `yaml:"api_key"`          // Bearer token for API authentication (optional)
	TLSCertFile    string  `yaml:"tls_cert_file"`    // Path to TLS certificate (enables HTTPS on server)
	TLSKeyFile     string  `yaml:"tls_key_file"`     // Path to TLS private key (enables HTTPS on server)
	TLSEnabled     bool    `yaml:"tls_enabled"`      // joe client: connect over HTTPS (must match server TLS setting)
	RateLimitRPS   float64 `yaml:"rate_limit_rps"`   // Requests per second per IP (0 = disabled)
	RateLimitBurst int     `yaml:"rate_limit_burst"` // Burst size for rate limiter (default 10)
}

// TLSConfigured reports whether TLS has been configured for the server side.
func (s *ServerConfig) TLSConfigured() bool {
	return s.TLSCertFile != "" && s.TLSKeyFile != ""
}

// LLMConfig configures LLM providers with support for multiple models
type LLMConfig struct {
	Current   string                 `yaml:"current"`   // Key into Available for the active model
	Available map[string]ModelConfig `yaml:"available"` // All configured models
}

// ModelConfig describes a single LLM model
type ModelConfig struct {
	Provider string `yaml:"provider"` // "claude", "gemini"
	Model    string `yaml:"model"`    // e.g. "claude-sonnet-4-20250514"
}

// CurrentModel returns the ModelConfig for the currently selected model
func (c *LLMConfig) CurrentModel() (ModelConfig, error) {
	mc, ok := c.Available[c.Current]
	if !ok {
		return ModelConfig{}, fmt.Errorf("current model %q not found in available models", c.Current)
	}
	return mc, nil
}

// ModelNames returns the sorted list of available model keys
func (c *LLMConfig) ModelNames() []string {
	names := make([]string, 0, len(c.Available))
	for k := range c.Available {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// RefreshConfig configures background refresh
type RefreshConfig struct {
	IntervalMinutes int           `yaml:"interval_minutes"`
	Interval        time.Duration `yaml:"-"` // Computed from IntervalMinutes
	LLMBudget       LLMBudget     `yaml:"llm_budget"`
}

// LLMBudget limits LLM usage during background refresh
type LLMBudget struct {
	MaxCallsPerHour int           `yaml:"max_calls_per_hour"`
	BatchThreshold  int           `yaml:"batch_threshold"`
	BatchTimeoutSec int           `yaml:"batch_timeout_sec"`
	BatchTimeout    time.Duration `yaml:"-"` // Computed from BatchTimeoutSec
}

// NotificationConfig configures notifications
type NotificationConfig struct {
	Desktop    ChannelConfig    `yaml:"desktop"`
	Slack      ChannelConfig    `yaml:"slack"`
	QuietHours QuietHoursConfig `yaml:"quiet_hours"`
}

// ChannelConfig configures a notification channel
type ChannelConfig struct {
	Enabled           bool   `yaml:"enabled"`
	PriorityThreshold string `yaml:"priority_threshold"` // "low", "medium", "high", "urgent"
}

// QuietHoursConfig configures quiet hours
type QuietHoursConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Start    string `yaml:"start"`
	End      string `yaml:"end"`
	Timezone string `yaml:"timezone"`
}

// LoggingConfig configures logging
type LoggingConfig struct {
	Level string `yaml:"level"` // "debug", "info", "warn", "error"
	File  string `yaml:"file"`
}

// Load loads configuration from the specified file path
// Falls back to defaults if file doesn't exist
// Environment variables override config file values
func Load(configPath string) (*Config, error) {
	// Start with defaults
	cfg := defaultConfig()
	slog.Debug("config: initialized with defaults")

	// Expand home directory if path starts with ~
	if len(configPath) > 0 && configPath[0] == '~' {
		expanded, err := paths.ExpandPath(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to expand config path: %w", err)
		}
		configPath = expanded
	}

	// Track config source
	configSource := "defaults"

	// Try to load from file
	if configPath != "" {
		if err := loadFromFile(cfg, configPath); err != nil {
			// If file doesn't exist, that's okay - use defaults
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to load config from %s: %w", configPath, err)
			}
			slog.Info("no config file detected, using defaults", "path", configPath)
			slog.Debug("config: file not found, using defaults", "path", configPath)
		} else {
			configSource = configPath
			slog.Info("loaded config from file", "path", configPath)
			slog.Debug("config: loaded from file", "path", configPath)
		}
	} else {
		slog.Info("no config path specified, using defaults")
		slog.Debug("config: no path specified, using defaults")
	}

	// Apply environment variable overrides
	envOverrides := applyEnvOverrides(cfg)
	if len(envOverrides) > 0 {
		slog.Debug("config: applied environment overrides", "vars", envOverrides)
	}

	// Compute derived fields
	cfg.Refresh.Interval = time.Duration(cfg.Refresh.IntervalMinutes) * time.Minute
	cfg.Refresh.LLMBudget.BatchTimeout = time.Duration(cfg.Refresh.LLMBudget.BatchTimeoutSec) * time.Second

	// Log final configuration
	slog.Debug("config: loaded",
		"source", configSource,
		"server.address", cfg.Server.Address,
		"llm.current", cfg.LLM.Current,
		"llm.available_models", len(cfg.LLM.Available),
		"refresh.interval_minutes", cfg.Refresh.IntervalMinutes,
		"logging.level", cfg.Logging.Level,
	)

	return cfg, nil
}

// defaultConfig returns a config with sensible defaults
func defaultConfig() *Config {
	return &Config{
		LLM: LLMConfig{
			Current: defaultLLMCurrent,
			Available: map[string]ModelConfig{
				defaultLLMCurrent: {Provider: providerClaude, Model: defaultLLMModel},
			},
		},
		Server: ServerConfig{
			Address: defaultServerAddress,
		},
		Refresh: RefreshConfig{
			IntervalMinutes: defaultRefreshIntervalMinutes,
			LLMBudget: LLMBudget{
				MaxCallsPerHour: defaultMaxCallsPerHour,
				BatchThreshold:  defaultBatchThreshold,
				BatchTimeoutSec: defaultBatchTimeoutSec,
			},
		},
		Notifications: NotificationConfig{
			Desktop: ChannelConfig{
				Enabled:           false,
				PriorityThreshold: defaultDesktopThreshold,
			},
			Slack: ChannelConfig{
				Enabled:           false,
				PriorityThreshold: defaultSlackThreshold,
			},
			QuietHours: QuietHoursConfig{
				Enabled:  false,
				Start:    defaultQuietStart,
				End:      defaultQuietEnd,
				Timezone: defaultQuietTimezone,
			},
		},
		Logging: LoggingConfig{
			Level: "info",
			File:  "",
		},
		Knowledge: KnowledgeConfig{
			SemanticTopK:         defaultKnowledgeSemanticTopK,
			DerivedMinConfidence: defaultKnowledgeMinConfidence,
			SyncEnabled:          false,
		},
	}
}

// loadFromFile loads config from a YAML file
func loadFromFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	return nil
}

// applyEnvOverrides applies environment variable overrides
// Supported environment variables:
//   - JOE_LLM_PROVIDER: override LLM provider
//   - JOE_LLM_MODEL: override LLM model
//   - JOE_LOG_LEVEL: override logging level (debug, info, warn, error)
//   - JOE_SERVER_ADDRESS: override server address
//
// Returns a slice of environment variable names that were applied.
func applyEnvOverrides(cfg *Config) []string {
	var overrides []string

	// LLM overrides
	provider := os.Getenv("JOE_LLM_PROVIDER")
	model := os.Getenv("JOE_LLM_MODEL")

	if provider != "" || model != "" {
		// Get the current model entry (or create one if missing)
		current := cfg.LLM.Current
		mc, ok := cfg.LLM.Available[current]
		if !ok {
			mc = ModelConfig{}
		}

		if provider != "" {
			mc.Provider = provider
			overrides = append(overrides, "JOE_LLM_PROVIDER")
		}
		if model != "" {
			mc.Model = model
			overrides = append(overrides, "JOE_LLM_MODEL")
		}

		if cfg.LLM.Available == nil {
			cfg.LLM.Available = make(map[string]ModelConfig)
		}
		cfg.LLM.Available[current] = mc
	}

	// Logging level override
	if logLevel := os.Getenv("JOE_LOG_LEVEL"); logLevel != "" {
		cfg.Logging.Level = logLevel
		overrides = append(overrides, "JOE_LOG_LEVEL")
	}

	// Server address override
	if serverAddr := os.Getenv("JOE_SERVER_ADDRESS"); serverAddr != "" {
		cfg.Server.Address = serverAddr
		overrides = append(overrides, "JOE_SERVER_ADDRESS")
	}

	// API key override
	if apiKey := os.Getenv("JOE_API_KEY"); apiKey != "" {
		cfg.Server.APIKey = apiKey
		overrides = append(overrides, "JOE_API_KEY")
	}

	return overrides
}

// Save saves the config to a YAML file
func Save(cfg *Config, path string) error {
	// Expand home directory if path starts with ~
	if len(path) > 0 && path[0] == '~' {
		expanded, err := paths.ExpandPath(path)
		if err != nil {
			return fmt.Errorf("failed to expand config path: %w", err)
		}
		path = expanded
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal to YAML with indentation
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
