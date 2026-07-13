package helm

import (
	"encoding/json"
	"fmt"
)

// Config holds Helm adapter configuration.
type Config struct {
	// KubeconfigPath is the path to the kubeconfig file.
	// If empty, uses the in-cluster config or KUBECONFIG env var.
	KubeconfigPath string `json:"kubeconfig_path"`

	// Context is the kubeconfig context to use. Defaults to current context.
	Context string `json:"context"`
}

// ParseConfig parses raw JSON component config into a Config.
func ParseConfig(raw []byte) (Config, error) {
	var cfg Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("unmarshal config: %w", err)
		}
	}
	return cfg, nil
}
