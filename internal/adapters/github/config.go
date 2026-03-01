package github

import (
	"encoding/json"
	"fmt"
)

// Config holds configuration for a GitHub source.
type Config struct {
	// BaseURL is the GitHub API base URL (default: https://api.github.com).
	// Set to a custom URL for GitHub Enterprise Server.
	BaseURL       string `json:"base_url,omitempty"`
	Token         string `json:"token"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

// ParseConfig parses a raw JSON config into a GitHub Config.
func ParseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	if len(raw) == 0 {
		return cfg, fmt.Errorf("github config is required")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse github config: %w", err)
	}
	if cfg.Token == "" {
		return Config{}, fmt.Errorf("github config: token is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.github.com"
	}
	return cfg, nil
}
