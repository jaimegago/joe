//go:build !seam_enabled

package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/runmodel"
)

// TestSeam_ConfirmCloseDisposition_Disabled is the default-build
// assertion half of the Change 12 paired test for
// seams.JoeConfirmCloseDispositionEnabled. With the seam at its
// production value (false), resolving a confirm_close solicitation
// with disposed_by="joe" must be refused with 403 BEFORE any write to
// the solicitation row.
//
// Human disposition (the default disposed_by) is unaffected and goes
// through the normal resolve path; that's covered by existing
// runs_test.go cases.
//
// The paired enabled-build assertion lives in
// seam_confirm_close_enabled_test.go with `//go:build seam_enabled`.
func TestSeam_ConfirmCloseDisposition_Disabled(t *testing.T) {
	ts, sessRepo, runRepo := newRunsServer(t)
	sessionID := declareIncident(t, sessRepo, "alice")
	runID := startRun(t, ts, sessionID, "alice")
	solID := openConfirmCloseSolicitation(t, ts, runID)

	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/solicitations/"+solID+"/resolve", "alice",
		map[string]any{
			"disposed_by":        "joe",
			"resolution_payload": map[string]any{"verdict": "close"},
		})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (joe-self-disposition seam is inert in "+
			"the default build; seams.JoeConfirmCloseDispositionEnabled = false)",
			resp.StatusCode)
	}

	// The solicitation must remain unresolved — the seam gate fires
	// BEFORE the resolve write.
	sol, _ := runRepo.GetSolicitation(context.Background(), solID)
	if sol == nil {
		t.Fatal("solicitation row missing after refused resolve")
	}
	if sol.ResolvedAt != nil {
		t.Errorf("solicitation resolved_at = %v despite seam refusal — the gate "+
			"must fire BEFORE the write", sol.ResolvedAt)
	}
}

// openConfirmCloseSolicitation opens a confirm_close solicitation on
// the given run via the HTTP layer and returns the new solicitation's
// id. Defined on the !seam_enabled side because the disabled-side test
// is the one that uses it. The enabled-side test reaches the gate
// through a fresh setup that doesn't need this helper.
func openConfirmCloseSolicitation(t *testing.T, ts *httptest.Server, runID string) string {
	t.Helper()
	resp := doRequest(t, http.MethodPost, ts.URL+"/api/v1/runs/"+runID+"/solicitations", "alice",
		map[string]any{
			"kind": "confirm_close",
			"payload": map[string]any{
				"conclusion":    "joe believes we're done",
				"action_ledger": []any{},
			},
		})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("open confirm_close: status = %d, want 201", resp.StatusCode)
	}
	var sol runmodel.Solicitation
	if err := json.NewDecoder(resp.Body).Decode(&sol); err != nil {
		t.Fatalf("decode solicitation: %v", err)
	}
	return sol.ID
}
