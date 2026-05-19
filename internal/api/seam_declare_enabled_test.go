//go:build seam_enabled

package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestSeam_AutonomousDeclare_Enabled is the build-tag-isolated half of
// the Change 12 paired test for seams.JoeAutonomousDeclareEnabled.
// Compiled only with `go test -tags=seam_enabled ./...`; that tag
// selects internal/seams/seams_enabled.go (const true) over the default
// internal/seams/seams.go (const false).
//
// With the seam enabled, a declare request with declared_kind=joe must
// NOT return 403 from the seam gate — the declare handler falls through
// to the normal path. The test proves the call-site wiring is in place
// so future enablement of the seam in production is a one-line change.
func TestSeam_AutonomousDeclare_Enabled(t *testing.T) {
	ts, sessRepo, rbacRepo := newRegimeServer(t)
	_ = sessRepo
	grantRegimeControl(t, rbacRepo, "alice")

	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]any{"declared_kind": "joe"})
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("status = 403 with seams.JoeAutonomousDeclareEnabled = true — " +
			"the seam-gated path is still refusing. The call site in " +
			"internal/api/regime.go's declare handler must drop into the joe " +
			"branch when the seam is enabled.")
	}
	// The downstream behavior — what happens AFTER the seam check — is
	// out of scope for the seam test. Phase 1 wires the gate; future
	// changes wire the autonomous declare behavior itself. Accept any
	// non-403 response code (201 created on success, 4xx/5xx for any
	// other reason — but not the seam-gated 403).
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var body struct {
			SessionID string `json:"session_id"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if body.SessionID == "" {
			t.Errorf("seam-enabled declare succeeded but returned empty session_id")
		}
	}
}
