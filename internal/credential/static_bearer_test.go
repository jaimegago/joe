package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// TestStaticBearer_EnvVarSourceResolvesToken proves the env_var source reuses the
// call-time env lookup: only a NAME is stored, the value is read at Resolve, and
// the resolved bearer token is reachable solely through the typed accessor.
func TestStaticBearer_EnvVarSourceResolvesToken(t *testing.T) {
	p := &StaticBearerProvider{
		lookupEnv: func(name string) (string, bool) {
			if name == "JOE_K8S_TOKEN" {
				return "bearer-from-env", true
			}
			return "", false
		},
	}
	raw := json.RawMessage(`{"credential_provider":"static-bearer","env_var":"JOE_K8S_TOKEN"}`)
	res, err := p.Resolve(context.Background(), "k8s-1", raw)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !res.Diagnostic.OK || res.Diagnostic.Stage != StageMintSucceeded {
		t.Fatalf("diagnostic = %+v; want ok mint-succeeded", res.Diagnostic)
	}
	if res.Diagnostic.Provider != KindStaticBearer {
		t.Errorf("provider = %q; want static-bearer", res.Diagnostic.Provider)
	}
	tok, ok := res.BearerToken()
	if !ok || tok != "bearer-from-env" {
		t.Fatalf("BearerToken = %q,%t; want bearer-from-env,true", tok, ok)
	}
	// A static-bearer resolution is NOT a static resolution.
	if _, ok := res.StaticValue(); ok {
		t.Errorf("StaticValue returned ok=true for a static-bearer resolution")
	}
}

// TestStaticBearer_EnvVarUnsetFails proves an unset named variable stops at
// mint-attempted with a non-sensitive reason and no token.
func TestStaticBearer_EnvVarUnsetFails(t *testing.T) {
	p := &StaticBearerProvider{lookupEnv: func(string) (string, bool) { return "", false }}
	raw := json.RawMessage(`{"env_var":"JOE_K8S_MISSING"}`)
	res, err := p.Resolve(context.Background(), "k8s-1", raw)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Diagnostic.OK || res.Diagnostic.Stage != StageMintAttempted {
		t.Fatalf("diagnostic = %+v; want not-ok mint-attempted", res.Diagnostic)
	}
	if tok, ok := res.BearerToken(); ok {
		t.Errorf("BearerToken = %q; want none on failure", tok)
	}
}

// TestStaticBearer_InClusterSourceReadsMountedToken proves the in_cluster source
// reads the pod-mounted service-account token DIRECTLY from the well-known path —
// not via rest.InClusterConfig — and returns it trimmed as the bearer token.
func TestStaticBearer_InClusterSourceReadsMountedToken(t *testing.T) {
	var readPath string
	p := &StaticBearerProvider{
		readFile: func(path string) ([]byte, error) {
			readPath = path
			return []byte("mounted-sa-token\n"), nil
		},
	}
	raw := json.RawMessage(`{"in_cluster":true}`)
	res, err := p.Resolve(context.Background(), "k8s-1", raw)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if readPath != serviceAccountTokenPath {
		t.Errorf("read path = %q; want the well-known SA token mount %q", readPath, serviceAccountTokenPath)
	}
	tok, ok := res.BearerToken()
	if !ok || tok != "mounted-sa-token" {
		t.Fatalf("BearerToken = %q,%t; want mounted-sa-token,true (trimmed)", tok, ok)
	}
}

// TestStaticBearer_InClusterUnreadableFails proves an unreadable mount stops at
// mint-attempted with a non-sensitive reason.
func TestStaticBearer_InClusterUnreadableFails(t *testing.T) {
	p := &StaticBearerProvider{readFile: func(string) ([]byte, error) { return nil, fmt.Errorf("no such file") }}
	raw := json.RawMessage(`{"in_cluster":true}`)
	res, err := p.Resolve(context.Background(), "k8s-1", raw)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Diagnostic.OK || res.Diagnostic.Stage != StageMintAttempted {
		t.Fatalf("diagnostic = %+v; want not-ok mint-attempted", res.Diagnostic)
	}
}

// TestStaticBearer_NoSourceErrors proves a config with neither source is a hard
// configuration error (distinct from an operational mint failure).
func TestStaticBearer_NoSourceErrors(t *testing.T) {
	p := NewStaticBearerProvider()
	if _, err := p.Resolve(context.Background(), "k8s-1", json.RawMessage(`{}`)); err == nil {
		t.Fatal("want error when neither env_var nor in_cluster is supplied")
	}
}

// TestStaticBearer_ProbeNoOpSuccess proves Probe advances a resolved token to
// connectivity-probed without contacting a backend.
func TestStaticBearer_ProbeNoOpSuccess(t *testing.T) {
	p := &StaticBearerProvider{lookupEnv: func(string) (string, bool) { return "tok", true }}
	res, err := p.Resolve(context.Background(), "k8s-1", json.RawMessage(`{"env_var":"X"}`))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	probed, err := p.Probe(context.Background(), res)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probed.Diagnostic.Stage != StageConnectivityProbed || !probed.Diagnostic.OK {
		t.Errorf("probe diagnostic = %+v; want ok connectivity-probed", probed.Diagnostic)
	}
}

// TestStaticBearer_AvailableReferencesNotApplicable proves the provider answers
// honestly not-applicable: its env_var name is free-form and its in_cluster source
// is a fixed mount, so neither is an enumerable candidate set.
func TestStaticBearer_AvailableReferencesNotApplicable(t *testing.T) {
	refs, err := NewStaticBearerProvider().AvailableReferences("kubernetes")
	if err != nil {
		t.Fatalf("AvailableReferences: %v", err)
	}
	if refs.Applicable {
		t.Errorf("static-bearer references should be not-applicable")
	}
	if len(refs.Candidates) != 0 {
		t.Errorf("candidates = %+v; want empty", refs.Candidates)
	}
}
