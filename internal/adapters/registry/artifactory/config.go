package artifactory

import (
	"encoding/json"
	"fmt"
)

// Config holds JFrog Artifactory adapter configuration.
type Config struct {
	// BaseURL is the Artifactory base URL, e.g. "https://company.jfrog.io/artifactory".
	BaseURL string `json:"base_url"`

	// Username for authentication.
	Username string `json:"username"`

	// APIKey or access token for authentication.
	APIKey string `json:"api_key"`

	// Repositories is an optional list of repository keys to include.
	// When empty, all local Docker and Helm repositories are included.
	Repositories []string `json:"repositories"`
}

// ParseConfig parses raw JSON source config into a Config.
func ParseConfig(raw []byte) (Config, error) {
	var cfg Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("unmarshal artifactory config: %w", err)
		}
	}
	if cfg.BaseURL == "" {
		return cfg, fmt.Errorf("base_url is required")
	}
	return cfg, nil
}
