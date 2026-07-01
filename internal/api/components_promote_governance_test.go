package api

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
)

// A003 promotion boundary — governed read-only-to-armed transition. These tests
// pin the security posture as STRUCTURAL invariants (break-tested): promotion is
// admin-gated, reject-unwired keyed on the W1 registry, same-tx fail-closed
// audited, indirection-only (no inline secret), never records credential
// material, performs no resolution, and owns the credential-update case via
// re-promote. They reuse the llmadminFixture (real migrated store, RBAC repo +
// admin gate, shared swappableAudit).

// registerComponent registers a credential-less component through the governed
// CREATE path so promotion tests start from a real inert record.
func registerComponent(t *testing.T, f *llmadminFixture, id, typ, config string) {
	t.Helper()
	body := `{"id":"` + id + `","type":"` + typ + `","name":"` + id + `","config":` + config + `}`
	w := f.do(http.MethodPost, "/api/v1/components", body, "user:alice")
	if w.Code != http.StatusCreated {
		t.Fatalf("register %s/%s: status=%d body=%s; want 201", typ, id, w.Code, w.Body.String())
	}
}

// componentConfigMap reads a component's current config back through the store
// and decodes it as a JSON object (nil if empty/non-object).
func componentConfigMap(t *testing.T, f *llmadminFixture, id string) map[string]any {
	t.Helper()
	comp, err := f.store.Components.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get component %q: %v", id, err)
	}
	if comp == nil {
		t.Fatalf("component %q not found", id)
	}
	if len(comp.Config) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(comp.Config, &m); err != nil {
		return nil
	}
	return m
}

// auditContextRaw returns the raw context blob of the most recent row for action.
func auditContextRaw(t *testing.T, f *llmadminFixture, action string) string {
	t.Helper()
	var blob string
	err := f.store.DB().QueryRowContext(context.Background(),
		`SELECT context FROM audit_log WHERE action = ? ORDER BY id DESC LIMIT 1`, action).Scan(&blob)
	if err != nil {
		t.Fatalf("query audit context for %q: %v", action, err)
	}
	return blob
}

// --- reject-unwired ---

// TestPromote_RejectsUnwiredType proves a component whose type has no wired
// credential provider cannot be armed: 400, config unchanged, no audit row.
// datadog is the unwired fixture: its credential is an api_key+app_key pair, so
// A003-W2 left it out of the static-token batch and it stays absent from the
// wired-type registry.
func TestPromote_RejectsUnwiredType(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-dd", "datadog", `{"site":"datadoghq.com"}`)

	w := f.do(http.MethodPost, "/api/v1/components/c-dd/promote",
		`{"env_var":"DD_TOKEN"}`, "user:alice")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("promote unwired datadog: status=%d body=%s; want 400", w.Code, w.Body.String())
	}
	// Assert the explicit reject-unwired check fired (the FIRST validation after
	// load), naming the type — not a downstream "unsupported kind" from reference
	// validation. This pins the ordering, not just the rejection.
	if !strings.Contains(w.Body.String(), "no credential provider wired for type datadog") {
		t.Errorf("reject-unwired error did not name the unwired type: %s", w.Body.String())
	}
	cfg := componentConfigMap(t, f, "c-dd")
	if _, armed := cfg["credential_provider"]; armed {
		t.Errorf("unwired promotion armed the component: config=%v", cfg)
	}
	if _, present := cfg["env_var"]; present {
		t.Errorf("unwired promotion wrote a locator: config=%v", cfg)
	}
	if n := f.countAudit(audit.ActionComponentPromote); n != 0 {
		t.Errorf("unwired promotion wrote %d audit rows; want 0", n)
	}
}

// --- wired types arm with a well-formed reference ---

