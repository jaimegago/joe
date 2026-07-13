package coreagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/core"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// forbiddenListErr builds the SHAPE the k8s adapter surfaces on a forbidden
// list: the client-go typed StatusError wrapped with the adapter's "list %s: %w"
// (internal/adapters/k8s/resources.go). Tests use it to prove the degradation
// path detects forbidden via typed-error unwrapping, not string matching.
func forbiddenListErr(resource string) error {
	typed := apierrors.NewForbidden(
		schema.GroupResource{Resource: resource},
		"",
		errors.New("cannot list resource"),
	)
	return fmt.Errorf("list %s: %w", resource, typed)
}

// TestRefreshK8sComponent_ForbiddenSkipsAndContinues proves the core degradation
// contract: a forbidden list error on one resource type (secrets) does NOT abort
// the tick — the type is recorded as a skip, the refresh continues, and the delta
// applied contains the OTHER types' nodes. The now-forbidden secret type yields
// no secret node (the reconcile-out consequence is intended).
func TestRefreshK8sComponent_ForbiddenSkipsAndContinues(t *testing.T) {
	graphStore := setupK8sGraphStore(t)
	refresher := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}

	source := &store.Component{ID: "src-forbidden", Type: store.ComponentTypeKubernetes, Name: "test"}
	adapter := &fakeK8sAdapter{
		items: map[string][]unstructured.Unstructured{
			"namespaces":  {makeNamespace("apps")},
			"deployments": {makeDeployment("apps", "api")},
			"nodes":       {makeNode("node-1")},
		},
		errResource: "secrets",
		err:         forbiddenListErr("secrets"),
	}

	skips, err := refresher.refreshK8sComponent(context.Background(), source, adapter)
	if err != nil {
		t.Fatalf("forbidden skip must not abort the tick, got error: %v", err)
	}
	if len(skips) != 1 || skips[0].Type != "secret" {
		t.Fatalf("expected exactly one skip for the secret type, got: %#v", skips)
	}

	nodes, _, err := LoadGraphStateForComponent(context.Background(), graphStore, source.ID)
	if err != nil {
		t.Fatalf("LoadGraphStateForComponent error: %v", err)
	}
	ids := map[string]struct{}{}
	for _, n := range nodes {
		ids[n.ID] = struct{}{}
	}
	// The successfully listed types are present.
	if _, ok := ids["k8s/src-forbidden/deployment/apps/api"]; !ok {
		t.Fatal("delta must contain the deployment node listed before the forbidden type")
	}
	// The forbidden type produced no node.
	if _, ok := ids["k8s/src-forbidden/secret/apps/app-secret"]; ok {
		t.Fatal("forbidden secret type must not appear in the graph")
	}
}

// TestRefreshK8sComponent_NonForbiddenAborts proves a non-forbidden list error
// keeps today's semantics exactly: the tick aborts with the wrapped error and
// applies NO delta (no nodes are written for the aborted component).
func TestRefreshK8sComponent_NonForbiddenAborts(t *testing.T) {
	graphStore := setupK8sGraphStore(t)
	refresher := &Refresher{
		services: &core.Services{Graph: graphStore},
		logger:   slog.Default(),
	}

	source := &store.Component{ID: "src-abort", Type: store.ComponentTypeKubernetes, Name: "test"}
	adapter := &fakeK8sAdapter{
		items: map[string][]unstructured.Unstructured{
			"namespaces":  {makeNamespace("apps")},
			"deployments": {makeDeployment("apps", "api")},
		},
		errResource: "secrets",
		err:         errors.New("connection reset by peer"), // not a typed forbidden error
	}

	skips, err := refresher.refreshK8sComponent(context.Background(), source, adapter)
	if err == nil {
		t.Fatal("a non-forbidden list error must abort the refresh")
	}
	if skips != nil {
		t.Fatalf("aborted tick must not return skips, got: %#v", skips)
	}
	nodes, _, loadErr := LoadGraphStateForComponent(context.Background(), graphStore, source.ID)
	if loadErr != nil {
		t.Fatalf("LoadGraphStateForComponent error: %v", loadErr)
	}
	if len(nodes) != 0 {
		t.Fatalf("aborted tick must apply no delta, but %d nodes were written", len(nodes))
	}
}

