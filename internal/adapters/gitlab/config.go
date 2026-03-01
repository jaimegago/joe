package gitlab

import (
	"encoding/json"
	"fmt"
)

// Config holds configuration for a GitLab source.
type Config struct {
	// BaseURL is the GitLab instance URL (default: https://gitlab.com).
	BaseURL       string `json:"base_url,omitempty"`
	Token         string `json:"token"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

// ParseConfig parses a raw JSON config into a GitLab Config.
func ParseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	if len(raw) == 0 {
		return cfg, fmt.Errorf("gitlab config is required")
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse gitlab config: %w", err)
	}
	if cfg.Token == "" {
		return Config{}, fmt.Errorf("gitlab config: token is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://gitlab.com"
	}
	return cfg, nil
}