// TestPromote_StaticWiredTypes proves github and gitlab (static-wired) arm with
// an env_var reference: config carries the discriminator + env_var, exactly one
// component.promote row, and the row carries NO credential material (the env var
// name does not appear in the audit context).
func TestPromote_StaticWiredTypes(t *testing.T) {
	for _, typ := range []string{"github", "gitlab"} {
		t.Run(typ, func(t *testing.T) {
			f := newLLMAdminFixture(t, true)
			f.markAdmin("user:alice")
			id := "c-" + typ
			registerComponent(t, f, id, typ, `{}`)

			const envName = "MY_FORGE_TOKEN_SECRET_LOCATOR"
			w := f.do(http.MethodPost, "/api/v1/components/"+id+"/promote",
				`{"env_var":"`+envName+`"}`, "user:alice")
			if w.Code != http.StatusOK {
				t.Fatalf("promote %s: status=%d body=%s; want 200", typ, w.Code, w.Body.String())
			}
			cfg := componentConfigMap(t, f, id)
			if cfg["credential_provider"] != "static" {
				t.Errorf("config credential_provider=%v; want static. config=%v", cfg["credential_provider"], cfg)
			}
			if cfg["env_var"] != envName {
				t.Errorf("config env_var=%v; want %q. config=%v", cfg["env_var"], envName, cfg)
			}
			if n := f.countAudit(audit.ActionComponentPromote); n != 1 {
				t.Fatalf("component.promote rows = %d; want exactly 1", n)
			}
			// No credential material in the audit row: the env var name must not
			// leak into the context blob (only the reference SHAPE is recorded).
			if raw := auditContextRaw(t, f, audit.ActionComponentPromote); strings.Contains(raw, envName) {
				t.Errorf("audit context leaked the env var locator value %q: %s", envName, raw)
			}
			principal, decision, d, _ := latestAudit(t, f, audit.ActionComponentPromote)
			if principal != "user:alice" || decision != string(audit.DecisionAllow) {
				t.Errorf("row principal=%q decision=%q; want user:alice/allow", principal, decision)
			}
			if d.Target != "component:"+id {
				t.Errorf("row target=%q; want component:%s", d.Target, id)
			}
		})
	}
}

