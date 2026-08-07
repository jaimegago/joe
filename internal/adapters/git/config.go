package git

import (
	"encoding/json"
	"fmt"
)

// Config holds configuration for a Git source.
//
// It carries NO authentication material. A git component's credential enters at
// promotion and only at promotion (D-0150): the component is armed either with a
// static HTTPS-token reference, which Connect resolves through the
// credential-provider seam at use time, or with the explicit no-credential kind,
// which clones anonymously. The former inline `http_token` / `ssh_key_path` /
// `auth_type` fields are gone; the first two are refused at registration by the
// shared credential-field denylist (internal/credential, retiredInlineAuthFields).
type Config struct {
	URL    string `json:"url"`
	Branch string `json:"branch,omitempty"`
	// ProviderComponentID optionally names the component id of the github or
	// gitlab component that hosts this repository. It is a DISCOVERY hint only:
	// the graph refresher derives one deterministic edge from it, and a dangling
	// reference is legal and simply yields no edge. It confers no RBAC, zone, or
	// governance meaning, and Connect never reads it.
	ProviderComponentID string `json:"provider_component_id,omitempty"`
}

// ParseConfig parses a raw JSON config into a Git Config.
func ParseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse git config: %w", err)
	}
	if cfg.URL == "" {
		return Config{}, fmt.Errorf("git config: url is required")
	}
	if cfg.Branch == "" {
		cfg.Branch = "HEAD"
	}
	return cfg, nil
}
