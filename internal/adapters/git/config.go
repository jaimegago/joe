package git

import (
	"encoding/json"
	"fmt"
)

// Config holds configuration for a Git source.
type Config struct {
	URL        string `json:"url"`
	Branch     string `json:"branch,omitempty"`
	AuthType   string `json:"auth_type,omitempty"`
	SSHKeyPath string `json:"ssh_key_path,omitempty"`
	HTTPToken  string `json:"http_token,omitempty"`
}

// ParseConfig parses a raw JSON config into a Git Config.
func ParseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse git config: %w", err)
	}
	if cfg.URL == "" {
		return Config{}, fmt.Errorf("git config: url is required")
	}
	if cfg.Branch == "" {
		cfg.Branch = "HEAD"
	}
	if cfg.AuthType == "" {
		cfg.AuthType = "none"
	}
	return cfg, nil
}
