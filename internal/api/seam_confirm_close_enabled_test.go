//go:build seam_enabled

package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/runmodel"
)

// TestSeam_ConfirmCloseDisposition_Enabled is the build-tag-isolated
// half of the Change 12 paired test for
// seams.JoeConfirmCloseDispositionEnabled. Compiled only with
// `go test -tags=seam_enabled ./...`.
//
// With the seam enabled, resolving a confirm_close solicitation with
// disposed_by="joe" must NOT return 403 from the seam gate. The
// downstream resolve writes through (200 OK) since no other gate exists
// on that path. Proves the call-site wiring at
// internal/api/runs.go's resolveSolicitation handler flips when the
// constant flips.
func TestSeam_ConfirmCloseDisposition_Enabled(t *testing.T) {
	ts, sessRepo, _ := newRunsServer(t)
	sessionID := declareIncident(t, sessRepo, "alice")
	runID := startRun(t, ts, sessionID, "alice")

	// Open a confirm_close solicitation directly through the HTTP layer.
	openResp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/solicitations", "alice",
		map[string]any{
			"kind": "confirm_close",
			"payload": map[string]any{
				"conclusion":    "joe believes we're done",
				"action_ledger": []any{},
			},
		})
	if openResp.StatusCode != http.StatusCreated {
		t.Fatalf("open confirm_close: status = %d, want 201", openResp.StatusCode)
	}
	var sol runmodel.Solicitation
	_ = json.NewDecoder(openResp.Body).Decode(&sol)
	openResp.Body.Close()

	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/solicitations/"+sol.ID+"/resolve", "alice",
		map[string]any{
			"disposed_by":        "joe",
			"resolution_payload": map[string]any{"verdict": "close"},
		})
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("status = 403 with seams.JoeConfirmCloseDispositionEnabled = true — " +
			"the seam-gated path is still refusing. The call site in " +
			"internal/api/runs.go's resolveSolicitation handler must skip " +
			"the seam check when the constant is true.")
	}
}
