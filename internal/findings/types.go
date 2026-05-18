// Package findings owns the §A4 cross-session attribution record:
// attributed, non-actionable synthesis messages a human posts into a
// target session's timeline. Annotation semantic only — no workflow,
// no queue, no accept/reject state.
//
// See docs/PHASE-0-SESSION-MODEL.md §A4 and §A6.
package findings

import "time"

// Finding is one row of the findings table.
//
// The three FKs to agent_sessions:
//
//   - SourceSessionID: where the author is currently working (the session
//     that hosted the synthesis act).
//   - TargetSessionID: the session this finding is posted into.
//   - ReferencedInvestigationSessionID: optional pointer to the
//     investigation that produced the underlying material (§A4: "a human
//     posts an attributed, non-actionable synthesis message into a target
//     session's timeline, optionally carrying a string reference to the
//     source investigation session"). The captain reads it and decides
//     whether to act — the normal captain-driven path (§A4).
//
// All three are §6-C ON DELETE CASCADE FKs in migration 011.
type Finding struct {
	ID                               string
	SourceSessionID                  string
	TargetSessionID                  string
	AuthorPrincipal                  string
	Body                             string
	PostedAt                         time.Time
	ReferencedInvestigationSessionID *string
}
