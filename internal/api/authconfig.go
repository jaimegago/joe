package api

import (
	"fmt"
	"net/http"
)

// Stream H2 follow-up — public auth-config endpoint.
//
// The Web UI's logged-out shell must decide whether to offer the OIDC login
// button BEFORE any credential exists. The oidc_enabled signal is also echoed
// on GET /api/v1/me, but /me sits behind EdgeAuth and 401s for a logged-out
// caller — so the cold login shell could never read it there, and the button
// would never appear. This endpoint carries the same signal pre-auth.
//
// It lives under the /api/v1/auth/ prefix, which EdgeAuth treats as public
// (auth.defaultPublicPrefix), so it is reachable with no session cookie and no
// bearer key. It reads no credential. The value is the SAME app-wide flag the
// rest of the server uses — services.OIDCEnabled, sourced once from
// cfg.Auth.OIDC.Configured() at the main.go build site — so it is present and
// correct whether OIDC is configured or not.
type authConfigResponse struct {
	OIDCEnabled bool `json:"oidc_enabled"`
}

// handleAuthConfig implements GET /api/v1/auth/config. It is public by virtue
// of its path prefix and must not require or read any session or bearer
// credential.
func (s *Server) handleAuthConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, authConfigResponse{
		OIDCEnabled: s.services != nil && s.services.OIDCEnabled,
	})
}

// registerAuthConfigRoutes registers the public auth-config endpoint. It is
// registered unconditionally (unlike the OIDC login/callback/logout handlers,
// which only mount when an issuer is configured) so the endpoint always
// answers with the correct oidc_enabled value — true or false.
func (s *Server) registerAuthConfigRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(fmt.Sprintf("GET %s/auth/config", prefix), s.handleAuthConfig)
}
