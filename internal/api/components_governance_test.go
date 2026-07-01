package api

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
)

// A003 Stream G — governed component CREATE. These tests pin the security
// posture as STRUCTURAL invariants (break-tested, not inspected): CREATE is
// admin-gated, same-tx fail-closed audited, credential-less by construction,
// and probe-free. They reuse the llmadminFixture (real migrated store, RBAC
// repo + admin gate, shared swappableAudit) — the same harness the
// read-promotions / admin-mutation governance tests use.

func componentCount(t *testing.T, f *llmadminFixture, id string) int {
	t.Helper()
	var n int
	if err := f.store.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM components WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("count components %q: %v", id, err)
	}
	return n
}

// --- credential-less by construction ---

// TestCreateComponent_RejectsCredentialFields proves that each credential-
// bearing field class is REJECTED (not silently stripped) at registration, and
// that a credential-less routing config is ACCEPTED. One sub-test per field
// class derived from the credential providers (static value/env_var, the
// credential_provider discriminator, an entra-exchange locator).
func TestCreateComponent_RejectsCredentialFields(t *testing.T) {
	rejected := map[string]string{
		"static_value":        `{"value":"super-secret-token"}`,
		"static_env_var":      `{"env_var":"AWS_SECRET_ACCESS_KEY"}`,
		"credential_provider": `{"credential_provider":"static"}`,
		"entra_client_secret": `{"client_secret_env_var":"AKS_CLIENT_SECRET"}`,
	}
	for name, cfg := range rejected {
		t.Run("rejects/"+name, func(t *testing.T) {
			f := newLLMAdminFixture(t, true)
			f.markAdmin("user:alice")
			body := `{"id":"c-` + name + `","type":"kubernetes","name":"k8s","config":` + cfg + `}`
			w := f.do(http.MethodPost, "/api/v1/components", body, "user:alice")
			if w.Code != http.StatusBadRequest {
				t.Fatalf("credential field %s: status=%d body=%s; want 400 (rejected, not stripped)", name, w.Code, w.Body.String())
			}
			if got := componentCount(t, f, "c-"+name); got != 0 {
				t.Errorf("component persisted despite credential field %s (count=%d); registration must reject", name, got)
			}
			if n := f.countAudit(audit.ActionComponentRegister); n != 0 {
				t.Errorf("audit rows = %d after rejected create; want 0", n)
			}
		})
	}

	t.Run("accepts/credential-less-routing", func(t *testing.T) {
		f := newLLMAdminFixture(t, true)
		f.markAdmin("user:alice")
		body := `{"id":"c-ok","type":"prometheus","name":"prom","config":{"endpoint":"https://prom.internal","audience":"prom"}}`
		w := f.do(http.MethodPost, "/api/v1/components", body, "user:alice")
		if w.Code != http.StatusCreated {
			t.Fatalf("credential-less routing config: status=%d body=%s; want 201", w.Code, w.Body.String())
		}
		if got := componentCount(t, f, "c-ok"); got != 1 {
			t.Errorf("credential-less component not persisted (count=%d); want 1", got)
		}
	})
}

