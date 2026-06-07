// Package sessiongate implements the §C captain-session-owned mutation
// gate as a pure function. Phase 1 Change 8 — see
// docs/PHASE-0-SESSION-MODEL.md §C and docs/PHASE-1-DECOMPOSITION.md
// Change 8.
//
// Invariant 5 / §C2: this gate is session-model-owned and runs UPSTREAM
// of the unchanged RBAC + Safety pipeline. It does NOT delegate to RBAC
// and does NOT modify the security layer. The named structural guard is
// the import-closure test in import_guard_test.go — sessiongate must not
// import internal/rbac, directly or transitively.
//
// §C4 positional, not semantic: the Check function takes
// (ctx, repo, sessionID, principal, class). It must not grow parameters
// named sourceID, tool, blast, or radius — those would make the gate
// semantic (computing on what the mutation touches) rather than
// positional (which session it arrives from). The signature-pin guard
// in import_guard_test.go enforces this via go/ast.
//
// §C5 non-configurable floor: the gate is not gated on any config flag.
// The behavioral permutation test in Change 10's executor-wrapper tests
// enumerates every config permutation and asserts non-captain mutation
// in incident regime is refused in all of them.
//
// Change 10 wires this into joe's executor wrapper. Phase 1 ships
// the function callable but unwired.
package sessiongate

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/safety"
	"github.com/jaimegago/joe/internal/sessionmodel"
)

// Decision is the outcome of Check. When Allow is true, the request
// proceeds into the unchanged RBAC + Safety + execute pipeline. When
// Allow is false, the caller refuses the mutation and redirects via the
// §A4 finding/annotation path. CaptainSessionID names the session to
// redirect to:
//   - empty when the active incident has no attached captain
//     (pending_captain / §B2 null authority). The LLM-facing refusal
//     payload should surface this distinctly so the synthesis is "no
//     authority to mutate yet; wait for captain attach".
//   - non-empty when there IS a captain session and the mutation
//     should be redirected to it (the captain or someone in their
//     session must mutate, per §C1).
type Decision struct {
	Allow            bool
	CaptainSessionID string
}

// Check is the §C captain-session-owned mutation gate.
//
// Decision rules (in order):
//
//  1. class == ActionRead → Allow. Reads/discovery unaffected per
//     §A1 and §C1.
//  2. regime is normal → Allow. No captain outside incident regime
//     (§B4 / §R1).
//  3. regime is incident:
//     - find the active incident session (incident_state ∉
//     {resolved, reviewed}).
//     - if mutating session ≠ active incident session → refuse,
//     redirect to active incident session.
//     - if active incident has no captain attached (pending_captain) →
//     refuse with empty CaptainSessionID (§B2 null authority).
//     - if principal ≠ current captain's principal → refuse, redirect
//     to active incident session.
//     - otherwise → Allow.
//
// The §B1 principal-threading rule is enforced here: the only principal
// that may mutate via the captain session is the captain's principal
// itself. Change 10's executor wrapper additionally SUBSTITUTES that
// principal into the downstream RBAC IsAllowed call.
func Check(
	ctx context.Context,
	repo sessionmodel.Repository,
	sessionID string,
	principal string,
	class safety.ActionClass,
) (Decision, error) {
	// 1. Reads/discovery always permitted.
	if class == safety.ActionRead {
		return Decision{Allow: true}, nil
	}

	// 2. Regime check.
	regime, err := repo.GetRegime(ctx)
	if err != nil {
		return Decision{}, fmt.Errorf("sessiongate: read regime: %w", err)
	}
	if regime == nil || regime.Mode == sessionmodel.RegimeModeNormal {
		return Decision{Allow: true}, nil
	}

	// 3. Incident regime: locate the active incident session.
	// "Active" = type=incident AND incident_state NOT IN (resolved, reviewed).
	// Phase 1 has at most one active by construction (declare creates
	// exactly one; resolve clears regime to normal). If we find zero
	// in incident regime, the system is in an inconsistent state
	// (regime says incident, no active session). §R7 says this
	// shouldn't happen; we fail closed.
	active, err := findActiveIncident(ctx, repo)
	if err != nil {
		return Decision{}, err
	}
	if active == nil {
		// Defensive: regime mismatch. Refuse without a redirect target.
		return Decision{Allow: false}, nil
	}

	// 3a. Mutating session must BE the active incident session.
	if sessionID != active.ID {
		return Decision{Allow: false, CaptainSessionID: active.ID}, nil
	}

	// 3b. Check captain attached and principal matches.
	captainPrincipal, ok, err := repo.CurrentCaptainPrincipal(ctx, active.ID)
	if err != nil {
		return Decision{}, fmt.Errorf("sessiongate: lookup captain: %w", err)
	}
	if !ok {
		// pending_captain — §B2 null authority. Empty CaptainSessionID
		// communicates "there is no captain yet; the redirect target
		// is the future captain, not a different session".
		return Decision{Allow: false}, nil
	}
	if principal != captainPrincipal {
		// Mutation from inside the incident session by a non-captain
		// principal (e.g., an observer attempting to mutate). Redirect
		// to the captain session so the synthesis lands as a finding.
		return Decision{Allow: false, CaptainSessionID: active.ID}, nil
	}

	return Decision{Allow: true}, nil
}

// findActiveIncident returns the currently-active incident session, or
// nil if no active incident exists. "Active" excludes resolved/reviewed.
// Phase 1 has at most one active incident by construction.
func findActiveIncident(ctx context.Context, repo sessionmodel.Repository) (*sessionmodel.AgentSession, error) {
	incidents, err := repo.ListSessionsByType(ctx, sessionmodel.SessionTypeIncident)
	if err != nil {
		return nil, fmt.Errorf("sessiongate: list incidents: %w", err)
	}
	for i := range incidents {
		s := &incidents[i]
		if s.IncidentState == nil {
			continue
		}
		switch *s.IncidentState {
		case sessionmodel.IncidentStateResolved, sessionmodel.IncidentStateReviewed:
			// Terminal — not active.
			continue
		default:
			return s, nil
		}
	}
	return nil, nil
}
