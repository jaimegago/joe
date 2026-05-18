// Package warnings owns the §E Joe-warnings surface — Joe's append-only,
// attributed, human-reviewable list of incident-judgments-it-is-not-
// authorized-to-act-on (§E1, §R3).
//
// Deliberately minimal (§E2 / R9): not a queue, not state-tracked, not
// self-escalating. The append-only invariant is structurally enforced by
// the Repository interface shape — see repository_test.go.
package warnings

import "time"

// Warning is one row of the joe_warnings table.
//
// SourceInvestigationSessionID is nullable: Joe may raise a warning
// without a corresponding investigation session. ReviewedAt and
// ReviewedByPrincipal track the human review act (the only mutation
// allowed on a warning row; see Repository.MarkReviewed).
type Warning struct {
	ID                           string
	RaisedAt                     time.Time
	SignalReference              string
	Body                         string
	SourceInvestigationSessionID *string
	ReviewedAt                   *time.Time
	ReviewedByPrincipal          *string
}
