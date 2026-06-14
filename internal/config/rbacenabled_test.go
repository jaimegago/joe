package config

import "testing"

// TestRBACEnabled is the engine-enable / refuse-to-start predicate matrix
// (JOE-IDBOOT). RBACEnabled is the SINGLE predicate both engine-construction
// sites and the boot guard share, so this table is the canonical statement of
// when Joe is governed. The two cases marked NON-NEGOTIABLE encode the whole
// decision: a half-configured OIDC block must NOT count as identity, and a
// complete-but-unreachable issuer MUST count (config completeness is the test,
// never IdP liveness — the predicate performs no network probe).
func TestRBACEnabled(t *testing.T) {
	sa := []ServiceAccount{{Name: "ci", Key: "k"}}
	completeOIDC := OIDCConfig{
		Issuer:      "https://idp.example.com",
		ClientID:    "client-123",
		RedirectURL: "https://joe.example.com/api/v1/auth/callback",
	}
	// An issuer that resolves nowhere: still complete config, so still governed.
	unreachableOIDC := OIDCConfig{
		Issuer:      "https://idp.invalid.nonexistent.example",
		ClientID:    "client-123",
		RedirectURL: "https://joe.example.com/api/v1/auth/callback",
	}
	partialOIDC := OIDCConfig{Issuer: "https://idp.example.com"} // client_id + redirect_url empty

	tests := []struct {
		name string
		cfg  *Config
		want bool
	}{
		{
			name: "no identity (no SA, no OIDC) -> refuse",
			cfg:  &Config{},
			want: false,
		},
		{
			name: "service-account only -> start",
			cfg:  &Config{Server: ServerConfig{ServiceAccounts: sa}},
			want: true,
		},
		{
			name: "complete-OIDC only -> start",
			cfg:  &Config{Auth: AuthConfig{OIDC: completeOIDC}},
			want: true,
		},
		{
			// NON-NEGOTIABLE: half-configured OIDC is NOT identity. This closes
			// the gap where issuer-only config would have looked configured.
			name: "partial-OIDC (issuer only) -> REFUSE",
			cfg:  &Config{Auth: AuthConfig{OIDC: partialOIDC}},
			want: false,
		},
		{
			// NON-NEGOTIABLE: a complete config whose IdP is down still starts,
			// governed. The predicate is pure-config and never probes, so an IdP
			// outage cannot be converted into a Joe outage.
			name: "complete-but-unreachable OIDC -> START governed",
			cfg:  &Config{Auth: AuthConfig{OIDC: unreachableOIDC}},
			want: true,
		},
		{
			name: "nil receiver -> false (nil-safe)",
			cfg:  nil,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.RBACEnabled(); got != tc.want {
				t.Errorf("RBACEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
