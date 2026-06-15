package coreagent

import (
	"context"
	"testing"

	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
)

// principalCapturingStore wraps a GraphStore and records the principal carried
// on the context at each write-path method ApplyGraphDelta invokes, so a test
// can assert that the agent:core principal reaches the graph-write half of the
// refresh loop via the one stamped boot context.
type principalCapturingStore struct {
	graph.GraphStore
	seen rbac.Principal
}

func (s *principalCapturingStore) AddNode(ctx context.Context, node graph.Node) error {
	s.seen = rbac.PrincipalFromContext(ctx)
	return s.GraphStore.AddNode(ctx, node)
}

// TestAgentCorePrincipalRidesRefreshContext is the A001-COREGOV CC-02
// acceptance test: the principal minted and stamped at the coreAgent.Start
// seam (cmd/joe/server.go) must reach BOTH halves of the refresh loop via the
// single stamped context — the read entry (refresh / refreshComponent) and an
// ApplyGraphDelta write call site.
func TestAgentCorePrincipalRidesRefreshContext(t *testing.T) {
	// Mint exactly as the boot path does: the canonical constructor, no
	// re-typed literal.
	want, err := rbac.AgentCorePrincipal()
	if err != nil {
		t.Fatalf("AgentCorePrincipal() error = %v", err)
	}
	if want != rbac.Principal("svc:agent:core") {
		t.Fatalf("AgentCorePrincipal() = %q, want svc:agent:core", want)
	}

	// Stamp the boot context exactly as cmd/joe/server.go wraps the context it
	// hands to coreAgent.Start.
	bootCtx := rbac.WithPrincipal(context.Background(), want)

	// Read half: Refresher.Start derives its loop context via
	// context.WithCancel(ctx); the read at refresh.go:172 runs under that
	// derived context. Mirror that derivation and assert the principal is
	// inherited — this is the inheritance the single stamp relies on.
	loopCtx, cancel := context.WithCancel(bootCtx)
	defer cancel()
	if got := rbac.PrincipalFromContext(loopCtx); got != want {
		t.Errorf("read half: PrincipalFromContext(loopCtx) = %q, want %q", got, want)
	}

	// Write half: drive the real ApplyGraphDelta with the same stamped context
	// and capture the principal it carries into the store write seam.
	base := setupGraphStore(t)
	capturing := &principalCapturingStore{GraphStore: base}
	delta := GraphDelta{
		NodesToUpsert: []graph.Node{
			{ID: "k8s/src-1/deployment/default/api", Type: "deployment", ComponentID: "src-1", Metadata: map[string]any{}},
		},
	}
	if err := ApplyGraphDelta(loopCtx, capturing, delta); err != nil {
		t.Fatalf("ApplyGraphDelta() error = %v", err)
	}
	if capturing.seen != want {
		t.Errorf("write half: principal at ApplyGraphDelta seam = %q, want %q", capturing.seen, want)
	}
}
