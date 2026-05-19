//go:build !seam_enabled

package api_test

import (
	"net/http"
	"testing"
)

// TestSeam_AutonomousResolve_Disabled is the default-build assertion
// half of the Change 12 paired test for
// seams.JoeAutonomousResolveEnabled. With the seam at its production
// value (false), a resolve request signaling as_joe=true must be
// refused with 403 BEFORE any call to
// sessionmodel.Repository.ResolveIncidentRegime.
//
// This protects PHASE-0-SESSION-MODEL.md §R5 / Invariant 4 (incident-
// mode exit may not be automated): the AST grep guard in
// regime_invariant_test.go pins ResolveIncidentRegime's sole production
// caller to (*regimeHandler).resolve. The seam-disabled gate returns
// 403 before that call, preserving the single-call-site invariant.
//
// The paired enabled-build assertion lives in
// seam_resolve_enabled_test.go with `//go:build seam_enabled`.
func TestSeam_AutonomousResolve_Disabled(t *testing.T) {
	ts, _, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")

	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/resolve", "alice",
		map[string]any{"as_joe": true})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (joe-autonomous resolve seam is inert in "+
			"the default build; seams.JoeAutonomousResolveEnabled = false)",
			resp.StatusCode)
	}
}
