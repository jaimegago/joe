package newrelic

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config holds New Relic adapter configuration.
type Config struct {
	APIKey    string `yaml:"api_key" json:"api_key"`       // Required: User API key (NRAK-...)
	AccountID int    `yaml:"account_id" json:"account_id"` // Required: New Relic account ID
	// Region is "US" (default) or "EU". Controls which NerdGraph endpoint is used.
	Region string `yaml:"region" json:"region"`
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

	if cfg.APIKey == "" {
		return cfg, fmt.Errorf("api_key is required")
	}
	if cfg.AccountID == 0 {
		return cfg, fmt.Errorf("account_id is required")
	}
	if cfg.Region == "" {
		cfg.Region = "US"
	}

	return cfg, nil
}

// NerdGraphURL returns the correct NerdGraph endpoint for the configured region.
func (c Config) NerdGraphURL() string {
	if c.Region == "EU" {
		return "https://api.eu.newrelic.com/graphql"
	}
	return "https://api.newrelic.com/graphql"
}
