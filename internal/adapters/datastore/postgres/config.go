package postgres

import (
	"encoding/json"
	"fmt"
)

// Config holds PostgreSQL adapter configuration.
type Config struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"ssl_mode"` // disable, require, verify-full
}

// ParseConfig parses a component config map into a Config.
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
	if cfg.User == "" {
		return cfg, fmt.Errorf("user is required")
	}
	if cfg.Database == "" {
		return cfg, fmt.Errorf("database is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 5432
	}
	if cfg.SSLMode == "" {
		cfg.SSLMode = "disable"
	}

	return cfg, nil
}
