package azure

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config holds Azure adapter configuration.
type Config struct {
	SubscriptionID string `yaml:"subscription_id" json:"subscription_id"`
	TenantID       string `yaml:"tenant_id" json:"tenant_id"`
	ClientID       string `yaml:"client_id" json:"client_id"`
	ClientSecret   string `yaml:"client_secret" json:"client_secret"`
	ResourceGroup  string `yaml:"resource_group" json:"resource_group"`
	Environment    string `yaml:"environment" json:"environment"`
}

// ParseConfig parses source config into Azure config.
func ParseConfig(sourceConfig map[string]any) (Config, error) {
	var cfg Config

	yamlData, err := yaml.Marshal(sourceConfig)
	if err != nil {
		return cfg, fmt.Errorf("marshal config to yaml: %w", err)
	}

	if err := yaml.Unmarshal(yamlData, &cfg); err != nil {
		return cfg, fmt.Errorf("unmarshal yaml to config: %w", err)
	}

	if cfg.SubscriptionID == "" {
		return cfg, fmt.Errorf("subscription_id is required")
	}

	return cfg, nil
}
