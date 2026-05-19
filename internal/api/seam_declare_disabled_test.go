//go:build !seam_enabled

package api_test

import (
	"net/http"
	"testing"
)

// TestSeam_AutonomousDeclare_Disabled is the default-build assertion
// half of the Change 12 paired test for
// seams.JoeAutonomousDeclareEnabled. With the seam at its production
// value (false), a declare request with declared_kind=joe must be
// refused with 403 BEFORE any call to
// sessionmodel.Repository.DeclareIncidentRegime.
//
// The paired enabled-build assertion lives in
// seam_declare_enabled_test.go with `//go:build seam_enabled`.
// Together they prove the wiring is in place: future enablement is a
// one-line constant change in internal/seams/seams.go, not a code
// change at the call site.
func TestSeam_AutonomousDeclare_Disabled(t *testing.T) {
	ts, _, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")

	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]any{"declared_kind": "joe"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (joe-autonomous declare seam is inert in the "+
			"default build; seams.JoeAutonomousDeclareEnabled = false)",
			resp.StatusCode)
	}
}
