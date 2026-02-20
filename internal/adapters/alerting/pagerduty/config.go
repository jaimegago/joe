package pagerduty

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

const defaultAPIURL = "https://api.pagerduty.com"

// Config holds PagerDuty adapter configuration.
type Config struct {
	APIKey string `yaml:"api_key" json:"api_key"` // Required: REST API v2 key
	APIURL string `yaml:"api_url" json:"api_url"` // Optional: override base URL (for testing)
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

	if cfg.APIURL == "" {
		cfg.APIURL = defaultAPIURL
	}

	return cfg, nil
}