// TestPromote_StaticBearerWired proves kubernetes (static-bearer-wired) arms with
// a hand-built-transport reference: config carries the discriminator, the cluster
// coordinates (api_server, ca_data), auth_method, and the env_var token locator;
// one audit row; and the env_var NAME does not leak into the row.
func TestPromote_StaticBearerWired(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-k8s", "kubernetes", `{}`)

	const envVar = "JOE_K8S_PROD_BEARER"
	w := f.do(http.MethodPost, "/api/v1/components/c-k8s/promote",
		`{"auth_method":"static-bearer","api_server":"https://k8s.prod:6443","ca_data":"PEMBYTES","env_var":"`+envVar+`"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("promote kubernetes: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	cfg := componentConfigMap(t, f, "c-k8s")
	if cfg["credential_provider"] != "static-bearer" {
		t.Errorf("config credential_provider=%v; want static-bearer. config=%v", cfg["credential_provider"], cfg)
	}
	if cfg["auth_method"] != "static-bearer" {
		t.Errorf("config auth_method=%v; want static-bearer", cfg["auth_method"])
	}
	if cfg["api_server"] != "https://k8s.prod:6443" {
		t.Errorf("config api_server=%v; want https://k8s.prod:6443", cfg["api_server"])
	}
	if cfg["env_var"] != envVar {
		t.Errorf("config env_var=%v; want %q", cfg["env_var"], envVar)
	}
	if n := f.countAudit(audit.ActionComponentPromote); n != 1 {
		t.Fatalf("component.promote rows = %d; want exactly 1", n)
	}
	if raw := auditContextRaw(t, f, audit.ActionComponentPromote); strings.Contains(raw, envVar) {
		t.Errorf("audit context leaked the env_var name %q: %s", envVar, raw)
	}
}

// TestPromote_PreservesRoutingConfig proves promotion MERGES the reference into
// the existing routing config rather than overwriting it.
func TestPromote_PreservesRoutingConfig(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh", "github", `{"endpoint":"https://github.example.com"}`)

	w := f.do(http.MethodPost, "/api/v1/components/c-gh/promote",
		`{"env_var":"GH_TOKEN"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("promote: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	cfg := componentConfigMap(t, f, "c-gh")
	if cfg["endpoint"] != "https://github.example.com" {
		t.Errorf("routing field 'endpoint' lost on promotion: config=%v", cfg)
	}
	if cfg["env_var"] != "GH_TOKEN" {
		t.Errorf("reference not written: config=%v", cfg)
	}
}

// --- inline-secret posture: indirection-only, inline value REJECTED ---

// TestPromote_RejectsInlineStaticValue pins the decision: an inline static
// `value` (a literal secret) is REFUSED — promotion requires an env_var
// indirection. The component stays un-armed and nothing audits. This is the
// invariant that keeps a literal secret out of the at-rest Config blob and the
// audit log entirely.
func TestPromote_RejectsInlineStaticValue(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh", "github", `{}`)

	const secret = "ghp_inline_literal_secret_value"
	w := f.do(http.MethodPost, "/api/v1/components/c-gh/promote",
		`{"value":"`+secret+`"}`, "user:alice")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("promote with inline value: status=%d body=%s; want 400 (indirection-only)", w.Code, w.Body.String())
	}
	cfg := componentConfigMap(t, f, "c-gh")
	if _, armed := cfg["credential_provider"]; armed {
		t.Errorf("inline-value promotion armed the component: config=%v", cfg)
	}
	if _, leaked := cfg["value"]; leaked {
		t.Errorf("inline secret written into config: config=%v", cfg)
	}
	if n := f.countAudit(audit.ActionComponentPromote); n != 0 {
		t.Errorf("rejected inline-value promotion wrote %d audit rows; want 0", n)
	}
	// The secret must appear nowhere in any audit row.
	var anyBlob string
	_ = f.store.DB().QueryRowContext(context.Background(),
		`SELECT COALESCE(GROUP_CONCAT(context), '') FROM audit_log`).Scan(&anyBlob)
	if strings.Contains(anyBlob, secret) {
		t.Errorf("inline secret leaked into the audit log: %s", anyBlob)
	}
}

// --- admin gate ---

// TestPromote_NonAdminForbidden proves a non-admin cannot promote: 403, config
// unchanged, no audit.
func TestPromote_NonAdminForbidden(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh", "github", `{}`)

	w := f.do(http.MethodPost, "/api/v1/components/c-gh/promote",
		`{"env_var":"GH_TOKEN"}`, "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin promote: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
	cfg := componentConfigMap(t, f, "c-gh")
	if _, armed := cfg["credential_provider"]; armed {
		t.Errorf("non-admin promotion armed the component: config=%v", cfg)
	}
	if n := f.countAudit(audit.ActionComponentPromote); n != 0 {
		t.Errorf("non-admin promote wrote %d audit rows; want 0", n)
	}
}

// --- same-tx fail-closed audit ---

// TestPromote_AuditFailureRollsBack proves the Config write and the audit row
// land in ONE transaction, fail-closed: a broken audit write rolls the arming
// back (the component stays credential-less) and the request is a server error.
func TestPromote_AuditFailureRollsBack(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh", "github", `{}`)
	f.breakAudit()

	w := f.do(http.MethodPost, "/api/v1/components/c-gh/promote",
		`{"env_var":"GH_TOKEN"}`, "user:alice")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("audit-broken promote: status=%d body=%s; want 500 (fail-closed)", w.Code, w.Body.String())
	}
	cfg := componentConfigMap(t, f, "c-gh")
	if _, armed := cfg["credential_provider"]; armed {
		t.Errorf("component armed despite failed audit (config=%v); same-tx fail-closed must roll back", cfg)
	}
}

// --- update-via-re-promote ---

// TestPromote_RearmOverwritesReference proves re-promoting an armed component
// overwrites the reference in a fresh governed/audited transaction, and that the
// second row records a re-arm (before.armed=true) — the credential-change case
// promotion owns, closing the delete-and-recreate-only gap (Finding 3).
func TestPromote_RearmOverwritesReference(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh", "github", `{}`)

	if w := f.do(http.MethodPost, "/api/v1/components/c-gh/promote",
		`{"env_var":"GH_TOKEN_OLD"}`, "user:alice"); w.Code != http.StatusOK {
		t.Fatalf("initial promote: status=%d body=%s", w.Code, w.Body.String())
	}
	if w := f.do(http.MethodPost, "/api/v1/components/c-gh/promote",
		`{"env_var":"GH_TOKEN_NEW"}`, "user:alice"); w.Code != http.StatusOK {
		t.Fatalf("re-promote: status=%d body=%s", w.Code, w.Body.String())
	}

	cfg := componentConfigMap(t, f, "c-gh")
	if cfg["env_var"] != "GH_TOKEN_NEW" {
		t.Errorf("re-promote did not overwrite reference: config=%v", cfg)
	}
	if n := f.countAudit(audit.ActionComponentPromote); n != 2 {
		t.Fatalf("component.promote rows = %d; want 2 (initial + re-arm)", n)
	}
	// The latest row is the re-arm: before.armed must be true.
	_, _, d, _ := latestAudit(t, f, audit.ActionComponentPromote)
	before, ok := d.Before.(map[string]any)
	if !ok {
		t.Fatalf("re-arm row has no before-state map: %+v", d)
	}
	if armed, _ := before["armed"].(bool); !armed {
		t.Errorf("re-arm row before.armed=%v; want true (must distinguish re-arm from initial-arm)", before["armed"])
	}
}

// TestPromote_RearmStillGatedAndValidated proves re-arm is subject to the SAME
// gate and validation: a non-admin re-arm is forbidden, and an inline value is
// still rejected on re-promote.
func TestPromote_RearmStillGatedAndValidated(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh", "github", `{}`)
	if w := f.do(http.MethodPost, "/api/v1/components/c-gh/promote",
		`{"env_var":"GH_TOKEN"}`, "user:alice"); w.Code != http.StatusOK {
		t.Fatalf("initial promote: status=%d", w.Code)
	}

	if w := f.do(http.MethodPost, "/api/v1/components/c-gh/promote",
		`{"env_var":"GH_TOKEN_2"}`, "user:bob"); w.Code != http.StatusForbidden {
		t.Errorf("non-admin re-promote: status=%d; want 403", w.Code)
	}
	if w := f.do(http.MethodPost, "/api/v1/components/c-gh/promote",
		`{"value":"inline"}`, "user:alice"); w.Code != http.StatusBadRequest {
		t.Errorf("inline-value re-promote: status=%d; want 400", w.Code)
	}
}

// --- mismatched discriminator ---

// TestPromote_RejectsMismatchedProvider proves a supplied credential_provider
// that does not match the type's wired Kind is rejected.
func TestPromote_RejectsMismatchedProvider(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh", "github", `{}`)

	// github is static-wired; asking for a non-matching provider must be refused.
	w := f.do(http.MethodPost, "/api/v1/components/c-gh/promote",
		`{"credential_provider":"vault-magic","env_var":"X"}`, "user:alice")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("mismatched provider: status=%d body=%s; want 400", w.Code, w.Body.String())
	}
}

// --- not found ---

func TestPromote_NotFound(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	w := f.do(http.MethodPost, "/api/v1/components/does-not-exist/promote",
		`{"env_var":"X"}`, "user:alice")
	if w.Code != http.StatusNotFound {
		t.Fatalf("promote missing component: status=%d body=%s; want 404", w.Code, w.Body.String())
	}
}

// --- no resolution on the promote path (structural) ---

// TestPromote_NoResolution is the STRUCTURAL guard that promotion writes a
// reference and never resolves it: handlePromoteComponent's body must contain no
// call to Connect, Resolve, or Probe, and no adapter construction
// (newAdapterForType) or provider selection (credential.Select). Whether the
// reference works is a separate explicit admin Probe (admin.go resolveAndProbe),
// not part of arming. A reviewer can be fooled; the AST cannot.
func TestPromote_NoResolution(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "components.go", nil, 0)
	if err != nil {
		t.Fatalf("parse components.go: %v", err)
	}
	var handler *ast.FuncDecl
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if ok && fd.Name.Name == "handlePromoteComponent" {
			handler = fd
			break
		}
	}
	if handler == nil {
		t.Fatal("handlePromoteComponent not found in components.go")
	}
	forbidden := map[string]bool{"Connect": true, "Resolve": true, "Probe": true, "Select": true, "newAdapterForType": true}
	var offenders []string
	ast.Inspect(handler.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if forbidden[fn.Sel.Name] {
				offenders = append(offenders, fn.Sel.Name)
			}
		case *ast.Ident:
			if forbidden[fn.Name] {
				offenders = append(offenders, fn.Name)
			}
		}
		return true
	})
	if len(offenders) != 0 {
		t.Errorf("handlePromoteComponent calls %v — promotion must perform NO credential resolution "+
			"(no Connect/Resolve/Probe/Select/adapter build). It writes a reference; connectivity is a separate explicit admin Probe.", offenders)
	}
}
