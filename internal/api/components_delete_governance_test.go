package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/store"
)

// A003 Stream G — governed component DELETE. Admin-gated, same-tx fail-closed
// audited, full-row removal (so no dangling credential reference survives).
// Reuses the llmadminFixture; componentCount lives in
// components_governance_test.go.

// TestDeleteComponent_NonAdminForbidden proves a non-admin cannot delete a
// component: 403, row intact, no delete audit.
func TestDeleteComponent_NonAdminForbidden(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.services.Adapters = adapters.NewRegistry()
	seedComponent(t, f, "del-bob")

	w := f.do(http.MethodDelete, "/api/v1/components/del-bob", "", "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin delete: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
	if got := componentCount(t, f, "del-bob"); got != 1 {
		t.Errorf("component removed by non-admin (count=%d); want 1 — delete must be admin-gated", got)
	}
	if n := f.countAudit(audit.ActionComponentDelete); n != 0 {
		t.Errorf("non-admin delete wrote %d audit rows; want 0", n)
	}
}

// TestDeleteComponent_AdminWritesAuditAndRemoves proves an admin delete removes
// the full record and writes its same-tx audit row.
func TestDeleteComponent_AdminWritesAuditAndRemoves(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.services.Adapters = adapters.NewRegistry()
	f.markAdmin("user:alice")
	seedComponent(t, f, "del-ok")

	w := f.do(http.MethodDelete, "/api/v1/components/del-ok", "", "user:alice")
	if w.Code != http.StatusNoContent {
		t.Fatalf("admin delete: status=%d body=%s; want 204", w.Code, w.Body.String())
	}
	if got := componentCount(t, f, "del-ok"); got != 0 {
		t.Errorf("component still present after admin delete (count=%d); full row must be removed", got)
	}
	principal, decision, d, found := latestAudit(t, f, audit.ActionComponentDelete)
	if !found {
		t.Fatal("no component.delete audit row")
	}
	if principal != "user:alice" || decision != string(audit.DecisionAllow) {
		t.Errorf("row principal=%q decision=%q; want user:alice/allow", principal, decision)
	}
	if d.Target != "component:del-ok" {
		t.Errorf("details=%+v; want target=component:del-ok", d)
	}
}

func seedComponent(t *testing.T, f *llmadminFixture, id string) {
	t.Helper()
	if err := f.store.Components.Create(context.Background(), &store.Component{
		ID:     id,
		Type:   "kubernetes",
		Name:   "seeded",
		Config: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("seed component %q: %v", id, err)
	}
}
