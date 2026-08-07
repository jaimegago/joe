package coreagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/safety"
)

// A003 Stream G — governed register_component LLM tool. The tool stays
// ActionRead and stays on the LLM surface (discovery is a legitimate LLM-path
// capability), but it is now credential-less by construction (same rejection
// rule as the HTTP create path) and writes a durable audit row even though it
// is credential-less. These are structural invariants, break-tested.

// TestRegisterComponentTool_RejectsCredentialFields proves the tool REJECTS a
// config carrying any credential-bearing field (so the LLM cannot supply a
// credential the operator surface would refuse) and that no component is
// written on rejection. One case per field class.
func TestRegisterComponentTool_RejectsCredentialFields(t *testing.T) {
	cases := map[string]map[string]any{
		"static_value":        {"value": "super-secret"},
		"static_env_var":      {"env_var": "AWS_SECRET_ACCESS_KEY"},
		"credential_provider": {"credential_provider": "static"},
		"entra_client_secret": {"client_secret_env_var": "AKS_CLIENT_SECRET"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			svc := makeTestServices(t)
			tool := NewRegisterComponentTool(svc, slog.Default())
			_, err := tool.Execute(context.Background(), map[string]any{
				"name": "discovered", "type": "kubernetes", "config": cfg,
			})
			if err == nil {
				t.Fatalf("credential field %s: expected rejection, got nil error", name)
			}
			comps, lerr := svc.Store.Components.List(context.Background())
			if lerr != nil {
				t.Fatalf("list components: %v", lerr)
			}
			if len(comps) != 0 {
				t.Errorf("component persisted despite credential field %s (count=%d); tool must reject", name, len(comps))
			}
		})
	}
}

// TestRegisterComponentTool_UnregistrableTypesRejected proves the tool rejects
// every type absent from the registrable set with an error and persists nothing —
// the SAME outcome a wholly unknown type gets, because both flow through
// store.IsValidComponentType. Two trims populated the set:
// trim-deadonarrival-component-types removed six with no construction path
// (oci_registry/dockerhub/artifactory/ecr have adapter packages wired into no
// construction map; cloudwatch/azuremonitor have no adapter code), and
// trim-unsupported-component-types removed twelve that are not credential-wired.
func TestRegisterComponentTool_UnregistrableTypesRejected(t *testing.T) {
	unregistrableTypes := []string{
		// No construction path (trim-deadonarrival-component-types).
		"oci_registry", "dockerhub", "artifactory", "ecr",
		"cloudwatch", "azuremonitor",
		// Not credential-wired (trim-unsupported-component-types). git is NOT in
		// this list: it was credential-wired and restored to the registrable set
		// by repo-registration-path / D-0150.
		"azure", "helm", "nginx-ingress", "aws", "datadog",
		"postgresql", "mysql", "redis", "mongodb", "kafka", "elasticsearch",
		"totally-unknown-type", // unknown-type baseline: identical outcome
	}
	for _, srcType := range unregistrableTypes {
		t.Run(srcType, func(t *testing.T) {
			svc := makeTestServices(t)
			tool := NewRegisterComponentTool(svc, slog.Default())
			_, err := tool.Execute(context.Background(), map[string]any{
				"name": "discovered", "type": srcType, "config": map[string]any{},
			})
			if err == nil {
				t.Fatalf("type %s: expected rejection, got nil error", srcType)
			}
			comps, lerr := svc.Store.Components.List(context.Background())
			if lerr != nil {
				t.Fatalf("list components: %v", lerr)
			}
			if len(comps) != 0 {
				t.Errorf("type %s: component persisted despite rejection (count=%d)", srcType, len(comps))
			}
		})
	}
}

