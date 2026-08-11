package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jaimegago/joe/internal/store"
)

// components_adapter_dispatch_test.go holds the API-side half of the
// adapter-dispatch consolidation break-test. The other two halves are
// internal/adapters/factory (construction coverage) and cmd/joe (the boot entry
// point).
//
// What these pin is that the API path actually routes through the canonical
// constructor, which coverage alone cannot show: a factory covering every type
// proves nothing if a call site still carries its own map.

// TestTestComponent_FormerlyBootOnlyTypesAreNoLongerReportedHealthy is the
// behavioural break-test for the Test Connection entry point.
//
// These five types sat in the boot pass and not in the runtime map, so
// handleTestComponent's nil branch caught them and answered ok:true — "type %q
// has no connection to test" — for components that very much do have a
// connection to test. An operator following the documented registration path got
// a green light from a component that had never connected to anything.
//
// The assertion is deliberately on `ok` alone, not on the message: these
// components have no reachable backend, so what should follow is an ordinary
// connect failure, and pinning the failure text would pin the wrong thing.
// Before the consolidation every one of these returned ok:true.
func TestTestComponent_FormerlyBootOnlyTypesAreNoLongerReportedHealthy(t *testing.T) {
	formerlyBootOnly := []string{
		store.ComponentTypeSplunk,
		store.ComponentTypeDynatrace,
		store.ComponentTypeNewRelic,
		store.ComponentTypeGitHub,
		store.ComponentTypeGitLab,
	}

	for _, componentType := range formerlyBootOnly {
		t.Run(componentType, func(t *testing.T) {
			srv, mux := setupWebUIServer(t)
			id := "c-" + componentType

			if err := srv.services.Store.Components.Create(context.Background(), &store.Component{
				ID: id, Type: componentType, Name: componentType,
				Config: json.RawMessage(`{}`),
			}); err != nil {
				t.Fatalf("seed component: %v", err)
			}

			w := doRequest(mux, "POST", "/api/v1/components/"+id+"/test", nil)
			if w.Code != http.StatusOK {
				t.Fatalf("test %s: status=%d body=%s; want 200 (the outcome rides in the body)", componentType, w.Code, w.Body.String())
			}

			var body struct {
				OK      bool   `json:"ok"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body %s: %v", w.Body.String(), err)
			}
			if body.OK {
				t.Errorf("test %s reported ok=true (%q) — a type with an adapter and no reachable backend must not report success",
					componentType, body.Message)
			}
		})
	}
}

// TestPromote_RegistersAdapterForFormerlyBootOnlyType is the behavioural
// break-test for the promotion entry point, and the item's headline case: a
// promoted github component used to get a nil no-op from the runtime map and
// carried no live adapter until the next restart.
//
// github is the type that makes this observable in-process — its Connect parses
// config and resolves the credential without reaching the network — so the whole
// promote → construct → Connect → Register chain runs for real. Before the
// consolidation the Get after promote returned ErrAdapterNotFound.
func TestPromote_RegistersAdapterForFormerlyBootOnlyType(t *testing.T) {
	f := promoteActivationFixture(t)

	const envVar = "JOE_ADAPTER_DISPATCH_GITHUB_TOKEN"
	t.Setenv(envVar, "a-token")

	// The inline token satisfies the github adapter's ParseConfig, which runs
	// ahead of credential resolution and requires the field to be non-empty; the
	// resolved static value then overrides it. Not a credential-bearing field at
	// the registration seam, so this registers.
	registerComponent(t, f, "c-github", store.ComponentTypeGitHub, `{"token":"placeholder-overridden-at-connect"}`)

	w := f.do(http.MethodPost, "/api/v1/components/c-github/promote",
		`{"env_var":"`+envVar+`"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("promote github: status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	adapter, err := f.services.Adapters.Get("c-github")
	if err != nil {
		t.Fatalf("registry after promoting a github component: %v; want the live adapter registered", err)
	}
	if adapter == nil {
		t.Fatal("registry after promote returned a nil adapter")
	}
}
