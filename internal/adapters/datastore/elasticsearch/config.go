package elasticsearch

import (
	"encoding/json"
	"fmt"
)

// Config holds Elasticsearch adapter configuration.
type Config struct {
	URL      string `json:"url"`      // Required: "http://elasticsearch:9200"
	Username string `json:"username"` // Optional: basic auth username
	Password string `json:"password"` // Optional: basic auth password
	APIKey   string `json:"api_key"`  // Optional: Bearer API key
}

// ParseConfig parses a source config map into a Config.
func ParseConfig(raw map[string]any) (Config, error) {
	var cfg Config

	data, err := json.Marshal(raw)
	if err != nil {
		return cfg, fmt.Errorf("marshal config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.URL == "" {
		return cfg, fmt.Errorf("url is required")
	}

	return cfg, nil
}