// TestRegisterComponentTool_WritesAuditOnCredentiallessSuccess proves a
// credential-less success writes exactly one component.register audit row whose
// actor is the Core Agent principal (svc:agent:core).
func TestRegisterComponentTool_WritesAuditOnCredentiallessSuccess(t *testing.T) {
	svc := makeTestServices(t)
	svc.Audit = audit.NewRepository(svc.Store.DB(), svc.Store.Driver())
	tool := NewRegisterComponentTool(svc, slog.Default())

	_, err := tool.Execute(context.Background(), map[string]any{
		"name": "discovered-cluster", "type": "kubernetes",
		"config": map[string]any{"endpoint": "https://k8s.internal"},
	})
	if err != nil {
		t.Fatalf("credential-less register: unexpected error %v", err)
	}

	var n int
	if qerr := svc.Store.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM audit_log WHERE action = ?`, audit.ActionComponentRegister).Scan(&n); qerr != nil {
		t.Fatalf("count audit rows: %v", qerr)
	}
	if n != 1 {
		t.Fatalf("component.register audit rows = %d; want exactly 1", n)
	}

	var principal, decision string
	if qerr := svc.Store.DB().QueryRowContext(context.Background(),
		`SELECT principal, decision FROM audit_log WHERE action = ? ORDER BY id DESC LIMIT 1`,
		audit.ActionComponentRegister).Scan(&principal, &decision); qerr != nil {
		t.Fatalf("read audit row: %v", qerr)
	}
	wantActor, _ := rbac.AgentCorePrincipal()
	if principal != string(wantActor) {
		t.Errorf("audit principal = %q; want %q (the Core Agent principal)", principal, wantActor)
	}
	if decision != string(audit.DecisionAllow) {
		t.Errorf("audit decision = %q; want allow", decision)
	}
}

// TestRegisterComponentTool_AbsentConfigPersistsInert is the tool-path twin of
// the HTTP regression (TestCreateComponent_AbsentConfigPersistsInert): a
// register_component call with NO config arg must persist a credential-less,
// inert component whose stored config round-trips to a valid empty JSON object.
// Both registration surfaces consult the same normalization seam, so neither can
// regress the restored D-0029 config-less-registration invariant.
func TestRegisterComponentTool_AbsentConfigPersistsInert(t *testing.T) {
	svc := makeTestServices(t)
	svc.Audit = audit.NewRepository(svc.Store.DB(), svc.Store.Driver())
	tool := NewRegisterComponentTool(svc, slog.Default())

	// No "config" arg at all.
	_, err := tool.Execute(context.Background(), map[string]any{
		"name": "discovered-inert", "type": "prometheus",
	})
	if err != nil {
		t.Fatalf("absent-config register: unexpected error %v", err)
	}

	comps, lerr := svc.Store.Components.List(context.Background())
	if lerr != nil {
		t.Fatalf("list components: %v", lerr)
	}
	if len(comps) != 1 {
		t.Fatalf("config-less register persisted %d components; want 1", len(comps))
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(comps[0].Config, &fields); err != nil {
		t.Fatalf("stored config %q is not a valid JSON object: %v", string(comps[0].Config), err)
	}
	if len(fields) != 0 {
		t.Errorf("stored config = %q; want an empty object {}", string(comps[0].Config))
	}
}

// TestRegisterComponentTool_ClassificationUnchanged pins that register_component
// REMAINS ActionRead: writing a discovered component to Joe's own store is not a
// managed-system mutation, so it must NOT be reclassified to Mutate or subjected
// to the write floor (A003 Stream G constraint).
func TestRegisterComponentTool_ClassificationUnchanged(t *testing.T) {
	if got := safety.ClassifyTool("register_component").Class; got != safety.ActionRead {
		t.Errorf("register_component class = %v; want ActionRead — must not be reclassified to Mutate", got)
	}
}

// TestRegisterComponentTool_GitRegisters proves the agent registration surface
// admits git through the SAME single seam the HTTP path uses (D-0150): both call
// store.IsValidComponentType, so restoring the type admitted it at both surfaces
// by construction, with no per-surface edit.
func TestRegisterComponentTool_GitRegisters(t *testing.T) {
	svc := makeTestServices(t)
	tool := NewRegisterComponentTool(svc, slog.Default())
	if _, err := tool.Execute(context.Background(), map[string]any{
		"name": "discovered repo", "type": "git",
		"config": map[string]any{"url": "https://example.com/org/repo.git"},
	}); err != nil {
		t.Fatalf("register git: %v", err)
	}
	comps, err := svc.Store.Components.List(context.Background())
	if err != nil {
		t.Fatalf("list components: %v", err)
	}
	if len(comps) != 1 || comps[0].Type != "git" {
		t.Fatalf("components = %+v; want one git component", comps)
	}
}

// TestRegisterComponentTool_GitRejectsInlineCredentials proves the agent surface
// refuses the git adapter's retired inline auth fields exactly as the HTTP path
// does, because both consult the single credential-field denylist. A model cannot
// smuggle a token in through a config the operator surface would refuse.
func TestRegisterComponentTool_GitRejectsInlineCredentials(t *testing.T) {
	for _, field := range []string{"http_token", "ssh_key_path"} {
		t.Run(field, func(t *testing.T) {
			svc := makeTestServices(t)
			tool := NewRegisterComponentTool(svc, slog.Default())
			_, err := tool.Execute(context.Background(), map[string]any{
				"name": "repo", "type": "git",
				"config": map[string]any{"url": "https://example.com/r.git", field: "secret-ish"},
			})
			if err == nil {
				t.Fatalf("tool accepted inline credential field %q", field)
			}
			comps, lerr := svc.Store.Components.List(context.Background())
			if lerr != nil {
				t.Fatalf("list components: %v", lerr)
			}
			if len(comps) != 0 {
				t.Errorf("component persisted despite rejection of %q", field)
			}
		})
	}
}

// TestRegisterComponentTool_GitValidatesProviderReferenceShape proves the
// per-type shape rule fires on the agent path too: a malformed provider
// declaration is refused, while a well-formed one naming no existing component is
// accepted (the reference is a discovery hint, validated for shape only).
func TestRegisterComponentTool_GitValidatesProviderReferenceShape(t *testing.T) {
	svc := makeTestServices(t)
	tool := NewRegisterComponentTool(svc, slog.Default())

	if _, err := tool.Execute(context.Background(), map[string]any{
		"name": "repo", "type": "git",
		"config": map[string]any{"url": "https://example.com/r.git", "provider_component_id": "Not Valid"},
	}); err == nil {
		t.Fatal("tool accepted a malformed provider_component_id")
	}

	if _, err := tool.Execute(context.Background(), map[string]any{
		"name": "repo", "type": "git",
		"config": map[string]any{"url": "https://example.com/r.git", "provider_component_id": "gh-not-yet"},
	}); err != nil {
		t.Fatalf("tool refused a well-formed but dangling provider reference: %v", err)
	}
}
