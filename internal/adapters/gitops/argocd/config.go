package argocd

import (
	"encoding/json"
	"fmt"
)

// Config holds Argo CD connection configuration.
type Config struct {
	URL         string `json:"url"`
	Token       string `json:"token"`
	InsecureTLS bool   `json:"insecure_tls"`
}

// ParseConfig parses raw JSON source config into a Config.
func ParseConfig(raw []byte) (Config, error) {
	var cfg Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("unmarshal config: %w", err)
		}
	}
	if cfg.URL == "" {
		return cfg, fmt.Errorf("url is required")
	}
	if cfg.Token == "" {
		return cfg, fmt.Errorf("token is required")
	}
	return cfg, nil
}
