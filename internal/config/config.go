package config

import "time"

// Config represents the Joe configuration
type Config struct {
	LLM           LLMConfig
	Refresh       RefreshConfig
	Notifications NotificationConfig
	Logging       LoggingConfig
}

// LLMConfig configures the LLM provider
type LLMConfig struct {
	Provider string // "claude", "openai", "ollama"
	Model    string
	APIKey   string // From env variable
}

// RefreshConfig configures background refresh
type RefreshConfig struct {
	Interval  time.Duration
	LLMBudget LLMBudget
}

// LLMBudget limits LLM usage during background refresh
type LLMBudget struct {
	MaxCallsPerHour int
	BatchThreshold  int
	BatchTimeout    time.Duration
}

// NotificationConfig configures notifications
type NotificationConfig struct {
	Desktop    ChannelConfig
	Slack      ChannelConfig
	QuietHours QuietHoursConfig
}

// ChannelConfig configures a notification channel
type ChannelConfig struct {
	Enabled           bool
	PriorityThreshold string // "low", "medium", "high", "urgent"
}

// QuietHoursConfig configures quiet hours
type QuietHoursConfig struct {
	Enabled  bool
	Start    string
	End      string
	Timezone string
}

// LoggingConfig configures logging
type LoggingConfig struct {
	Level string // "debug", "info", "warn", "error"
	File  string
}

// Load loads configuration from file and environment
func Load() (*Config, error) {
	// TODO: Implement config loading
	// For now, return a default config
	return &Config{
		LLM: LLMConfig{
			Provider: "claude",
			Model:    "claude-sonnet-4-20250514",
		},
		Refresh: RefreshConfig{
			Interval: 5 * time.Minute,
		},
		Logging: LoggingConfig{
			Level: "info",
		},
	}, nil
}
