//go:build seam_enabled

package api_test

import (
	"net/http"
	"testing"
)

// TestSeam_AutonomousResolve_Enabled is the build-tag-isolated half of
// the Change 12 paired test for seams.JoeAutonomousResolveEnabled.
// Compiled only with `go test -tags=seam_enabled ./...`.
//
// With the seam enabled, a resolve request signaling as_joe=true must
// NOT return 403 from the seam gate. The downstream path may legitimately
// fail for other reasons (e.g., 409 "regime is not incident" because no
// incident is active in this minimal test setup), but the seam-gated
// 403 specifically must be absent. That proves the call-site wiring at
// internal/api/regime.go's resolve handler flips when the constant
// flips — future enablement is a one-line change.
func TestSeam_AutonomousResolve_Enabled(t *testing.T) {
	ts, _, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")

	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/resolve", "alice",
		map[string]any{"as_joe": true})
	defer resp.Body.Close()

	// The seam-gated 403 carries a specific error message. Other 403s
	// (e.g., RBAC zone-access failure) also exist but the test environment
	// grants regime-control to alice, so the only path to 403 would be
	// the seam itself. Reject any 403 outright.
	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("status = 403 with seams.JoeAutonomousResolveEnabled = true — " +
			"the seam-gated path is still refusing. The call site in " +
			"internal/api/regime.go's resolve handler must drop the seam " +
			"check when the constant is true.")
	}
}
