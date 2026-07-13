package ecr

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config holds AWS ECR adapter configuration.
type Config struct {
	Region     string `yaml:"region" json:"region"`           // AWS region, e.g. "us-east-1"
	Profile    string `yaml:"profile" json:"profile"`         // AWS named profile (optional)
	AccessKey  string `yaml:"access_key" json:"access_key"`   // Static access key (prefer IAM roles)
	SecretKey  string `yaml:"secret_key" json:"secret_key"`   // Static secret key (prefer IAM roles)
	RoleARN    string `yaml:"role_arn" json:"role_arn"`       // IAM role to assume (optional)
	RegistryID string `yaml:"registry_id" json:"registry_id"` // AWS account ID; defaults to caller account
}

// ParseConfig parses raw component config (map[string]any) into a Config.
// Mirrors the pattern used by the AWS adapter in internal/adapters/aws/config.go.
func ParseConfig(sourceConfig map[string]any) (Config, error) {
	var cfg Config

	yamlData, err := yaml.Marshal(sourceConfig)
	if err != nil {
		return cfg, fmt.Errorf("marshal ecr config to yaml: %w", err)
	}
	if err := yaml.Unmarshal(yamlData, &cfg); err != nil {
		return cfg, fmt.Errorf("unmarshal ecr config: %w", err)
	}
	if cfg.Region == "" {
		return cfg, fmt.Errorf("region is required")
	}
	return cfg, nil
}
