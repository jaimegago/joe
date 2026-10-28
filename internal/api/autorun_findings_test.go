package api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/access"
	"github.com/jaimegago/joe/internal/autorun"
	"github.com/jaimegago/joe/internal/graph"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/runfindings"
	"github.com/jaimegago/joe/internal/sessionmodel"
	"github.com/jaimegago/joe/internal/store"
)

// pipelineFixture is a migrated store with a three-node graph (an ingress
// fronting a service that stores in a database), a permit-all accessor over
// it, the run-findings repository, and the pipeline under test.
type pipelineFixture struct {
	t        *testing.T
	s        *store.Store
	repo     *runfindings.SQLRepository
	pipeline *runFindingsPipeline
	now      time.Time
}

func newPipelineFixture(t *testing.T) *pipelineFixture {
	t.Helper()
	s, err := store.New(store.DatabaseConfig{Driver: store.DriverSQLite, DSN: ":memory:"}, nil)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	g := graph.NewSQLiteStore(s.DB(), nil)
	ctx := context.Background()
	for _, n := range []graph.Node{
		{ID: "k8s/default/ingress-a", Type: "ingress", ComponentID: "prod-k8s", Metadata: map[string]any{}},
		{ID: "k8s/default/svc-a", Type: "service", ComponentID: "prod-k8s", Metadata: map[string]any{}},
		{ID: "pg/primary", Type: "database", ComponentID: "prod-pg", Metadata: map[string]any{}},
		{ID: "mon/prom", Type: "prometheus", ComponentID: "mon", Metadata: map[string]any{}},
	} {
		if err := g.AddNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []graph.Edge{
		{From: "k8s/default/ingress-a", To: "k8s/default/svc-a", Relation: graph.RelationIngressFor, Confidence: graph.Explicit},
		{From: "k8s/default/svc-a", To: "pg/primary", Relation: graph.RelationStoresIn, Confidence: graph.Explicit},
	} {
		if err := g.AddEdge(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	f := &pipelineFixture{t: t, s: s, repo: runfindings.NewRepository(s.DB(), s.Driver()),
		now: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
	f.pipeline = &runFindingsPipeline{
		repo:   f.repo,
		reader: access.New(nil, g, nil, nil),
		now:    func() time.Time { return f.now },
	}
	return f
}

func (f *pipelineFixture) run(state string) *autorun.Run {
	f.t.Helper()
	sess := uuid.NewString()
	repo := sessionmodel.NewRepository(f.s.DB(), f.s.Driver())
	if _, err := repo.CreateSession(context.Background(), sessionmodel.AgentSession{
		ID: sess, Type: sessionmodel.SessionTypeDefault, CreatorPrincipal: "svc:agent:core",
	}); err != nil {
		f.t.Fatalf("create session: %v", err)
	}
	return &autorun.Run{
		ID:                 uuid.NewString(),
		Principal:          "svc:agent:core",
		ResolvedComponents: []string{"prod-k8s", "prod-pg"},
		Covered:            []string{"prod-k8s", "prod-pg"},
		State:              state,
		SessionID:          sess,
	}
}

func (f *pipelineFixture) ctx() context.Context {
	return rbac.WithPrincipal(context.Background(), rbac.Principal("svc:agent:core"))
}

const answerWithBlock = "I took both components.\n\n```joe-findings\n" +
	`{"findings": [
	  {"type": "issue", "subject_node_id": "pg/primary", "claim": "the primary is not accepting connections", "consequence": "unavailable", "evidence_line": "pg_isready: no response",
	   "proposal": {"shape": "operator_action", "target_node_id": "pg/primary", "target_system_id": "primary", "base": "postgres process absent", "change": "restart postgres", "expected_effect": "pg_isready accepts", "limits": "disk not checked"}},
	  {"type": "risk", "subject_node_id": "k8s/default/svc-a", "claim": "svc-a runs a single replica", "consequence": "would_be_unavailable", "condition": "a node drain takes the only pod down", "evidence_line": "replicas: 1"},
	  {"type": "graph_update", "subject_node_id": "k8s/default/svc-a", "claim": "svc-a references a redis no component covers", "evidence_line": "REDIS_URL set",
	   "proposal": {"shape": "operator_action", "target_node_id": "k8s/default/svc-a", "target_system_id": "cache:6379", "base": "no redis component", "change": "register it", "expected_effect": "edge resolves", "limits": "type may be blocked"}},
	  {"type": "issue", "subject_node_id": "mon/prom", "claim": "outside the scope", "consequence": "degraded", "evidence_line": "x"}
	]}` + "\n```\n\nThat is all."

func TestRunFindingsPipeline_EstablishesLabelsPersistsAndRenders(t *testing.T) {
	f := newPipelineFixture(t)
	run := f.run(autorun.StateCompleted)

	section, err := f.pipeline.Findings(f.ctx(), run, answerWithBlock)
	if err != nil {
		t.Fatal(err)
	}
	// Rendered from the records: the P0 issue (reach 2 raised it from P1),
	// the risk, the graph update apart, the out-of-scope candidate dropped,
	// the prose kept apart and labelled.
	for _, want := range []string{
		"## Findings", "### P0", "[new] issue** `pg/primary`", "[new] risk** `k8s/default/svc-a`",
		"## Proposed admissions", "redis no component covers",
		"Not emitted: 1 candidate finding(s)", "## Pass notes (unverified)", "That is all.",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("section lacks %q:\n%s", want, section)
		}
	}
	if strings.Contains(section, "outside the scope") {
		t.Error("an out-of-scope candidate was rendered")
	}
	if !strings.Contains(section, "## Pass notes") || strings.Index(section, "## Pass notes") < strings.Index(section, "## Proposed admissions") {
		t.Error("prose is not apart from and after the lists")
	}

	// Persisted, under the resolved set's scope key, with labels.
	scopeKey := runfindings.ScopeKey(run.ResolvedComponents)
	set, err := f.repo.GetSet(context.Background(), run.ID)
	if err != nil || set == nil || set.ScopeKey != scopeKey || set.FindingCount != 3 || set.SessionID != run.SessionID {
		t.Fatalf("set = %+v, %v", set, err)
	}
	got, err := f.repo.ListByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Band != runfindings.BandP0 || got[0].Label != runfindings.LabelNew || got[0].ReachCount != 2 {
		t.Errorf("first record = %+v", got[0])
	}
	if got[0].Proposal == nil || got[0].Proposal.Target.ComponentID != "prod-pg" {
		t.Errorf("proposal not established: %+v", got[0].Proposal)
	}
}

func TestRunFindingsPipeline_LabelsAgainstThePreviousRunOnTheScope(t *testing.T) {
	f := newPipelineFixture(t)
	first := f.run(autorun.StateCompleted)
	if _, err := f.pipeline.Findings(f.ctx(), first, answerWithBlock); err != nil {
		t.Fatal(err)
	}

	// The next run, a day later, re-finds the risk, not the issue, and did
	// not cover the database's component.
	f.now = f.now.Add(24 * time.Hour)
	second := f.run(autorun.StateCompleted)
	second.Covered = []string{"prod-k8s"}
	second.NotCovered = []string{"prod-pg"}
	second.StopReason = "token_ceiling"
	answer := "```joe-findings\n" + `{"findings": [
	  {"type": "risk", "subject_node_id": "k8s/default/svc-a", "claim": "svc-a runs a single replica", "consequence": "would_be_unavailable", "condition": "a node drain takes the only pod down", "evidence_line": "replicas: 1"}
	]}` + "\n```"
	section, err := f.pipeline.Findings(f.ctx(), second, answer)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.repo.ListByRun(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]runfindings.Label{}
	for _, g := range got {
		labels[g.Claim] = g.Label
	}
	want := map[string]runfindings.Label{
		"svc-a runs a single replica":                  runfindings.LabelStanding,
		"the primary is not accepting connections":     runfindings.LabelUnknown,  // not covered this run
		"svc-a references a redis no component covers": runfindings.LabelResolved, // covered, not re-found
	}
	for claim, l := range want {
		if labels[claim] != l {
			t.Errorf("%q labelled %q, want %q", claim, labels[claim], l)
		}
	}
	if !strings.Contains(section, "did not cover: prod-pg") || !strings.Contains(section, "token_ceiling") {
		t.Errorf("coverage and stop reason not rendered:\n%s", section)
	}
	// The unknown row keeps the previous run's evidence pointer.
	for _, g := range got {
		if g.Label == runfindings.LabelUnknown && g.EvidenceSessionID != first.SessionID {
			t.Errorf("unknown row evidence = %s, want the first run's session", g.EvidenceSessionID)
		}
	}
	// A third run that covers everything and re-finds nothing resolves the
	// unknown one: the next run is the reconciler.
	f.now = f.now.Add(24 * time.Hour)
	third := f.run(autorun.StateCompleted)
	if _, err := f.pipeline.Findings(f.ctx(), third, "```joe-findings\n{\"findings\": []}\n```"); err != nil {
		t.Fatal(err)
	}
	got, _ = f.repo.ListByRun(context.Background(), third.ID)
	for _, g := range got {
		if g.Label != runfindings.LabelResolved {
			t.Errorf("third run: %q labelled %q, want resolved", g.Claim, g.Label)
		}
	}
}

func TestRunFindingsPipeline_NoBlockIsAnErrorAndPersistsNothing(t *testing.T) {
	f := newPipelineFixture(t)
	run := f.run(autorun.StateCompleted)
	_, err := f.pipeline.Findings(f.ctx(), run, "I looked around and everything seems fine.")
	if !errors.Is(err, ErrNoFindingsBlock) {
		t.Fatalf("err = %v, want ErrNoFindingsBlock", err)
	}
	if set, _ := f.repo.GetSet(context.Background(), run.ID); set != nil {
		t.Error("a set was persisted for a run that emitted no block")
	}
	// An unreadable block is an error too, not an empty list.
	if _, err := f.pipeline.Findings(f.ctx(), run, "```joe-findings\nnot json\n```"); err == nil {
		t.Error("unreadable block accepted")
	}
	if set, _ := f.repo.GetSet(context.Background(), run.ID); set != nil {
		t.Error("a set was persisted for an unreadable block")
	}
	// A run without a session has nowhere to point evidence at.
	run.SessionID = ""
	if _, err := f.pipeline.Findings(f.ctx(), run, answerWithBlock); err == nil {
		t.Error("run without a session accepted")
	}
}
