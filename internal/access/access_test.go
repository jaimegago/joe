package access_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/adapters"
	gitadapter "github.com/jaimegago/joe/internal/adapters/git"
	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	lokiadapter "github.com/jaimegago/joe/internal/adapters/observability/loki"
	prometheusadapter "github.com/jaimegago/joe/internal/adapters/observability/prometheus"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/store"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// --- fake RBAC repository (PolicyEngine only reads three methods) ---

type fakeRepo struct {
	zones       map[string]rbac.Zone
	assignments map[string]string   // sourceID -> zoneID
	policies    map[string][]string // principal -> []zoneID
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		zones: map[string]rbac.Zone{
			"z-read":     {ID: "z-read", AllowedActions: []rbac.Action{rbac.ActionRead, rbac.ActionQuery}},
			"z-write":    {ID: "z-write", AllowedActions: []rbac.Action{rbac.ActionRead, rbac.ActionQuery, rbac.ActionMutate}},
			"unassigned": {ID: "unassigned", AllowedActions: []rbac.Action{rbac.ActionRead}},
		},
		assignments: map[string]string{},
		policies:    map[string][]string{},
	}
}

func (f *fakeRepo) grant(principal, zoneID string) {
	f.policies[principal] = append(f.policies[principal], zoneID)
}
func (f *fakeRepo) assign(sourceID, zoneID string) { f.assignments[sourceID] = zoneID }

func (f *fakeRepo) GetAssignment(_ context.Context, sourceID string) (*rbac.SourceZoneAssignment, error) {
	z, ok := f.assignments[sourceID]
	if !ok {
		return nil, nil
	}
	return &rbac.SourceZoneAssignment{SourceID: sourceID, ZoneID: z}, nil
}
func (f *fakeRepo) GetZone(_ context.Context, id string) (*rbac.Zone, error) {
	z, ok := f.zones[id]
	if !ok {
		return nil, nil
	}
	return &z, nil
}
func (f *fakeRepo) ListPoliciesForPrincipal(_ context.Context, principal string) ([]rbac.Policy, error) {
	out := make([]rbac.Policy, 0)
	for _, z := range f.policies[principal] {
		out = append(out, rbac.Policy{Principal: principal, ZoneID: z})
	}
	return out, nil
}

// Unused-by-PolicyEngine methods (interface completeness).
func (f *fakeRepo) ListZones(context.Context) ([]rbac.Zone, error)            { return nil, nil }
func (f *fakeRepo) CreateZone(context.Context, rbac.Zone) (*rbac.Zone, error) { return nil, nil }
func (f *fakeRepo) ListAssignments(context.Context) ([]rbac.SourceZoneAssignment, error) {
	return nil, nil
}
func (f *fakeRepo) UpsertAssignment(context.Context, rbac.SourceZoneAssignment) error { return nil }
func (f *fakeRepo) ListPolicies(context.Context) ([]rbac.Policy, error)               { return nil, nil }
func (f *fakeRepo) CreatePolicy(context.Context, rbac.Policy) (*rbac.Policy, error)   { return nil, nil }
func (f *fakeRepo) DeletePolicy(context.Context, int64) error                         { return nil }
func (f *fakeRepo) DeletePolicyForPrincipalZone(context.Context, string, string) (int64, error) {
	return 0, nil
}
func (f *fakeRepo) ListUnassignedSourceIDs(context.Context) ([]string, error) { return nil, nil }

// --- fake adapters (one per acceptance kind), each records a call ---

type base struct{}

func (base) Connect(context.Context, store.Source) error { return nil }
func (base) Disconnect() error                           { return nil }
func (base) Status() adapters.Status                     { return adapters.Status{Connected: true} }

type fakeK8s struct {
	base
	called *bool
}

func (f fakeK8s) ListResources(context.Context, string, string) ([]unstructured.Unstructured, error) {
	*f.called = true
	return nil, nil
}
func (f fakeK8s) GetResource(context.Context, string, string, string) (*unstructured.Unstructured, error) {
	*f.called = true
	return nil, nil
}
func (f fakeK8s) GetPodLogs(context.Context, string, string, string, int) (string, error) {
	*f.called = true
	return "", nil
}

type fakeGit struct {
	base
	called *bool
}

func (f fakeGit) ReadFile(context.Context, string) (string, error) { *f.called = true; return "", nil }
func (f fakeGit) ListFiles(context.Context, string) ([]gitadapter.FileInfo, error) {
	*f.called = true
	return nil, nil
}
func (f fakeGit) Log(context.Context, int) ([]gitadapter.CommitInfo, error) {
	*f.called = true
	return nil, nil
}
func (f fakeGit) Diff(context.Context, string, string) (string, error) {
	*f.called = true
	return "", nil
}

