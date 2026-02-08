package k8s

import (
	"encoding/json"
	"fmt"
)

// Config holds the Kubernetes-specific source configuration.
type Config struct {
	Kubeconfig string `json:"kubeconfig,omitempty"` // path to kubeconfig file (empty = default)
	Context    string `json:"context,omitempty"`    // kubeconfig context name (empty = current)
	InCluster  bool   `json:"in_cluster,omitempty"` // use in-cluster service account
}

// ParseConfig extracts a K8s Config from raw JSON source config.
func ParseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse k8s config: %w", err)
	}
	return cfg, nil
}
