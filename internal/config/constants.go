package config

const (
	// Default server configuration.
	defaultServerAddress = "localhost:7777"

	// Default LLM configuration.
	defaultLLMCurrent = "claude-sonnet"

	// Default refresh intervals.
	defaultRefreshIntervalMinutes = 5
	defaultMaxCallsPerHour        = 100
	defaultBatchThreshold         = 10
	defaultBatchTimeoutSec        = 30

	// Default notification thresholds.
	defaultDesktopThreshold = "medium"
	defaultSlackThreshold   = "high"

	// Default quiet hours.
	defaultQuietStart    = "22:00"
	defaultQuietEnd      = "08:00"
	defaultQuietTimezone = "Local"

	// Provider names.
	providerClaude = "claude"
	providerGemini = "gemini"
)