// TestCreateComponent_AbsentConfigPersistsInert is the regression break-test for
// the restored D-0029 invariant: a registration payload carrying id + type +
// name and NO config field must persist and land inert. The bug was that an
// absent config reached the NOT NULL components.config INSERT as a nil blob and
// surfaced as a generic 500; the suite stayed green only because every other
// create helper always sent a config (even `{}`). The shared registration seam
// now normalizes the absent config to an empty JSON object before encryption, so
// the round-trip read returns a valid empty-object config — confirming the
// encrypt/decrypt path handles the defaulted "{}".
func TestCreateComponent_AbsentConfigPersistsInert(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")

	// No "config" key at all — the UI registration payload shape that regressed.
	body := `{"id":"c-noconfig","type":"prometheus","name":"prom"}`
	w := f.do(http.MethodPost, "/api/v1/components", body, "user:alice")
	if w.Code != http.StatusCreated {
		t.Fatalf("absent-config create: status=%d body=%s; want 201", w.Code, w.Body.String())
	}
	if got := componentCount(t, f, "c-noconfig"); got != 1 {
		t.Fatalf("config-less component not persisted (count=%d); want 1", got)
	}

	// Round-trip read through the store: the stored config is a valid empty JSON
	// object, and the component is inert (no credential reference → not armed).
	comp, err := f.services.Store.Components.Get(context.Background(), "c-noconfig")
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	if comp == nil {
		t.Fatal("component not found after create")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(comp.Config, &fields); err != nil {
		t.Fatalf("stored config %q is not a valid JSON object: %v", string(comp.Config), err)
	}
	if len(fields) != 0 {
		t.Errorf("stored config = %q; want an empty object {}", string(comp.Config))
	}
	if _, armed := armedState(comp.Config); armed {
		t.Error("config-less registration must land inert (unarmed); armedState reported armed")
	}
}

// --- admin gate ---

// TestCreateComponent_NonAdminForbidden proves a non-admin cannot register a
// component: 403, no row, no audit.
func TestCreateComponent_NonAdminForbidden(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	body := `{"id":"c-bob","type":"kubernetes","name":"k8s","config":{}}`
	w := f.do(http.MethodPost, "/api/v1/components", body, "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin create: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
	if got := componentCount(t, f, "c-bob"); got != 0 {
		t.Errorf("non-admin create persisted a component (count=%d); want 0", got)
	}
	if n := f.countAudit(audit.ActionComponentRegister); n != 0 {
		t.Errorf("non-admin create wrote %d audit rows; want 0", n)
	}
}

// --- same-tx fail-closed audit ---

// TestCreateComponent_AdminWritesSingleAuditRow proves an admin create writes
// EXACTLY ONE component.register row, with the registering principal and the
// component target.
func TestCreateComponent_AdminWritesSingleAuditRow(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	body := `{"id":"c-audit","type":"kubernetes","name":"prod-cluster","config":{}}`
	w := f.do(http.MethodPost, "/api/v1/components", body, "user:alice")
	if w.Code != http.StatusCreated {
		t.Fatalf("admin create: status=%d body=%s; want 201", w.Code, w.Body.String())
	}
	if n := f.countAudit(audit.ActionComponentRegister); n != 1 {
		t.Fatalf("component.register audit rows = %d; want exactly 1", n)
	}
	principal, decision, d, found := latestAudit(t, f, audit.ActionComponentRegister)
	if !found {
		t.Fatal("no component.register audit row")
	}
	if principal != "user:alice" || decision != string(audit.DecisionAllow) {
		t.Errorf("row principal=%q decision=%q; want user:alice/allow", principal, decision)
	}
	if d.Target != "component:c-audit" || d.After == nil {
		t.Errorf("details=%+v; want target=component:c-audit with after-state", d)
	}
}

// TestCreateComponent_AuditFailureRollsBack proves the audit row and the
// component land in ONE transaction, fail-closed: when the audit write fails,
// the component is NOT persisted and the request is a server error.
func TestCreateComponent_AuditFailureRollsBack(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	f.breakAudit()

	body := `{"id":"c-rollback","type":"kubernetes","name":"k8s","config":{}}`
	w := f.do(http.MethodPost, "/api/v1/components", body, "user:alice")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("audit-broken create: status=%d body=%s; want 500 (fail-closed)", w.Code, w.Body.String())
	}
	if got := componentCount(t, f, "c-rollback"); got != 0 {
		t.Errorf("component persisted despite failed audit (count=%d); same-tx fail-closed must roll back the create", got)
	}
}

// --- probe is gone (structural) ---

// TestCreateComponent_NoConnectProbe is the STRUCTURAL guard that the eager
// Connect probe was removed from the registration path: handleCreateComponent's
// body must contain no call to Connect and no call to newAdapterForType (the
// type→adapter resolver that fed the probe). A reviewer can be fooled; the AST
// cannot. Style mirrors TestAdminRoutes_AllRequireAdminGate.
func TestCreateComponent_NoConnectProbe(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "components.go", nil, 0)
	if err != nil {
		t.Fatalf("parse components.go: %v", err)
	}

	var handler *ast.FuncDecl
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if ok && fd.Name.Name == "handleCreateComponent" {
			handler = fd
			break
		}
	}
	if handler == nil {
		t.Fatal("handleCreateComponent not found in components.go")
	}

	var offenders []string
	ast.Inspect(handler.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if fn.Sel.Name == "Connect" {
				offenders = append(offenders, "Connect")
			}
		case *ast.Ident:
			if fn.Name == "newAdapterForType" {
				offenders = append(offenders, "newAdapterForType")
			}
		}
		return true
	})
	if len(offenders) != 0 {
		t.Errorf("handleCreateComponent calls %v — the eager Connect probe must NOT be re-introduced at registration (A003 Stream G). "+
			"A credential-less record cannot authenticate; connecting at registration is the attacker-controllable network-call / env-dereference vector. "+
			"Connectivity checking belongs to promotion.", offenders)
	}
}
