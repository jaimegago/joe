package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/store"
)

// A001-COREGOV CC-04 — admin HTTP surface for the per-component-type
// auto_promote_reads flag (GET/POST /api/v1/admin/read-promotions). These tests
// share the llmadminFixture, which now wires services.PromoteReads.

// TestReadPromotions_Set_RequiresAdmin: a non-admin caller is rejected (403)
// and no flag/audit row is written.
func TestReadPromotions_Set_RequiresAdmin(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	w := f.do(http.MethodPost, "/api/v1/admin/read-promotions",
		`{"component_type":"kubernetes","enabled":true}`, "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin set: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
	if n := f.countAudit(audit.ActionAdminReadPromoteSet); n != 0 {
		t.Errorf("audit rows for read_promotion.set = %d; want 0 — non-admin must not write", n)
	}
	on, err := f.services.PromoteReads.Repo().IsPromoted(context.Background(), "kubernetes")
	if err != nil || on {
		t.Errorf("flag after rejected set = (%v, %v); want (false, nil)", on, err)
	}
}

// TestReadPromotions_Set_RejectsUnknownType: an unknown component type is a 400
// and writes nothing.
func TestReadPromotions_Set_RejectsUnknownType(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	w := f.do(http.MethodPost, "/api/v1/admin/read-promotions",
		`{"component_type":"not-a-real-type","enabled":true}`, "user:alice")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown type: status=%d body=%s; want 400", w.Code, w.Body.String())
	}
	if n := f.countAudit(audit.ActionAdminReadPromoteSet); n != 0 {
		t.Errorf("audit rows for rejected unknown type = %d; want 0", n)
	}
}

// TestReadPromotions_Set_SuccessAuditsAtomically: an admin set returns 200,
// flips the flag, and writes one allow audit row with the {target, after}
// context.
func TestReadPromotions_Set_SuccessAuditsAtomically(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	w := f.do(http.MethodPost, "/api/v1/admin/read-promotions",
		`{"component_type":"kubernetes","enabled":true}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("admin set: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	on, err := f.services.PromoteReads.Repo().IsPromoted(context.Background(), "kubernetes")
	if err != nil || !on {
		t.Fatalf("flag after set = (%v, %v); want (true, nil)", on, err)
	}
	principal, decision, d, found := latestAudit(t, f, audit.ActionAdminReadPromoteSet)
	if !found {
		t.Fatal("no read_promotion.set audit row written")
	}
	if principal != "user:alice" || decision != string(audit.DecisionAllow) {
		t.Errorf("set row principal=%q decision=%q; want user:alice/allow", principal, decision)
	}
	if d.Target != "read_promotion:kubernetes" {
		t.Errorf("set row target=%q; want read_promotion:kubernetes", d.Target)
	}
	if d.After != true {
		t.Errorf("set row after=%v; want true", d.After)
	}
}

// TestReadPromotions_Set_FailClosedOnAuditFailure: when the audit insert fails,
// the mutation rolls back — neither the flag nor an audit row persists, and the
// handler returns 500.
func TestReadPromotions_Set_FailClosedOnAuditFailure(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	f.breakAudit()
	w := f.do(http.MethodPost, "/api/v1/admin/read-promotions",
		`{"component_type":"kubernetes","enabled":true}`, "user:alice")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("set under broken audit: status=%d body=%s; want 500", w.Code, w.Body.String())
	}
	on, err := f.services.PromoteReads.Repo().IsPromoted(context.Background(), "kubernetes")
	if err != nil {
		t.Fatalf("IsPromoted after failed set: %v", err)
	}
	if on {
		t.Error("flag must not persist when the audit write failed (rollback)")
	}
}

// TestReadPromotions_List_FullEnumWithOverlay: GET returns one row per known
// component type, with the stored ON flags overlaid.
func TestReadPromotions_List_FullEnumWithOverlay(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	// Promote one type.
	if err := f.services.PromoteReads.SetPromoted(
		context.Background(), "kubernetes", true); err != nil {
		t.Fatalf("seed SetPromoted: %v", err)
	}

	w := f.do(http.MethodGet, "/api/v1/admin/read-promotions", "", "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("admin list: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	var body struct {
		ReadPromotions []readPromotionView `json:"read_promotions"`
		Count          int                 `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if body.Count != len(store.AllowedComponentTypes()) {
		t.Errorf("count=%d; want %d (full enum)", body.Count, len(store.AllowedComponentTypes()))
	}
	var sawK8sOn, sawOtherOff bool
	for _, v := range body.ReadPromotions {
		if v.ComponentType == "kubernetes" && v.Enabled {
			sawK8sOn = true
		}
		if v.ComponentType != "kubernetes" && !v.Enabled {
			sawOtherOff = true
		}
	}
	if !sawK8sOn {
		t.Error("kubernetes should be reported ON in the list")
	}
	if !sawOtherOff {
		t.Error("at least one un-promoted type should be reported OFF (default)")
	}
}
