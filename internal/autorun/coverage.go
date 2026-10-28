package autorun

import (
	"sort"

	"github.com/jaimegago/joe/internal/agentloop"
)

// coverage derives which components of the scope the pass read, from the tool
// calls the loop actually made. A component is covered when at least one tool
// call named it as component_id and returned without error. It is an
// agentloop.RunObserver, fed every step; the model never asserts coverage — a
// claim about what was looked at is checked against what was called.
//
// The derivation is deliberately conservative on the covered side: a call
// that errored (a denial, a timeout, a bad argument) looked at nothing, so it
// does not cover. Graph and registry tools that name no component cover
// nothing either; a component is covered by reading it, not by reading about
// it.
type coverage struct {
	scope   map[string]struct{}
	covered map[string]struct{}
}

func newCoverage(resolved []string) *coverage {
	c := &coverage{
		scope:   make(map[string]struct{}, len(resolved)),
		covered: make(map[string]struct{}),
	}
	for _, id := range resolved {
		c.scope[id] = struct{}{}
	}
	return c
}

// OnStep implements agentloop.RunObserver.
func (c *coverage) OnStep(step agentloop.StepRecord) {
	if len(step.ToolResults) == 0 {
		return
	}
	argsByID := make(map[string]map[string]any, len(step.LLMResponse.ToolCalls))
	for _, tc := range step.LLMResponse.ToolCalls {
		argsByID[tc.ID] = tc.Args
	}
	for _, r := range step.ToolResults {
		if r.Error != "" {
			continue
		}
		args := argsByID[r.ID]
		if args == nil {
			continue
		}
		id, _ := args["component_id"].(string)
		if id == "" {
			continue
		}
		if _, inScope := c.scope[id]; inScope {
			c.covered[id] = struct{}{}
		}
	}
}

// Partition returns the scope split into covered and not covered, each sorted.
func (c *coverage) Partition() (covered, notCovered []string) {
	covered, notCovered = []string{}, []string{}
	for id := range c.scope {
		if _, ok := c.covered[id]; ok {
			covered = append(covered, id)
		} else {
			notCovered = append(notCovered, id)
		}
	}
	sort.Strings(covered)
	sort.Strings(notCovered)
	return covered, notCovered
}
