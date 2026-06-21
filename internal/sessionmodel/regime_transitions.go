package sessionmodel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/jaimegago/joe/internal/store"
)

// DeclareIncidentRegime is the single-transaction (§R7) declare path:
// PROMOTES an existing 'default' session IN PLACE to the incident master
// (§12.3) — it does NOT mint a fresh incident row. In one transaction it
// flips the designated session's type to 'incident', sets incident_state to
// 'declared', flips system_regime to incident, and attaches the declaring
// principal as captain. The promoted session keeps its original id and its
// original creator_principal, so the creator (owner) and the captain
// (declarer) may differ (§12.3 "Consequence, recorded, accepted"). Calls
// into DeclareIncidentRegimeWithHook with no fault-injection hook.
func (r *SQLRepository) DeclareIncidentRegime(ctx context.Context, principal, sessionID string, declaredKind RegimeKind) (string, string, error) {
	return r.DeclareIncidentRegimeWithHook(ctx, principal, sessionID, declaredKind, nil)
}

// DeclareIncidentRegimeWithHook is the test seam used by the
// single-transaction rollback assertion. hookAfterCaptainAttach, if
// non-nil, is invoked AFTER the captain insert but BEFORE COMMIT. If it
// returns a non-nil error, the entire transaction rolls back. Lets tests
// prove the single-transaction property without needing a natural failure
// point post-captain-attach. Production code calls DeclareIncidentRegime
// (no hook); the only intended caller of this hooked variant is the
// rollback test in internal/api/regime_test.go.
func (r *SQLRepository) DeclareIncidentRegimeWithHook(
	ctx context.Context,
	principal, sessionID string,
	declaredKind RegimeKind,
	hookAfterCaptainAttach func(*sql.Tx) error,
) (promotedID, captainID string, err error) {
	if principal == "" {
		return "", "", fmt.Errorf("declare incident regime: principal required")
	}
	if sessionID == "" {
		return "", "", fmt.Errorf("declare incident regime: session id required (promote-in-place takes an existing session)")
	}
	if declaredKind == "" {
		declaredKind = RegimeKindHuman
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("declare incident regime: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			// Best-effort rollback. If rollback fails, the original err is
			// what the caller sees.
			_ = tx.Rollback()
		}
	}()

	// Precondition 1: regime must currently be normal. This is the
	// "no second concurrent incident" guard (§12.3 one global single-row
	// regime, one master incident session).
	var currentMode string
	if err = tx.QueryRowContext(ctx, `SELECT mode FROM system_regime WHERE id = 1`).Scan(&currentMode); err != nil {
		return "", "", fmt.Errorf("declare incident regime: read current regime: %w", err)
	}
	if RegimeMode(currentMode) != RegimeModeNormal {
		err = ErrRegimeAlreadyIncident
		return "", "", err
	}

	// Precondition 2: the designated session must exist and must not
	// already be an incident — a session is promoted at most once
	// (§12.3 promote-in-place, no double-promotion).
	var existingType string
	scanErr := tx.QueryRowContext(ctx, store.Rebind(r.driver,
		`SELECT type FROM agent_sessions WHERE id = ?`), sessionID).Scan(&existingType)
	if errors.Is(scanErr, sql.ErrNoRows) {
		err = ErrNotFound
		return "", "", err
	}
	if scanErr != nil {
		err = fmt.Errorf("declare incident regime: read target session: %w", scanErr)
		return "", "", err
	}
	if SessionType(existingType) == SessionTypeIncident {
		err = ErrSessionAlreadyIncident
		return "", "", err
	}

	// 1. Promote the session IN PLACE: type -> incident, incident_state ->
	// declared, and clear linked_incident_id. Clearing the link is required
	// by the migration-025 CHECK (linked_incident_id IS NULL OR type <>
	// 'incident'): a session that was participating in a (now-resolved)
	// incident sheds that pointer when it becomes a master incident itself.
	// creator_principal is left UNTOUCHED so the original owner is preserved
	// (§12.3 creator vs captain may differ). The WHERE guards type <>
	// 'incident' as belt-and-suspenders against a concurrent promote.
	now := time.Now().UTC()
	declared := string(IncidentStateDeclared)
	res, perr := tx.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_sessions
		SET type = ?, incident_state = ?, linked_incident_id = NULL,
		    last_activity_at = ?
		WHERE id = ? AND type <> 'incident'`),
		string(SessionTypeIncident), declared,
		now.Format(time.RFC3339), sessionID)
	if perr != nil {
		err = fmt.Errorf("declare incident regime: promote session: %w", perr)
		return "", "", err
	}
	// Defensive: a 0-row update would mean the session was promoted out from
	// under us between the SELECT and the UPDATE. Treat as already-incident.
	if n, aerr := res.RowsAffected(); aerr == nil && n == 0 {
		err = ErrSessionAlreadyIncident
		return "", "", err
	}

	// 2. Flip regime to incident.
	if _, err = tx.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE system_regime
		SET mode = ?, declared_at = ?, declared_by_principal = ?, declared_kind = ?
		WHERE id = 1`),
		string(RegimeModeIncident), now.Format(time.RFC3339),
		principal, string(declaredKind)); err != nil {
		return "", "", fmt.Errorf("declare incident regime: update regime: %w", err)
	}

	// 3. Attach declaring principal as captain (R-CAP1).
	// Seed last_seen_at = attached_at (§6-D: a fresh attach counts as
	// reachable).
	captainID = uuid.NewString()
	activeState := string(TransferStateActive)
	if _, err = tx.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO session_captains
			(id, session_id, captain_type, principal, attached_at, detached_at,
			 transfer_state, incoming_principal, transfer_initiator, last_seen_at)
		VALUES (?, ?, ?, ?, ?, NULL, ?, NULL, NULL, ?)`),
		captainID, sessionID, string(CaptainTypeHuman), principal,
		now.Format(time.RFC3339), activeState, now.Format(time.RFC3339)); err != nil {
		return "", "", fmt.Errorf("declare incident regime: attach captain: %w", err)
	}

	// Test seam: force a rollback to prove single-transaction property.
	if hookAfterCaptainAttach != nil {
		if hookErr := hookAfterCaptainAttach(tx); hookErr != nil {
			err = hookErr
			return "", "", err
		}
	}

	if err = tx.Commit(); err != nil {
		return "", "", fmt.Errorf("declare incident regime: commit: %w", err)
	}
	return sessionID, captainID, nil
}

// ResolveIncidentRegime is the SOLE production-code path that transitions
// system_regime back to 'normal'. The AST invariant guard in
// internal/api/regime_invariant_test.go asserts that calls to this method
// appear in exactly one production-code call site (the human-resolve
// handler in internal/api/regime.go) — the named structural guard for
// §R5 / Invariant 4 ("incident-mode exit may not be automated"). When the
// Change 12 autonomous-resolve seam lands, that seam returns 403 BEFORE
// calling this method (gated on the inert const seams.JoeAutonomousResolveEnabled),
// so the invariant guard continues to hold.
func (r *SQLRepository) ResolveIncidentRegime(ctx context.Context, principal string) (string, error) {
	return r.ResolveIncidentRegimeWithHook(ctx, principal, nil)
}

// ResolveIncidentRegimeWithHook is the test seam, same pattern as declare.
// Production code calls ResolveIncidentRegime (no hook).
func (r *SQLRepository) ResolveIncidentRegimeWithHook(
	ctx context.Context,
	principal string,
	hookAfterStateUpdate func(*sql.Tx) error,
) (sessionID string, err error) {
	if principal == "" {
		return "", fmt.Errorf("resolve incident regime: principal required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("resolve incident regime: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Precondition: regime must be incident.
	var currentMode string
	if err = tx.QueryRowContext(ctx, `SELECT mode FROM system_regime WHERE id = 1`).Scan(&currentMode); err != nil {
		return "", fmt.Errorf("resolve incident regime: read current regime: %w", err)
	}
	if RegimeMode(currentMode) != RegimeModeIncident {
		err = ErrRegimeNotIncident
		return "", err
	}

	// Find the active incident session and verify state is
	// 'believed_mitigated'. Active = type=incident AND incident_state NOT
	// IN ('resolved','reviewed'). Phase 1 has at most one active by
	// construction (declare creates exactly one), so we pick the most
	// recently created.
	var (
		activeID    string
		activeState string
	)
	err = tx.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT id, incident_state
		FROM agent_sessions
		WHERE type = 'incident'
		  AND incident_state NOT IN ('resolved', 'reviewed')
		ORDER BY created_at DESC
		LIMIT 1`)).Scan(&activeID, &activeState)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNoActiveIncident
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("resolve incident regime: find active incident: %w", err)
	}
	if IncidentState(activeState) != IncidentStateBelievedMitigated {
		err = ErrIncidentNotMitigated
		return "", err
	}

	// 1. Transition session state to 'resolved'.
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_sessions
		SET incident_state = ?, last_activity_at = ?
		WHERE id = ?`),
		string(IncidentStateResolved), now.Format(time.RFC3339), activeID); err != nil {
		return "", fmt.Errorf("resolve incident regime: update session state: %w", err)
	}

	if hookAfterStateUpdate != nil {
		if hookErr := hookAfterStateUpdate(tx); hookErr != nil {
			err = hookErr
			return "", err
		}
	}

	// 2. Detach the active captain of the resolving incident (resolve-half
	// of the no-auto-lapse captaincy model: captaincy ends only on explicit
	// transfer or on incident resolve — see D-0024). Without this, the
	// session_captains row would survive resolution with detached_at IS NULL,
	// so GetActiveCaptain / CurrentCaptainPrincipal would keep reporting a
	// phantom captain on a resolved incident. This runs inside the same tx as
	// the regime flip below so there is no observable intermediate state where
	// the regime is normal but a captain is still active (or vice versa).
	// Mirrors MarkCaptainDetached's SET but keyed by session_id so it clears
	// whichever active row exists; session_captains has no detach-reason
	// column, so no reason is recorded.
	if _, err = tx.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE session_captains
		SET detached_at = ?, transfer_state = NULL,
		    incoming_principal = NULL, transfer_initiator = NULL
		WHERE session_id = ? AND detached_at IS NULL`),
		now.Format(time.RFC3339), activeID); err != nil {
		return "", fmt.Errorf("resolve incident regime: detach captain: %w", err)
	}

	// 3. Clear regime to normal. This is the structurally-guarded UPDATE.
	if _, err = tx.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE system_regime
		SET mode = ?, declared_at = NULL, declared_by_principal = NULL, declared_kind = NULL
		WHERE id = 1`),
		string(RegimeModeNormal)); err != nil {
		return "", fmt.Errorf("resolve incident regime: clear regime: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return "", fmt.Errorf("resolve incident regime: commit: %w", err)
	}
	return activeID, nil
}
