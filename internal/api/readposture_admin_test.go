package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/readposture"
)

// read-posture-latch — admin HTTP surface for the install-wide read posture
// (GET/POST /api/v1/admin/read-posture). These tests share the llmadminFixture,
// which now wires services.ReadPosture.

// TestReadPosture_Set_RequiresAdmin: a non-admin caller is refused (403) and no
// audit row and no posture change is written.
func TestReadPosture_Set_RequiresAdmin(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	w := f.do(http.MethodPost, "/api/v1/admin/read-posture",
		`{"posture":"zoned"}`, "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin set: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
	if n := f.countAudit(audit.ActionAdminReadPostureSet); n != 0 {
		t.Errorf("audit rows for read_posture.set = %d; want 0 — non-admin must not write", n)
	}
	got, err := f.services.ReadPosture.Repo().ReadPosture(context.Background())
	if err != nil {
		t.Fatalf("ReadPosture: %v", err)
	}
	if got != readposture.PostureTeamFlat {
		t.Errorf("posture after rejected set = %q; want unchanged %q", got, readposture.PostureTeamFlat)
	}
}

// TestReadPosture_Set_RejectsUnknownPosture: an unknown posture value is a 400
// and writes nothing.
func TestReadPosture_Set_RejectsUnknownPosture(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	w := f.do(http.MethodPost, "/api/v1/admin/read-posture",
		`{"posture":"open"}`, "user:alice")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown posture: status=%d body=%s; want 400", w.Code, w.Body.String())
	}
	if n := f.countAudit(audit.ActionAdminReadPostureSet); n != 0 {
		t.Errorf("audit rows for rejected unknown posture = %d; want 0", n)
	}
}

// TestReadPosture_Set_SuccessAuditsAtomically: an admin flip to zoned returns
// 200, persists the new posture, and writes one allow audit row with the
// {target, before, after} context.
func TestReadPosture_Set_SuccessAuditsAtomically(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	w := f.do(http.MethodPost, "/api/v1/admin/read-posture",
		`{"posture":"zoned"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("admin set: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	got, err := f.services.ReadPosture.Repo().ReadPosture(context.Background())
	if err != nil || got != readposture.PostureZoned {
		t.Fatalf("posture after set = (%q, %v); want (zoned, nil)", got, err)
	}
	principal, decision, d, found := latestAudit(t, f, audit.ActionAdminReadPostureSet)
	if !found {
		t.Fatal("no read_posture.set audit row written")
	}
	if principal != "user:alice" || decision != string(audit.DecisionAllow) {
		t.Errorf("set row principal=%q decision=%q; want user:alice/allow", principal, decision)
	}
	if d.Target != "read_posture" {
		t.Errorf("set row target=%q; want read_posture", d.Target)
	}
	if d.Before != readposture.PostureTeamFlat {
		t.Errorf("set row before=%v; want %q", d.Before, readposture.PostureTeamFlat)
	}
	if d.After != readposture.PostureZoned {
		t.Errorf("set row after=%v; want %q", d.After, readposture.PostureZoned)
	}
}

// TestReadPosture_Set_FailClosedOnAuditFailure: when the audit insert fails, the
// mutation rolls back — neither the posture nor an audit row persists, and the
// handler returns 500.
func TestReadPosture_Set_FailClosedOnAuditFailure(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	f.breakAudit()
	w := f.do(http.MethodPost, "/api/v1/admin/read-posture",
		`{"posture":"zoned"}`, "user:alice")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("set under broken audit: status=%d body=%s; want 500", w.Code, w.Body.String())
	}
	got, err := f.services.ReadPosture.Repo().ReadPosture(context.Background())
	if err != nil {
		t.Fatalf("ReadPosture after failed set: %v", err)
	}
	if got != readposture.PostureTeamFlat {
		t.Error("posture must not change when the audit write failed (rollback)")
	}
}

// TestReadPosture_Get_DefaultTeamFlat: GET returns the current posture; a fresh
// install reports team_flat, and the read is audited (fail-open) under an admin.
func TestReadPosture_Get_DefaultTeamFlat(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	w := f.do(http.MethodGet, "/api/v1/admin/read-posture", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("admin get: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	var body struct {
		Posture string `json:"posture"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode get body: %v", err)
	}
	if body.Posture != readposture.PostureTeamFlat {
		t.Errorf("GET posture=%q; want %q (launch default)", body.Posture, readposture.PostureTeamFlat)
	}
	if n := f.countAudit(audit.ActionAdminReadPostureRead); n != 1 {
		t.Errorf("read_posture.read audit rows = %d; want 1", n)
	}
}

// TestReadPosture_Get_RequiresAdmin: a non-admin GET is refused (403).
func TestReadPosture_Get_RequiresAdmin(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	w := f.do(http.MethodGet, "/api/v1/admin/read-posture", "", "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin get: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
}
