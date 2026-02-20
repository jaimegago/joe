package loki

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config holds Loki adapter configuration.
type Config struct {
	URL    string `yaml:"url" json:"url"`         // Required: "http://loki:3100"
	OrgID  string `yaml:"org_id" json:"org_id"`   // Optional: multi-tenancy X-Scope-OrgID
	APIKey string `yaml:"api_key" json:"api_key"` // Optional: Bearer token
}

// ParseConfig parses a source config map into a Config.
func ParseConfig(sourceConfig map[string]any) (Config, error) {
	var cfg Config

	yamlData, err := yaml.Marshal(sourceConfig)
	if err != nil {
		return cfg, fmt.Errorf("marshal config to yaml: %w", err)
	}

	if err := yaml.Unmarshal(yamlData, &cfg); err != nil {
		return cfg, fmt.Errorf("unmarshal yaml to config: %w", err)
	}

	if cfg.URL == "" {
		return cfg, fmt.Errorf("url is required")
	}

	return cfg, nil
}
