package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jaimegago/joe/internal/adapters"
)

// promoteActivationFixture is newLLMAdminFixture with an adapter registry wired
// in. The shared fixture passes nil for the registry (core.New's adapterRegistry
// argument), which is exactly the shape that made the promote-activation gap
// untestable before: with no registry there is nothing to observe. The Server
// holds the *core.Services pointer, so assigning after construction is seen by
// the handlers, which dereference services.Adapters at call time.
func promoteActivationFixture(t *testing.T) *llmadminFixture {
	t.Helper()
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	f.services.Adapters = adapters.NewRegistry()
	return f
}

// prometheusStub stands in for a reachable Prometheus. The adapter's Connect
// probes /api/v1/status/buildinfo, so a 200 there is the whole contract needed
// to drive a real in-process Connect success — no network, and no injectable
// adapter-factory seam in production code.
func prometheusStub(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status/buildinfo" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"version":"2.50.0"}}`))
	}))
	t.Cleanup(ts.Close)
	return ts
}

// TestPromote_RegistersLiveAdapter pins D-0119: a successful promote brings the
// live adapter up and registers it, so the autonomous refresher resolves it on
// the very next tick instead of logging "adapter not found for component X"
// until a daemon restart or a manual Test Connection.
//
// Revert-run-restore verified: with the connectAndRegisterAdapter call removed
// from handlePromoteComponent, this fails at the Get with ErrAdapterNotFound —
// the exact error the reported log line carried.
func TestPromote_RegistersLiveAdapter(t *testing.T) {
	f := promoteActivationFixture(t)
	ts := prometheusStub(t)

	const envVar = "JOE_PROMOTE_ACTIVATION_TEST_TOKEN"
	t.Setenv(envVar, "a-token")

	registerComponent(t, f, "c-prom", "prometheus", `{"url":"`+ts.URL+`"}`)

	// Registration alone constructs nothing (handleCreateComponent is
	// deliberately probe-free), so the registry must be empty before promote.
	if _, err := f.services.Adapters.Get("c-prom"); !errors.Is(err, adapters.ErrAdapterNotFound) {
		t.Fatalf("registry before promote: err=%v; want ErrAdapterNotFound", err)
	}

	w := f.do(http.MethodPost, "/api/v1/components/c-prom/promote",
		`{"env_var":"`+envVar+`"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("promote: status=%d body=%s; want 200", w.Code, w.Body.String())
	}

	adapter, err := f.services.Adapters.Get("c-prom")
	if err != nil {
		t.Fatalf("registry after promote: %v; want the live adapter registered", err)
	}
	if adapter == nil {
		t.Fatal("registry after promote returned a nil adapter")
	}
}

// TestPromote_ConnectFailureDoesNotRollBackArm pins the best-effort half of
// D-0119: activation rides behind the committed arm and cannot undo it. An
// unreachable backend still promotes 200 with the reference persisted; only the
// registration is skipped, leaving the refresher and Test Connection as the
// retry paths.
func TestPromote_ConnectFailureDoesNotRollBackArm(t *testing.T) {
	f := promoteActivationFixture(t)

	// A server closed before use gives a deterministic connection-refused on a
	// port nothing is listening on.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	const envVar = "JOE_PROMOTE_ACTIVATION_DEAD_TOKEN"
	t.Setenv(envVar, "a-token")

	registerComponent(t, f, "c-dead", "prometheus", `{"url":"`+deadURL+`"}`)

	w := f.do(http.MethodPost, "/api/v1/components/c-dead/promote",
		`{"env_var":"`+envVar+`"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("promote with unreachable backend: status=%d body=%s; want 200 (arm is committed independently of activation)", w.Code, w.Body.String())
	}

	cfg := componentConfigMap(t, f, "c-dead")
	if cfg["credential_provider"] != "static" {
		t.Errorf("config credential_provider=%v; want static (the arm must survive a failed connect). config=%v", cfg["credential_provider"], cfg)
	}
	if cfg["env_var"] != envVar {
		t.Errorf("config env_var=%v; want %q (the arm must survive a failed connect)", cfg["env_var"], envVar)
	}
	if _, err := f.services.Adapters.Get("c-dead"); !errors.Is(err, adapters.ErrAdapterNotFound) {
		t.Errorf("registry after failed connect: err=%v; want ErrAdapterNotFound (a failed Connect must register nothing)", err)
	}
}

// TestPromote_NilRegistryTolerated pins that activation is nil-safe: the
// composition root always wires a registry, but the shared api test fixtures
// pass nil, and a nil registry must degrade to "arm succeeds, nothing
// registered" rather than panicking inside a governed handler.
func TestPromote_NilRegistryTolerated(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	if f.services.Adapters != nil {
		t.Fatal("fixture unexpectedly wired an adapter registry; this test needs the nil case")
	}
	ts := prometheusStub(t)

	const envVar = "JOE_PROMOTE_ACTIVATION_NILREG_TOKEN"
	t.Setenv(envVar, "a-token")

	registerComponent(t, f, "c-nilreg", "prometheus", `{"url":"`+ts.URL+`"}`)

	w := f.do(http.MethodPost, "/api/v1/components/c-nilreg/promote",
		`{"env_var":"`+envVar+`"}`, "user:alice")
	if w.Code != http.StatusOK {
		t.Fatalf("promote with nil registry: status=%d body=%s; want 200", w.Code, w.Body.String())
	}
}
