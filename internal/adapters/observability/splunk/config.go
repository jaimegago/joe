package splunk

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config holds Splunk adapter configuration.
type Config struct {
	URL   string `yaml:"url" json:"url"`     // Required: Splunk server URL, e.g. "https://splunk.corp.example.com:8089"
	Token string `yaml:"token" json:"token"` // Required: Bearer token (Splunk HEC or API token)
	Index string `yaml:"index" json:"index"` // Optional: default index to search
}

// ParseConfig parses a component config map into a Config.
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
	if cfg.Token == "" {
		return cfg, fmt.Errorf("token is required")
	}

	return cfg, nil
}
