package config

const (
	// Default server configuration.
	defaultServerAddress = "localhost:7777"

	// Default LLM configuration.
	defaultLLMCurrent = "claude-sonnet"
	defaultLLMModel   = "claude-sonnet-4-20250514"

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

	// Default knowledge configuration.
	defaultKnowledgeSemanticTopK  = 5
	defaultKnowledgeMinConfidence = float32(0.0)
)
