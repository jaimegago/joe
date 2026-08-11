package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
	"github.com/jaimegago/joe/internal/store"
)

// TestConnectSourcesDefault_ConnectsTypeTheBootPassOnceMissed is the boot-side
// half of the adapter-dispatch consolidation break-test.
//
// prometheus was in the runtime adapter map and absent from the boot pass's
// hand-rolled type list, so a prometheus component lost its live adapter on
// every restart and nothing rebuilt it until an admin clicked Test Connection
// per component. The boot pass now iterates the stored components and builds
// each through the canonical constructor, so the type list it can drift from no
// longer exists.
//
// Run against the previous boot pass this fails at the registry Get with
// ErrAdapterNotFound: nothing ever listed prometheus.
func TestConnectSourcesDefault_ConnectsTypeTheBootPassOnceMissed(t *testing.T) {
	ctx := context.Background()

	// The prometheus adapter's Connect probes /api/v1/status/buildinfo, so a 200
	// there is the whole contract needed to drive a real in-process Connect.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status/buildinfo" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.50.0"}}`))
	}))
	defer backend.Close()

	const envVar = "JOE_BOOT_CONNECT_PROMETHEUS_TOKEN"
	t.Setenv(envVar, "a-token")

	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Stored in the shape promotion leaves behind: the backend coordinate plus a
	// credential reference, never an inline secret.
	config := `{"url":"` + backend.URL + `","credential_provider":"static","env_var":"` + envVar + `"}`
	if err := s.Components.Create(ctx, &store.Component{
		ID:     "c-prom",
		Type:   store.ComponentTypePrometheus,
		Name:   "prom",
		Config: json.RawMessage(config),
	}); err != nil {
		t.Fatalf("seed component: %v", err)
	}

	registry := adapters.NewRegistry()
	connectSourcesDefault(ctx, s, registry)

	adapter, err := registry.Get("c-prom")
	if err != nil {
		t.Fatalf("registry after boot connect: %v; want the prometheus adapter registered", err)
	}
	if adapter == nil {
		t.Fatal("registry after boot connect returned a nil adapter")
	}
	if status := adapter.Status(); !status.Connected {
		t.Errorf("registered adapter status = %+v; want Connected", status)
	}
}

// TestConnectSourcesDefault_UnbuildableTypeDoesNotStopTheRest pins that the boot
// pass stays per-component non-fatal now that it walks every stored component
// rather than a curated type list. A row whose type has no construction path —
// the artifact-registry group, which registration can no longer create but which
// pre-trim databases still hold — must be skipped and logged, not allowed to cut
// the pass short and leave later components adapterless.
func TestConnectSourcesDefault_UnbuildableTypeDoesNotStopTheRest(t *testing.T) {
	ctx := context.Background()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status/buildinfo" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.50.0"}}`))
	}))
	defer backend.Close()

	const envVar = "JOE_BOOT_CONNECT_SURVIVES_TOKEN"
	t.Setenv(envVar, "a-token")

	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := s.Components.Create(ctx, &store.Component{
		ID: "c-registry", Type: store.ComponentTypeOCIRegistry, Name: "legacy registry",
		Config: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("seed unbuildable component: %v", err)
	}
	config := `{"url":"` + backend.URL + `","credential_provider":"static","env_var":"` + envVar + `"}`
	if err := s.Components.Create(ctx, &store.Component{
		ID: "c-prom", Type: store.ComponentTypePrometheus, Name: "prom",
		Config: json.RawMessage(config),
	}); err != nil {
		t.Fatalf("seed component: %v", err)
	}

	registry := adapters.NewRegistry()
	connectSourcesDefault(ctx, s, registry)

	if _, err := registry.Get("c-prom"); err != nil {
		t.Errorf("registry for the buildable component: %v; an unbuildable row must not stop the pass", err)
	}
	if _, err := registry.Get("c-registry"); err == nil {
		t.Error("the unbuildable component registered an adapter; it has no construction path")
	}
}