// TestRefreshComponent_DegradedStatusWrittenAndCleared drives the FULL
// refreshComponent path through a real store and proves the third status state:
// a skipped-but-successful tick writes status "degraded" with a summary naming
// the skipped type, and a subsequent clean tick writes the healthy status and
// clears last_error.
func TestRefreshComponent_DegradedStatusWrittenAndCleared(t *testing.T) {
	svc := makeTestServices(t)
	ctx := agentCoreCtx(t)

	comp := &store.Component{ID: "k8s-degraded", Name: "degraded", Type: store.ComponentTypeKubernetes, Config: jsonRaw()}
	if err := svc.Store.Components.Create(ctx, comp); err != nil {
		t.Fatalf("create component: %v", err)
	}

	promote := &fakePromote{
		idToType: map[string]string{"k8s-degraded": "kubernetes"},
		promoted: map[string]bool{"kubernetes": true},
	}
	engine := rbac.NewPolicyEngineWithPromote(emptyRBACRepo{}, promote)
	accessor := access.New(svc.Adapters, svc.Graph, engine, nil)
	r := &Refresher{services: svc, logger: slog.Default(), accessor: accessor}

	// Tick 1: secrets forbidden → degraded.
	svc.Adapters.Register("k8s-degraded", &fakeK8sAdapter{
		items: map[string][]unstructured.Unstructured{
			"namespaces": {makeNamespace("apps")},
		},
		errResource: "secrets",
		err:         forbiddenListErr("secrets"),
	})
	if err := r.refreshComponent(ctx, comp); err != nil {
		t.Fatalf("degraded tick must not error: %v", err)
	}

	got, err := svc.Store.Components.Get(ctx, "k8s-degraded")
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	if got.Status != "degraded" {
		t.Fatalf("status = %q, want degraded", got.Status)
	}
	if !strings.Contains(got.LastError, "secret") || !strings.Contains(got.LastError, "forbidden") {
		t.Fatalf("degraded last_error should name the skipped type and reason, got: %q", got.LastError)
	}

	// Tick 2: clean (no forbidden) → healthy, last_error cleared. Reload so the
	// baseline reflects the persisted degraded summary (recovery transition).
	svc.Adapters.Register("k8s-degraded", &fakeK8sAdapter{
		items: map[string][]unstructured.Unstructured{
			"namespaces": {makeNamespace("apps")},
			"secrets":    {makeSecret("apps", "app-secret")},
		},
	})
	if err := r.refreshComponent(ctx, got); err != nil {
		t.Fatalf("clean tick must not error: %v", err)
	}
	cleared, err := svc.Store.Components.Get(ctx, "k8s-degraded")
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	if cleared.Status != "active" {
		t.Fatalf("status = %q, want active after a clean tick", cleared.Status)
	}
	if cleared.LastError != "" {
		t.Fatalf("clean tick must clear last_error, got: %q", cleared.LastError)
	}
}

// TestRefreshCRDSpec_ForbiddenVsMissing proves the CRD path distinguishes a
// forbidden CRD list (degradation → a skip) from an uninstalled CRD (not-found →
// silent, no skip), and treats any other error as a tolerant no-skip Debug skip.
func TestRefreshCRDSpec_ForbiddenVsMissing(t *testing.T) {
	r := setupCRDRefresher(t)
	src := &store.Component{ID: "src-crd-deg", Type: store.ComponentTypeKubernetes}
	spec := crdRefreshSpecs[0] // scaledobjects.keda.sh

	// Forbidden → degradation skip keyed by the CRD short name.
	forbidden := &fakeK8sAdapter{
		errResource: spec.Resource,
		err:         fmt.Errorf("list %s: %w", spec.Resource, apierrors.NewForbidden(schema.GroupResource{Group: "keda.sh", Resource: "scaledobjects"}, "", errors.New("nope"))),
	}
	_, _, skip := r.refreshCRDSpec(context.Background(), src, forbidden, spec, time.Time{})
	if skip == nil {
		t.Fatal("a forbidden CRD list must produce a degradation skip")
	}
	if skip.Type != crdShortName(spec.Resource) {
		t.Fatalf("skip type = %q, want %q", skip.Type, crdShortName(spec.Resource))
	}

	// Not-found (uninstalled CRD) → silent, no skip.
	missing := &fakeK8sAdapter{
		errResource: spec.Resource,
		err:         fmt.Errorf("list %s: %w", spec.Resource, apierrors.NewNotFound(schema.GroupResource{Group: "keda.sh", Resource: "scaledobjects"}, "")),
	}
	if _, _, skip := r.refreshCRDSpec(context.Background(), src, missing, spec, time.Time{}); skip != nil {
		t.Fatalf("an uninstalled CRD must NOT be a degradation skip, got: %#v", skip)
	}

	// Any other error → tolerant, no skip.
	other := &fakeK8sAdapter{
		errResource: spec.Resource,
		err:         errors.New("connection reset"),
	}
	if _, _, skip := r.refreshCRDSpec(context.Background(), src, other, spec, time.Time{}); skip != nil {
		t.Fatalf("a non-forbidden CRD error must NOT be a degradation skip, got: %#v", skip)
	}
}

// TestSecretNodeMetadataOnlyKeyNames is the SITE-CLAIMS pinning test for the
// published claim "the graph only ever records secret key names and object
// metadata, never secret values". buildK8sMetadata extracts only the data map's
// KEY NAMES for a secret; this asserts the value bytes never enter the node.
func TestSecretNodeMetadataOnlyKeyNames(t *testing.T) {
	// makeSecret plants data {"BAR": "YmF6"} — key BAR, base64 value "YmF6".
	secret := makeSecret("apps", "app-secret")
	meta := buildK8sMetadata(&secret, "secret", "apps")

	keys, ok := meta["data_keys"].([]string)
	if !ok {
		t.Fatalf("secret metadata must carry data_keys as []string, got: %T", meta["data_keys"])
	}
	if len(keys) != 1 || keys[0] != "BAR" {
		t.Fatalf("data_keys = %v, want [BAR] (key name only)", keys)
	}
	// The raw value must not appear anywhere in the metadata.
	if s := fmt.Sprintf("%v", meta); strings.Contains(s, "YmF6") {
		t.Fatalf("secret VALUE leaked into node metadata: %s", s)
	}
	if _, present := meta["data"]; present {
		t.Fatal("secret metadata must not carry a data map (values), only data_keys")
	}
}