type fakeProm struct {
	base
	called *bool
}

func (f fakeProm) Query(context.Context, string, time.Time) (*prometheusadapter.QueryResult, error) {
	*f.called = true
	return nil, nil
}
func (f fakeProm) QueryRange(context.Context, string, time.Time, time.Time, time.Duration) (*prometheusadapter.QueryResult, error) {
	*f.called = true
	return nil, nil
}
func (f fakeProm) Targets(context.Context) ([]prometheusadapter.Target, error) {
	*f.called = true
	return nil, nil
}

type fakeLoki struct {
	base
	called *bool
}

func (f fakeLoki) Query(context.Context, string, int, time.Duration) (*lokiadapter.QueryResult, error) {
	*f.called = true
	return nil, nil
}
func (f fakeLoki) QueryRange(context.Context, string, time.Time, time.Time, int) (*lokiadapter.QueryResult, error) {
	*f.called = true
	return nil, nil
}
func (f fakeLoki) ListServices(context.Context) ([]string, error) { *f.called = true; return nil, nil }

type fakeArgo struct {
	base
	called *bool
}

func (f fakeArgo) Apps(context.Context, string) ([]argocdadapter.App, error) {
	*f.called = true
	return nil, nil
}
func (f fakeArgo) GetApp(context.Context, string) (*argocdadapter.AppDetail, error) {
	*f.called = true
	return nil, nil
}
func (f fakeArgo) GetDiff(context.Context, string) (*argocdadapter.Diff, error) {
	*f.called = true
	return nil, nil
}
func (f fakeArgo) GetHistory(context.Context, string, int) ([]argocdadapter.SyncOperation, error) {
	*f.called = true
	return nil, nil
}

// fakeGraph records whether any method was called.
type fakeGraph struct{ called *bool }

func (f fakeGraph) AddNode(context.Context, graph.Node) error { *f.called = true; return nil }
func (f fakeGraph) AddEdge(context.Context, graph.Edge) error { *f.called = true; return nil }
func (f fakeGraph) GetNode(context.Context, string) (*graph.Node, error) {
	*f.called = true
	return nil, nil
}
func (f fakeGraph) Query(context.Context, string) ([]graph.Node, error) {
	*f.called = true
	return nil, nil
}
func (f fakeGraph) Related(context.Context, string, int) (*graph.Subgraph, error) {
	*f.called = true
	return &graph.Subgraph{}, nil
}
func (f fakeGraph) Path(context.Context, string, string) ([]graph.Edge, error) {
	*f.called = true
	return nil, nil
}
func (f fakeGraph) DeleteNode(context.Context, string) error { *f.called = true; return nil }
func (f fakeGraph) DeleteEdge(context.Context, string, string, string) error {
	*f.called = true
	return nil
}
func (f fakeGraph) Summary(context.Context) (graph.GraphSummary, error) {
	*f.called = true
	return graph.GraphSummary{}, nil
}
func (f fakeGraph) ListNodesBySource(context.Context, string) ([]graph.Node, error) {
	*f.called = true
	return nil, nil
}
func (f fakeGraph) ListEdgesForNodes(context.Context, []string) ([]graph.Edge, error) {
	*f.called = true
	return nil, nil
}
func (f fakeGraph) ListAll(context.Context) (*graph.Subgraph, error) {
	*f.called = true
	return &graph.Subgraph{}, nil
}

const (
	allowed = "alice" // granted z-read
	denied  = "mallory"
)

// dispatch invokes one accessor method for a kind, returning its error.
type kindCase struct {
	name     string
	sourceID string
	register func(reg *adapters.Registry, called *bool)
	invoke   func(a *access.Accessor, principal rbac.Principal) error
}

