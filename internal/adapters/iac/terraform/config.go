package terraform

import (
	"encoding/json"
	"fmt"
)

// Config holds Terraform adapter configuration.
type Config struct {
	// StatePath is the path to a .tfstate file or a directory containing
	// terraform.tfstate. Required.
	StatePath string `json:"state_path"`
}

// ParseConfig parses raw JSON source config into a Config.
func ParseConfig(raw []byte) (Config, error) {
	var cfg Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("unmarshal config: %w", err)
		}
	}
	if cfg.StatePath == "" {
		return cfg, fmt.Errorf("state_path is required")
	}
	return cfg, nil
}
