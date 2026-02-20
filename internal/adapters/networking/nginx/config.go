package nginx

import (
	"encoding/json"
	"fmt"
)

// Config holds the NGINX Ingress adapter configuration.
type Config struct {
	// KubeconfigPath is the path to the kubeconfig file.
	// If empty, in-cluster config is used.
	KubeconfigPath string `json:"kubeconfig_path"`
	// Context is the kubeconfig context to use. Empty means current context.
	Context string `json:"context"`
	// StatusURL is the optional URL of the NGINX status endpoint,
	// e.g. "http://nginx-ingress-controller.ingress-nginx:8080".
	// Omit to disable nginx_status queries.
	StatusURL string `json:"status_url"`
	// StatusPath is the path of the NGINX status endpoint (default: /nginx_status).
	StatusPath string `json:"status_path"`
}

// ParseConfig parses a JSON config blob into Config.
func ParseConfig(raw []byte) (Config, error) {
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse nginx config: %w", err)
	}
	if cfg.StatusPath == "" {
		cfg.StatusPath = "/nginx_status"
	}
	return cfg, nil
}