func acceptanceKinds() []kindCase {
	return []kindCase{
		{
			name:     "k8s",
			sourceID: "s-k8s",
			register: func(reg *adapters.Registry, c *bool) { reg.Register("s-k8s", fakeK8s{called: c}) },
			invoke: func(a *access.Accessor, p rbac.Principal) error {
				_, err := a.K8sListResources(context.Background(), p, "s-k8s", "pods", "")
				return err
			},
		},
		{
			name:     "prometheus",
			sourceID: "s-prom",
			register: func(reg *adapters.Registry, c *bool) { reg.Register("s-prom", fakeProm{called: c}) },
			invoke: func(a *access.Accessor, p rbac.Principal) error {
				_, err := a.PrometheusQuery(context.Background(), p, "s-prom", "up", time.Time{})
				return err
			},
		},
		{
			name:     "git",
			sourceID: "s-git",
			register: func(reg *adapters.Registry, c *bool) { reg.Register("s-git", fakeGit{called: c}) },
			invoke: func(a *access.Accessor, p rbac.Principal) error {
				_, err := a.GitReadFile(context.Background(), p, "s-git", "README.md")
				return err
			},
		},
		{
			name:     "argocd",
			sourceID: "s-argo",
			register: func(reg *adapters.Registry, c *bool) { reg.Register("s-argo", fakeArgo{called: c}) },
			invoke: func(a *access.Accessor, p rbac.Principal) error {
				_, err := a.ArgoCDApps(context.Background(), p, "s-argo", "")
				return err
			},
		},
		{
			name:     "loki",
			sourceID: "s-loki",
			register: func(reg *adapters.Registry, c *bool) { reg.Register("s-loki", fakeLoki{called: c}) },
			invoke: func(a *access.Accessor, p rbac.Principal) error {
				_, err := a.LokiQuery(context.Background(), p, "s-loki", "{}", 10, time.Hour)
				return err
			},
		},
	}
}

func TestAccessor_PerKind_AllowAndDeny(t *testing.T) {
	for _, kc := range acceptanceKinds() {
		kc := kc
		t.Run(kc.name, func(t *testing.T) {
			// Allowed principal granted a zone the source belongs to.
			repo := newFakeRepo()
			repo.assign(kc.sourceID, "z-read")
			repo.grant(allowed, "z-read")
			engine := rbac.NewPolicyEngine(repo)

			var calledAllow bool
			reg := adapters.NewRegistry()
			kc.register(reg, &calledAllow)
			a := access.New(reg, nil, engine)

			if err := kc.invoke(a, rbac.Principal(allowed)); err != nil {
				t.Fatalf("%s: granted principal should be allowed, got error: %v", kc.name, err)
			}
			if !calledAllow {
				t.Fatalf("%s: allowed call did not reach the adapter", kc.name)
			}

			// Denied principal with no matching grant.
			var calledDeny bool
			reg2 := adapters.NewRegistry()
			kc.register(reg2, &calledDeny)
			a2 := access.New(reg2, nil, engine)

			err := kc.invoke(a2, rbac.Principal(denied))
			if !errors.Is(err, access.ErrPermissionDenied) {
				t.Fatalf("%s: ungranted principal should be denied with ErrPermissionDenied, got: %v", kc.name, err)
			}
			if calledDeny {
				t.Fatalf("%s: adapter was called despite a denied decision — no infra call must occur on deny", kc.name)
			}
		})
	}
}

func TestAccessor_Graph_AllowAndDeny(t *testing.T) {
	repo := newFakeRepo()
	repo.assign(access.GraphSourceID, "z-read")
	repo.grant(allowed, "z-read")
	engine := rbac.NewPolicyEngine(repo)

	// Allowed.
	var calledAllow bool
	a := access.New(adapters.NewRegistry(), fakeGraph{called: &calledAllow}, engine)
	if _, err := a.GraphQuery(context.Background(), rbac.Principal(allowed), "svc"); err != nil {
		t.Fatalf("granted principal should query the graph, got: %v", err)
	}
	if !calledAllow {
		t.Fatal("allowed graph query did not reach the store")
	}

	// Denied — and the store must not be touched.
	var calledDeny bool
	a2 := access.New(adapters.NewRegistry(), fakeGraph{called: &calledDeny}, engine)
	if _, err := a2.GraphQuery(context.Background(), rbac.Principal(denied), "svc"); !errors.Is(err, access.ErrPermissionDenied) {
		t.Fatalf("ungranted principal should be denied, got: %v", err)
	}
	if calledDeny {
		t.Fatal("graph store was queried despite a denied decision")
	}
}

// TestAccessor_NilEngine_PermitsAll verifies that a nil policy engine (RBAC
// disabled, mirroring the HTTP transport's EnforcementMiddleware(nil)) permits
// every decision — the behaviour-preserving default for local/dev setups.
func TestAccessor_NilEngine_PermitsAll(t *testing.T) {
	var called bool
	reg := adapters.NewRegistry()
	reg.Register("s-k8s", fakeK8s{called: &called})
	a := access.New(reg, nil, nil) // nil engine

	if _, err := a.K8sListResources(context.Background(), rbac.Principal("anyone"), "s-k8s", "pods", ""); err != nil {
		t.Fatalf("nil engine should permit, got: %v", err)
	}
	if !called {
		t.Fatal("nil-engine call did not reach the adapter")
	}
}
