package runfindings

import (
	"time"

	"github.com/google/uuid"
)

// LabelInput is what labelling needs beyond the two finding sets.
type LabelInput struct {
	// Covered is the set of components this run's pass completed. A previous
	// finding not re-found on a covered component is resolved; on an uncovered
	// one it is unknown.
	Covered Set
	// RunID, ScopeKey and SessionID are stamped on the rows carried forward
	// for resolved and unknown findings, so every row of a run's set belongs
	// to that run.
	RunID     string
	ScopeKey  string
	SessionID string
	Now       time.Time
}

// LabelAgainstPrevious assigns each of this run's findings one of the four labels against the
// previous completed run on the same scope, and carries forward each previous
// finding this run did not re-find as a resolved or unknown row.
//
// The previous run is a reference for labelling, never for judgment: nothing
// here reads a previous finding's band, consequence or evidence into a current
// one, and a previous finding never suppresses a current one. "Previous
// findings" are the previous run's new, standing and unknown rows; its resolved
// rows were already closed and are not carried again.
//
// Every row returned belongs to this run. A resolved or unknown row keeps the
// previous row's evidence pointer — the turn where the finding was established
// — because that is still where the evidence is.
func LabelAgainstPrevious(current, previous []Finding, in LabelInput) []Finding {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	currentKeys := make(map[string]struct{}, len(current))
	for _, f := range current {
		currentKeys[f.Key] = struct{}{}
	}
	prevKeys := make(map[string]struct{}, len(previous))
	for _, f := range previous {
		if f.Label == LabelResolved {
			continue
		}
		prevKeys[f.Key] = struct{}{}
	}

	out := make([]Finding, 0, len(current)+len(previous))
	for _, f := range current {
		if _, ok := prevKeys[f.Key]; ok {
			f.Label = LabelStanding
		} else {
			f.Label = LabelNew
		}
		out = append(out, f)
	}
	for _, f := range previous {
		if f.Label == LabelResolved {
			continue
		}
		if _, ok := currentKeys[f.Key]; ok {
			continue
		}
		carried := f
		carried.ID = uuid.NewString()
		carried.RunID = in.RunID
		carried.ScopeKey = in.ScopeKey
		carried.SessionID = in.SessionID
		carried.CreatedAt = now
		if in.Covered != nil && in.Covered(f.SubjectComponentID) {
			carried.Label = LabelResolved
		} else {
			carried.Label = LabelUnknown
		}
		out = append(out, carried)
	}
	return out
}
