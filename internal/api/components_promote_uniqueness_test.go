package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/credential"
)

// TestPromote_StaticEnvVarUniqueness break-tests the D-0061 invariant: two
// distinct components cannot be promoted to the SAME environment variable, and
// two distinct components promoted to DISTINCT names each resolve to their own
// value. It drives the REAL HTTP promotion guard (not a reimplementation) and the
// REAL StaticProvider.Resolve, so it fails if either the guard or resolution
// regresses. Env vars are process-global, so two components sharing a name would
// resolve to one secret with no distinction — the guard prevents exactly that.
func TestPromote_StaticEnvVarUniqueness(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh-a", "github", `{}`)
	registerComponent(t, f, "c-gh-b", "github", `{}`)

	const shared = "JOE_GITHUB_PROD"

	// First component arms with the shared name: 200.
	if w := f.do(http.MethodPost, "/api/v1/components/c-gh-a/promote",
		`{"env_var":"`+shared+`"}`, "user:alice"); w.Code != http.StatusOK {
		t.Fatalf("promote c-gh-a: status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	// COLLISION: a DISTINCT component cannot take the same name — 409, the error
	// names the conflicting component, and c-gh-b stays un-armed.
	w := f.do(http.MethodPost, "/api/v1/components/c-gh-b/promote",
		`{"env_var":"`+shared+`"}`, "user:alice")
	if w.Code != http.StatusConflict {
		t.Fatalf("collision promote c-gh-b: status=%d body=%s; want 409 (uniqueness guard)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "c-gh-a") {
		t.Errorf("conflict error did not name the conflicting component: %s", w.Body.String())
	}
	if cfg := componentConfigMap(t, f, "c-gh-b"); cfg["env_var"] != nil {
		t.Errorf("collision promotion armed c-gh-b anyway: config=%v", cfg)
	}

	// SELF-EXCLUSION: re-promoting c-gh-a to its OWN existing name is NOT a
	// self-conflict — the scan excludes the component being promoted.
	if w := f.do(http.MethodPost, "/api/v1/components/c-gh-a/promote",
		`{"env_var":"`+shared+`"}`, "user:alice"); w.Code != http.StatusOK {
		t.Fatalf("self re-promote c-gh-a to its own name: status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	// DISTINCT name: c-gh-b arms with its own name — 200.
	const distinct = "JOE_GITHUB_STAGING"
	if w := f.do(http.MethodPost, "/api/v1/components/c-gh-b/promote",
		`{"env_var":"`+distinct+`"}`, "user:alice"); w.Code != http.StatusOK {
		t.Fatalf("distinct promote c-gh-b: status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	// Two distinct components now hold two distinct locators, and each resolves to
	// its OWN value through the REAL static provider — proving the names are
	// independently resolvable, not aliased.
	t.Setenv(shared, "token-for-A")
	t.Setenv(distinct, "token-for-B")
	provider := credential.NewStaticProvider()

	resolveValue := func(id string) string {
		t.Helper()
		comp, err := f.store.Components.Get(context.Background(), id)
		if err != nil || comp == nil {
			t.Fatalf("get %s: %v", id, err)
		}
		res, err := provider.Resolve(context.Background(), id, comp.Config)
		if err != nil {
			t.Fatalf("resolve %s: %v", id, err)
		}
		v, ok := res.StaticValue()
		if !ok {
			t.Fatalf("resolve %s: no static value (diag=%+v)", id, res.Diagnostic)
		}
		return v
	}

	if got := resolveValue("c-gh-a"); got != "token-for-A" {
		t.Errorf("c-gh-a resolved to %q; want token-for-A", got)
	}
	if got := resolveValue("c-gh-b"); got != "token-for-B" {
		t.Errorf("c-gh-b resolved to %q; want token-for-B", got)
	}
}

// TestPromote_EntraSharedClientSecretAllowed break-tests the deliberate exemption
// (agent-identity-doc-03): the Entra client secret is referenced by the DISTINCT
// field client_secret_env_var, which the env-var uniqueness guard (keyed on the
// literal env_var field) does NOT scan. So two kubernetes components MAY both
// promote with the SAME client_secret_env_var — the legitimate case of one Azure
// app registration fronting many clusters — where two static-bearer env_var token
// references would collide. It drives the REAL HTTP promotion guard.
func TestPromote_EntraSharedClientSecretAllowed(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-aks-a", "kubernetes", `{}`)
	registerComponent(t, f, "c-aks-b", "kubernetes", `{}`)

	const sharedSecret = "JOE_AZURE_APP_SECRET"
	body := func(apiServer string) string {
		return `{"auth_method":"entra-exchange","api_server":"` + apiServer + `","tenant_id":"tenant-1","client_id":"app-1","audience":"api://aks","client_secret_env_var":"` + sharedSecret + `"}`
	}

	// Both AKS clusters arm via the SAME app-registration client secret — both 200.
	if w := f.do(http.MethodPost, "/api/v1/components/c-aks-a/promote", body("https://aks-a:443"), "user:alice"); w.Code != http.StatusOK {
		t.Fatalf("promote c-aks-a: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
	if w := f.do(http.MethodPost, "/api/v1/components/c-aks-b/promote", body("https://aks-b:443"), "user:alice"); w.Code != http.StatusOK {
		t.Fatalf("promote c-aks-b sharing the same client_secret_env_var: status=%d body=%s; want 200 (shared app registration is legitimate, not a collision)", w.Code, w.Body.String())
	}

	// Both armed configs carry the shared secret reference and the entra discriminator.
	for _, id := range []string{"c-aks-a", "c-aks-b"} {
		cfg := componentConfigMap(t, f, id)
		if cfg["client_secret_env_var"] != sharedSecret {
			t.Errorf("%s client_secret_env_var = %v; want %q", id, cfg["client_secret_env_var"], sharedSecret)
		}
		if cfg["credential_provider"] != string(credential.KindEntraExchange) {
			t.Errorf("%s credential_provider = %v; want entra-exchange", id, cfg["credential_provider"])
		}
		// The shared secret must NOT have been written under the guarded env_var field.
		if cfg["env_var"] != nil {
			t.Errorf("%s wrote env_var=%v; the Entra secret must use the distinct client_secret_env_var field", id, cfg["env_var"])
		}
	}
}
