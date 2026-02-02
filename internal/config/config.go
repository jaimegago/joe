package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the Joe configuration
type Config struct {
	LLM           LLMConfig          `yaml:"llm"`
	Refresh       RefreshConfig      `yaml:"refresh"`
	Notifications NotificationConfig `yaml:"notifications"`
	Logging       LoggingConfig      `yaml:"logging"`
}

// LLMConfig configures the LLM provider
type LLMConfig struct {
	Provider string `yaml:"provider"` // "claude", "gemini"
	Model    string `yaml:"model"`
	APIKey   string `yaml:"-"` // Never serialize API keys
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

	// Expand home directory if path starts with ~
	if len(configPath) > 0 && configPath[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		configPath = filepath.Join(home, configPath[1:])
	}

	// Try to load from file
	if configPath != "" {
		if err := loadFromFile(cfg, configPath); err != nil {
			// If file doesn't exist, that's okay - use defaults
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to load config from %s: %w", configPath, err)
			}
		}
	}

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	// Compute derived fields
	cfg.Refresh.Interval = time.Duration(cfg.Refresh.IntervalMinutes) * time.Minute
	cfg.Refresh.LLMBudget.BatchTimeout = time.Duration(cfg.Refresh.LLMBudget.BatchTimeoutSec) * time.Second

	return cfg, nil
}

// defaultConfig returns a config with sensible defaults
func defaultConfig() *Config {
	return &Config{
		LLM: LLMConfig{
			Provider: "claude",
			Model:    "claude-sonnet-4-20250514",
		},
		Refresh: RefreshConfig{
			IntervalMinutes: 5,
			LLMBudget: LLMBudget{
				MaxCallsPerHour: 100,
				BatchThreshold:  10,
				BatchTimeoutSec: 30,
			},
		},
		Notifications: NotificationConfig{
			Desktop: ChannelConfig{
				Enabled:           false,
				PriorityThreshold: "medium",
			},
			Slack: ChannelConfig{
				Enabled:           false,
				PriorityThreshold: "high",
			},
			QuietHours: QuietHoursConfig{
				Enabled:  false,
				Start:    "22:00",
				End:      "08:00",
				Timezone: "Local",
			},
		},
		Logging: LoggingConfig{
			Level: "info",
			File:  "",
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
func applyEnvOverrides(cfg *Config) {
	// LLM provider can be overridden
	if provider := os.Getenv("JOE_LLM_PROVIDER"); provider != "" {
		cfg.LLM.Provider = provider
	}

	// LLM model can be overridden
	if model := os.Getenv("JOE_LLM_MODEL"); model != "" {
		cfg.LLM.Model = model
	}

	// API keys are always from environment, never from config file
	// This is handled separately in main.go for security
}

// Save saves the config to a YAML file
func Save(cfg *Config, path string) error {
	// Expand home directory if path starts with ~
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, path[1:])
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
