package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/api"
	"github.com/jaimegago/joe/internal/auth"
	"github.com/jaimegago/joe/internal/core"
)

// TestAuthConfig_PublicNoCredentialReflectsConfigured is the Stream H2
// follow-up acceptance: the public GET /api/v1/auth/config endpoint answers
// with the app-wide oidc_enabled flag for a caller carrying NO credential —
// no session cookie, no bearer key — and reflects the configured-vs-not state.
//
// The endpoint is wrapped in the SAME EdgeAuth chain main.go builds so the
// test proves the real pre-auth reachability, not just the handler in
// isolation: EdgeAuth's /api/v1/auth/ public bypass lets the unauthenticated
// request through, while a protected path (/api/v1/me) 401s for the same
// credential-less caller. That contrast is exactly why the signal had to move
// off /me: the cold logged-out shell can read /auth/config but never /me.
func TestAuthConfig_PublicNoCredentialReflectsConfigured(t *testing.T) {
	for _, tc := range []struct {
		name        string
		oidcEnabled bool
	}{
		{name: "configured", oidcEnabled: true},
		{name: "not-configured", oidcEnabled: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := api.New(&core.Services{OIDCEnabled: tc.oidcEnabled}, nil)
			mux := http.NewServeMux()
			srv.RegisterRoutes(mux)

			// Wrap in EdgeAuth exactly as the production chain does. When OIDC
			// is configured auth is enforced (protected paths 401 without a
			// credential); the /api/v1/auth/ prefix is public regardless.
			handler := auth.EdgeAuth(auth.EdgeConfig{OIDCConfigured: tc.oidcEnabled})(mux)

			// auth-config: reachable with no credential, reflects the flag.
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/config", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("GET /api/v1/auth/config without credential = %d, want 200: body=%s", w.Code, w.Body.String())
			}
			var resp struct {
				OIDCEnabled bool `json:"oidc_enabled"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode auth-config body: %v: body=%s", err, w.Body.String())
			}
			if resp.OIDCEnabled != tc.oidcEnabled {
				t.Errorf("auth-config oidc_enabled = %v, want %v", resp.OIDCEnabled, tc.oidcEnabled)
			}

			// When auth is enabled, the protected /me path 401s for the same
			// credential-less caller — proving auth-config carries the signal
			// pre-auth precisely where /me cannot.
			if tc.oidcEnabled {
				meReq := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
				meW := httptest.NewRecorder()
				handler.ServeHTTP(meW, meReq)
				if meW.Code != http.StatusUnauthorized {
					t.Errorf("GET /api/v1/me without credential = %d, want 401 (the path auth-config replaces pre-auth)", meW.Code)
				}
			}
		})
	}
}
