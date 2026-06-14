package main

import (
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/config"
)

// TestRequireIdentityConfigured exercises the boot-time refuse-to-start guard
// (JOE-IDBOOT) directly. The guard wraps config.Config.RBACEnabled, so it
// inherits the same five-case decision matrix: it returns the rich remediation
// error exactly when Joe would otherwise boot ungoverned (nil policy engine),
// and nil when an identity source is configured. The two NON-NEGOTIABLE rows
// prove the half-configured-OIDC gap is closed and that an IdP outage is not
// converted into a Joe outage.
func TestRequireIdentityConfigured(t *testing.T) {
	sa := []config.ServiceAccount{{Name: "ci", Key: "k"}}
	completeOIDC := config.OIDCConfig{
		Issuer:      "https://idp.example.com",
		ClientID:    "client-123",
		RedirectURL: "https://joe.example.com/api/v1/auth/callback",
	}
	unreachableOIDC := config.OIDCConfig{
		Issuer:      "https://idp.invalid.nonexistent.example",
		ClientID:    "client-123",
		RedirectURL: "https://joe.example.com/api/v1/auth/callback",
	}
	partialOIDC := config.OIDCConfig{Issuer: "https://idp.example.com"}

	tests := []struct {
		name       string
		cfg        *config.Config
		wantRefuse bool
	}{
		{
			name:       "no identity -> refuse to start",
			cfg:        &config.Config{},
			wantRefuse: true,
		},
		{
			name:       "service-account only -> start",
			cfg:        &config.Config{Server: config.ServerConfig{ServiceAccounts: sa}},
			wantRefuse: false,
		},
		{
			name:       "complete-OIDC only -> start",
			cfg:        &config.Config{Auth: config.AuthConfig{OIDC: completeOIDC}},
			wantRefuse: false,
		},
		{
			// NON-NEGOTIABLE: partial OIDC must hit the guard and refuse.
			name:       "partial-OIDC (issuer only) -> REFUSE",
			cfg:        &config.Config{Auth: config.AuthConfig{OIDC: partialOIDC}},
			wantRefuse: true,
		},
		{
			// NON-NEGOTIABLE: complete config, unreachable IdP -> must NOT
			// fail-start. The guard is pure-config; it never probes the issuer.
			name:       "complete-but-unreachable OIDC -> START governed",
			cfg:        &config.Config{Auth: config.AuthConfig{OIDC: unreachableOIDC}},
			wantRefuse: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := requireIdentityConfigured(tc.cfg)
			gotRefuse := err != nil
			if gotRefuse != tc.wantRefuse {
				t.Fatalf("requireIdentityConfigured() err=%v, wantRefuse=%v", err, tc.wantRefuse)
			}
			if !gotRefuse {
				return
			}
			// The refusal must carry the operator-actionable remediation that
			// guides completing all three OIDC fields OR adding a service
			// account — not a bare sentinel.
			msg := err.Error()
			for _, want := range []string{"ungoverned", "auth.oidc.issuer", "auth.oidc.client_id", "auth.oidc.redirect_url", "server.service_accounts", "restart"} {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal message missing %q; got:\n%s", want, msg)
				}
			}
		})
	}
}
