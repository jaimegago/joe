package grafana

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config holds Grafana adapter configuration.
type Config struct {
	URL    string `yaml:"url" json:"url"`         // Required: "http://grafana:3000"
	APIKey string `yaml:"api_key" json:"api_key"` // Required: service account token or API key
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

	if cfg.APIKey == "" {
		return cfg, fmt.Errorf("api_key is required")
	}

	return cfg, nil
}
