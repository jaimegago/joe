package api

import (
	"context"
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

// zoneAccess is one zone the caller can reach, with the actions that zone
// permits. The frontend reads this to (a) detect the zero-zone dead-end and
// render an "access pending" empty state instead of a chat input that would
// only 403, and (b) eventually surface what the caller can do where.
type zoneAccess struct {
	ID             string   `json:"id"`
	AllowedActions []string `json:"allowed_actions"`
}

type currentUserResponse struct {
	Principal   string `json:"principal"`
	IsAdmin     bool   `json:"is_admin"`
	RBACEnabled bool   `json:"rbac_enabled"`
	// OIDCEnabled tells the Web UI whether to offer the OIDC login button
	// (Stream H2). Sourced from cfg.Auth.OIDC.Configured() via
	// services.OIDCEnabled; present and correct whether OIDC is
	// configured or not. It is an app-wide capability flag, not a
	// per-caller fact, but rides on /me to avoid a second endpoint.
	OIDCEnabled bool `json:"oidc_enabled"`
	// Zones is the set of security zones the caller can reach, each with the
	// zone's allowed actions. For an admin it is every zone (admin allows any
	// zone the zone itself permits — D-0011); for a non-admin it is exactly
	// the zones their rbac_policies grants cover. Always a non-nil slice so
	// the JSON is `[]` (not null) for a zero-zone caller — the signal the UI
	// keys its access-pending empty state on. The same per-principal grant
	// data the RBAC engine reads for every decision, surfaced here.
	Zones []zoneAccess `json:"zones"`
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

	zones, err := s.zonesForCaller(r.Context(), string(principal), isAdmin)
	if err != nil {
		writeInternalError(w, err, "current-user zones read")
		return
	}

	writeJSON(w, http.StatusOK, currentUserResponse{
		Principal:   string(principal),
		IsAdmin:     isAdmin,
		RBACEnabled: rbacEnabled,
		OIDCEnabled: s.services != nil && s.services.OIDCEnabled,
		Zones:       zones,
	})
}

// zonesForCaller resolves the zones the caller can reach. An admin reaches
// every zone (admin allows any zone the zone itself permits — D-0011); a
// non-admin reaches exactly the zones their rbac_policies grants cover. The
// result is always non-nil so the /me JSON carries `[]` (not null) for a
// zero-zone caller, which is the condition the UI's access-pending empty
// state detects. When RBAC is not configured (nil repo) there is no zone
// model to report, so the empty slice is returned.
func (s *Server) zonesForCaller(ctx context.Context, principal string, isAdmin bool) ([]zoneAccess, error) {
	zones := []zoneAccess{}
	if s.services == nil || s.services.RBAC == nil {
		return zones, nil
	}

	all, err := s.services.RBAC.ListZones(ctx)
	if err != nil {
		return nil, err
	}

	if isAdmin {
		for _, z := range all {
			zones = append(zones, toZoneAccess(z))
		}
		return zones, nil
	}

	// Non-admin: intersect all zones with the principal's granted zone ids.
	policies, err := s.services.RBAC.ListPoliciesForPrincipal(ctx, principal)
	if err != nil {
		return nil, err
	}
	granted := make(map[string]bool, len(policies))
	for _, p := range policies {
		granted[p.ZoneID] = true
	}
	for _, z := range all {
		if granted[z.ID] {
			zones = append(zones, toZoneAccess(z))
		}
	}
	return zones, nil
}

func toZoneAccess(z rbac.Zone) zoneAccess {
	actions := make([]string, 0, len(z.AllowedActions))
	for _, a := range z.AllowedActions {
		actions = append(actions, string(a))
	}
	return zoneAccess{ID: z.ID, AllowedActions: actions}
}

func (s *Server) registerCurrentUserRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(fmt.Sprintf("GET %s/me", prefix), s.handleCurrentUser)
}
