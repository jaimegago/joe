package oci

import (
	"encoding/json"
	"fmt"
)

// Config holds OCI-compatible registry adapter configuration.
// Supports DockerHub, GHCR, Harbor, Quay, and any OCI Distribution Spec v2 registry.
type Config struct {
	// RegistryURL is the base URL of the registry, e.g. "https://registry-1.docker.io".
	RegistryURL string `json:"registry_url"`

	// Username for basic authentication.
	Username string `json:"username"`

	// Password or token for authentication.
	Password string `json:"password"`

	// SkipTLSVerify disables TLS certificate verification (for self-hosted registries).
	SkipTLSVerify bool `json:"skip_tls_verify"`
}

// ParseConfig parses raw JSON source config into a Config.
func ParseConfig(raw []byte) (Config, error) {
	var cfg Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("unmarshal oci config: %w", err)
		}
	}
	if cfg.RegistryURL == "" {
		return cfg, fmt.Errorf("registry_url is required")
	}
	return cfg, nil
}
