package mongodb

import (
	"encoding/json"
	"fmt"
)

// Config holds MongoDB adapter configuration.
type Config struct {
	URI        string `json:"uri"`      // Required: e.g. "mongodb://user:pass@host:27017"
	Database   string `json:"database"` // Default: "admin"
	TLSEnabled bool   `json:"tls_enabled"`
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

	if cfg.URI == "" {
		return cfg, fmt.Errorf("uri is required")
	}
	if cfg.Database == "" {
		cfg.Database = "admin"
	}

	return cfg, nil
}
