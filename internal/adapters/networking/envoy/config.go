package envoy

import (
	"encoding/json"
	"fmt"
)

// Config holds the Envoy admin API adapter configuration.
type Config struct {
	// URL is the base URL of the Envoy admin API,
	// e.g. "http://envoy-admin:9901".
	URL string `json:"url"`
	// TimeoutMS is the HTTP request timeout in milliseconds (default 10000).
	TimeoutMS int `json:"timeout_ms"`
}

// ParseConfig parses a JSON config blob into Config.
func ParseConfig(raw []byte) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse envoy config: %w", err)
	}
	if cfg.URL == "" {
		return Config{}, fmt.Errorf("envoy config: url is required")
	}
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = 10000
	}
	return cfg, nil
}
