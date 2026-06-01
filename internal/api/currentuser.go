package api

import (
	"fmt"
	"net/http"

	"github.com/jaimegago/joe/internal/rbac"
)

// Stream G phase G5 — current-user endpoint.
//
// First current-user awareness for the frontend. Returns the three
// fields that, between them, are sufficient for an admin-only UI panel
// to hide itself for non-admins WITHOUT a probe-and-handle-403 round
// trip: who am I, am I an admin, and is enforcement even active?
//
// The endpoint is intentionally NOT admin-gated — every authenticated
// caller needs to know its own admin status to render UI correctly.
// Auth-disabled callers (services.RBACEnabled == false) get
// is_admin = true so the local-dev experience is unblocked, matching
// the admin gate's permit-all convention; principal in that case is
// whatever the edge resolved (the Unknown sentinel if nothing set it).

type currentUserResponse struct {
	Principal   string `json:"principal"`
	IsAdmin     bool   `json:"is_admin"`
	RBACEnabled bool   `json:"rbac_enabled"`
}

// handleCurrentUser implements GET /api/v1/me.
func (s *Server) handleCurrentUser(w http.ResponseWriter, r *http.Request) {
	principal := rbac.PrincipalFromContext(r.Context())
	rbacEnabled := s.services != nil && s.services.RBACEnabled

	isAdmin := !rbacEnabled // auth-disabled → admin-true, mirroring requireAdmin
	if rbacEnabled && s.services != nil && s.services.RBAC != nil {
		ok, err := s.services.RBAC.IsAdmin(r.Context(), string(principal))
		if err != nil {
			writeInternalError(w, err, "current-user is_admin read")
			return
		}
		isAdmin = ok
	}

	writeJSON(w, http.StatusOK, currentUserResponse{
		Principal:   string(principal),
		IsAdmin:     isAdmin,
		RBACEnabled: rbacEnabled,
	})
}

func (s *Server) registerCurrentUserRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(fmt.Sprintf("GET %s/me", prefix), s.handleCurrentUser)
}
