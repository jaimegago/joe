package datadog

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config holds Datadog adapter configuration.
type Config struct {
	// Site is the Datadog region endpoint (e.g. "datadoghq.com", "datadoghq.eu", "us3.datadoghq.com").
	// Defaults to "datadoghq.com".
	Site   string `yaml:"site" json:"site"`
	APIKey string `yaml:"api_key" json:"api_key"` // Required: DD-API-KEY
	AppKey string `yaml:"app_key" json:"app_key"` // Required: DD-APPLICATION-KEY
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
	if cfg.AppKey == "" {
		return cfg, fmt.Errorf("app_key is required")
	}
	if cfg.Site == "" {
		cfg.Site = "datadoghq.com"
	}

	return cfg, nil
}

// BaseURL returns the full API base URL for the configured site.
func (c Config) BaseURL() string {
	return "https://api." + c.Site
}
