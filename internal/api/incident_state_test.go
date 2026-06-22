package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/sessionmodel"
)

// TestAdvanceIncidentState_LifecycleToResolve is the end-to-end proof that the
// new POST /sessions/{id}/incident-state endpoint makes resolve reachable
// THROUGH THE HTTP API — before it existed, an incident could only reach
// 'believed_mitigated' via a direct SQL bypass in tests, so a freshly-declared
// incident could never be resolved by an operator. Here the whole lifecycle runs
// over HTTP: declare → being_worked → believed_mitigated → resolve.
func TestAdvanceIncidentState_LifecycleToResolve(t *testing.T) {
	ts, sessRepo, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")

	sid := createDefaultSession(t, sessRepo, "alice")
	doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sid}).Body.Close()

	// declared → being_worked.
	advance := func(state string) *http.Response {
		return doRequest(t, http.MethodPost,
			ts.URL+"/api/v1/sessions/"+sid+"/incident-state", "alice",
			map[string]string{"state": state})
	}
	r1 := advance("being_worked")
	if r1.StatusCode != http.StatusOK {
		t.Fatalf("advance to being_worked: status = %d, want 200", r1.StatusCode)
	}
	r1.Body.Close()
	if sess, _ := sessRepo.GetSession(context.Background(), sid); sess.IncidentState == nil ||
		*sess.IncidentState != sessionmodel.IncidentStateBeingWorked {
		t.Fatalf("state = %v, want being_worked", sess.IncidentState)
	}

	// being_worked → believed_mitigated.
	r2 := advance("believed_mitigated")
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("advance to believed_mitigated: status = %d, want 200", r2.StatusCode)
	}
	r2.Body.Close()

	// resolve now succeeds — the gate ErrIncidentNotMitigated is cleared.
	rResolve := doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/resolve", "alice", nil)
	if rResolve.StatusCode != http.StatusOK {
		t.Fatalf("resolve after mitigated: status = %d, want 200", rResolve.StatusCode)
	}
	rResolve.Body.Close()

	if reg, _ := sessRepo.GetRegime(context.Background()); reg.Mode != sessionmodel.RegimeModeNormal {
		t.Errorf("regime after resolve = %q, want normal", reg.Mode)
	}
}

// TestAdvanceIncidentState_Unauthorized: a principal without the regime-control
// resolve capability is refused (403) — advancing carries the SAME authorization
// as resolve, so a non-captain/non-grantee cannot nudge the lifecycle.
func TestAdvanceIncidentState_Unauthorized(t *testing.T) {
	ts, sessRepo, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")
	sid := createDefaultSession(t, sessRepo, "alice")
	doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sid}).Body.Close()

	// bob holds no regime-control policy.
	resp := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/sessions/"+sid+"/incident-state", "bob",
		map[string]string{"state": "being_worked"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAdvanceIncidentState_RejectsNonIncident: a plain 'default' session is not
// an incident master, so the lifecycle controls cannot mutate it (409). This is
// what stops the controls from corrupting the wrong session.
func TestAdvanceIncidentState_RejectsNonIncident(t *testing.T) {
	ts, sessRepo, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")
	sid := createDefaultSession(t, sessRepo, "alice")

	resp := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/sessions/"+sid+"/incident-state", "alice",
		map[string]string{"state": "being_worked"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestAdvanceIncidentState_RejectsTerminalState: 'resolved' and 'reviewed' are
// owned by the resolve path (Invariant 4 / no-auto-resolve), so they are NOT
// reachable through this endpoint — a 400, not a silent state jump.
func TestAdvanceIncidentState_RejectsTerminalState(t *testing.T) {
	ts, sessRepo, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")
	sid := createDefaultSession(t, sessRepo, "alice")
	doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sid}).Body.Close()

	resp := doRequest(t, http.MethodPost,
		ts.URL+"/api/v1/sessions/"+sid+"/incident-state", "alice",
		map[string]string{"state": "resolved"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

// TestRegimeRead_SurfacesIncidentSession proves GET /regime now carries the
// active incident master's id and state, so the UI banner can mark and deep-link
// to the otherwise-unmarked promoted session. Normal mode omits them.
func TestRegimeRead_SurfacesIncidentSession(t *testing.T) {
	ts, sessRepo, rbacRepo := newRegimeServer(t)
	grantRegimeControl(t, rbacRepo, "alice")

	// Normal mode: no incident fields.
	var before struct {
		Mode              string `json:"Mode"`
		IncidentSessionID string `json:"IncidentSessionID"`
	}
	r0 := doRequest(t, http.MethodGet, ts.URL+"/api/v1/regime", "alice", nil)
	json.NewDecoder(r0.Body).Decode(&before)
	r0.Body.Close()
	if before.IncidentSessionID != "" {
		t.Errorf("normal mode IncidentSessionID = %q, want empty", before.IncidentSessionID)
	}

	sid := createDefaultSession(t, sessRepo, "alice")
	doRequest(t, http.MethodPost, ts.URL+"/api/v1/regime/declare", "alice",
		map[string]string{"session_id": sid}).Body.Close()

	var after struct {
		Mode              string `json:"Mode"`
		IncidentSessionID string `json:"IncidentSessionID"`
		IncidentState     string `json:"IncidentState"`
	}
	r1 := doRequest(t, http.MethodGet, ts.URL+"/api/v1/regime", "alice", nil)
	json.NewDecoder(r1.Body).Decode(&after)
	r1.Body.Close()
	if after.Mode != string(sessionmodel.RegimeModeIncident) {
		t.Errorf("mode = %q, want incident", after.Mode)
	}
	if after.IncidentSessionID != sid {
		t.Errorf("IncidentSessionID = %q, want %q", after.IncidentSessionID, sid)
	}
	if after.IncidentState != string(sessionmodel.IncidentStateDeclared) {
		t.Errorf("IncidentState = %q, want declared", after.IncidentState)
	}
}
