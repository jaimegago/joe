package falco

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config holds Falco adapter configuration.
type Config struct {
	URL    string `yaml:"url" json:"url"`         // Required: Falco Sidekick UI backend URL (e.g. "http://falcosidekick:2802")
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
