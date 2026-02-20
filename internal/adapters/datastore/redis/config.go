package redis

import (
	"encoding/json"
	"fmt"
)

// Config holds Redis adapter configuration.
type Config struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Password   string `json:"password"`
	DB         int    `json:"db"`
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

	if cfg.Host == "" {
		return cfg, fmt.Errorf("host is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 6379
	}

	return cfg, nil
}
