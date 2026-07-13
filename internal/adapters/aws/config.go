package aws

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config holds AWS adapter configuration
type Config struct {
	Region    string `yaml:"region" json:"region"`         // AWS region (e.g., "us-west-2")
	Profile   string `yaml:"profile" json:"profile"`       // AWS profile name (optional)
	AccessKey string `yaml:"access_key" json:"access_key"` // AWS access key (optional, prefer IAM roles)
	SecretKey string `yaml:"secret_key" json:"secret_key"` // AWS secret key (optional, prefer IAM roles)
	RoleARN   string `yaml:"role_arn" json:"role_arn"`     // IAM role ARN to assume (optional)
}

// ParseConfig parses component config into AWS config
func ParseConfig(sourceConfig map[string]any) (Config, error) {
	var cfg Config

	// Convert map to YAML bytes and back to struct
	yamlData, err := yaml.Marshal(sourceConfig)
	if err != nil {
		return cfg, fmt.Errorf("marshal config to yaml: %w", err)
	}

	err = yaml.Unmarshal(yamlData, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("unmarshal yaml to config: %w", err)
	}

	// Validate required fields
	if cfg.Region == "" {
		return cfg, fmt.Errorf("region is required")
	}

	return cfg, nil
}
