package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/autorun"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/runfindings"
)

// runFindingsPipeline is the wiring between the run side and the report side
// of the read-only Auto Joe build (joe-pm threads/autojoe-ro-run-build.md and
// threads/autojoe-ro-report-build.md): it implements autorun.FindingsPipeline
// over internal/runfindings, so a finished run's final prose becomes typed,
// persisted, labelled finding records and the findings section of its final
// turn, which the run service places above its limits section. It lives here
// rather than in either package so that runfindings never imports autorun and
// autorun never imports runfindings: the seam is one interface, and this is
// its one implementation.
//
// Every read it makes is a guarded read through the run path's own accessor
// as the run's principal — the context the service stamped carries
// svc:agent:core — so the pipeline sees exactly what the run saw. Nothing it
// writes enters any authorization decision (D-0167).
type runFindingsPipeline struct {
	repo   runfindings.Repository
	reader runfindings.GraphReader
	now    func() time.Time
}

// ErrNoFindingsBlock is returned when a completed run's final prose carries
// no joe-findings block. It is an error rather than an empty set on purpose:
// persisting zero findings would label every previous finding on the scope
// resolved, and a model that did not answer the contract has not resolved
// anything. The run service renders the prose unverified and records the
// error on the turn; nothing is persisted.
var ErrNoFindingsBlock = errors.New("the pass emitted no " + runfindings.FindingsFence + " block")

// Findings implements autorun.FindingsPipeline.
func (p *runFindingsPipeline) Findings(ctx context.Context, run *autorun.Run, answer string) (string, error) {
	if run == nil {
		return "", errors.New("findings pipeline: nil run")
	}
	if run.SessionID == "" {
		return "", errors.New("findings pipeline: run has no session")
	}
	block, rest, found := runfindings.SplitFindingsBlock(answer)
	if !found {
		return "", ErrNoFindingsBlock
	}
	cands, err := runfindings.ParseCandidates(block)
	if err != nil {
		return "", fmt.Errorf("findings pipeline: %w", err)
	}

	now := p.now().UTC()
	// The scope's identity is the resolved component set — two runs over one
	// selection at different times compare on the set each recorded
	// (docs/backlog/autojoe-ro.md § Activation) — not the selection key the
	// run record holds for its one-live-run-per-scope guard.
	scopeKey := runfindings.ScopeKey(run.ResolvedComponents)
	scope := runfindings.SetOf(run.ResolvedComponents...)

	current, dropped, err := runfindings.Establish(ctx, cands, runfindings.EstablishInput{
		Reader:    p.reader,
		Principal: rbac.PrincipalFromContext(ctx),
		Scope:     scope,
		RunID:     run.ID,
		ScopeKey:  scopeKey,
		SessionID: run.SessionID,
		Now:       now,
	})
	if err != nil {
		return "", fmt.Errorf("findings pipeline: %w", err)
	}

	// The previous completed run on the same scope, through its persisted
	// findings — read for labelling only, never as evidence.
	var previous []runfindings.Finding
	prev, err := p.repo.LatestSetForScope(ctx, scopeKey, now)
	if err != nil {
		return "", fmt.Errorf("findings pipeline: previous run: %w", err)
	}
	if prev != nil {
		previous, err = p.repo.ListByRun(ctx, prev.RunID)
		if err != nil {
			return "", fmt.Errorf("findings pipeline: previous findings: %w", err)
		}
	}
	labelled := runfindings.LabelAgainstPrevious(current, previous, runfindings.LabelInput{
		Covered:   runfindings.SetOf(run.Covered...),
		RunID:     run.ID,
		ScopeKey:  scopeKey,
		SessionID: run.SessionID,
		Now:       now,
	})
	if err := p.repo.Put(ctx, runfindings.FindingSet{
		RunID: run.ID, ScopeKey: scopeKey, SessionID: run.SessionID, CreatedAt: now,
	}, labelled); err != nil {
		return "", fmt.Errorf("findings pipeline: persist: %w", err)
	}

	section := runfindings.Render(runfindings.RenderInput{
		Findings:   labelled,
		Coverage:   runfindings.Coverage{Covered: run.Covered, NotCovered: run.NotCovered},
		StopReason: run.StopReason,
		Dropped:    dropped,
	})
	// The model's prose beside the block is session material nobody verified
	// (D-0140); it is kept, labelled, and apart from the list, so nothing in
	// the list exists only in it.
	if rest = strings.TrimSpace(rest); rest != "" {
		section = strings.TrimRight(section, "\n") + "\n\n## Pass notes (unverified)\n\n" + rest + "\n"
	}
	return section, nil
}
