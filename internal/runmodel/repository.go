package runmodel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// Repository is the durable interface for the §D run substrate.
//
// Idempotency-key surface (§D5 / Invariant 2): the only ways to write to
// tool_idempotency_keys are RecordToolIntent (creates an 'issued' row) and
// MarkToolCompleted / MarkToolFailed (transition an existing 'issued' row
// to a terminal status). Crucially, there is no method that records a
// completed-or-failed result without an already-issued key. This is the
// structural protection §D5 requires — write-result-without-prior-intent
// is not expressible against this interface, and a future contributor
// adding one would have to amend this interface visibly. The behavioral
// half of the contract (idempotent-on-duplicate; refuse-to-overwrite) is
// exercised in repository_test.go.
type Repository interface {
	// Runs

	CreateRun(ctx context.Context, r Run) (*Run, error)
	GetRun(ctx context.Context, id string) (*Run, error)
	ListRunsForSession(ctx context.Context, sessionID string) ([]Run, error)
	// UpdateRunState transitions a run's state and (if applicable) sets
	// ended_at. Phase 1 does not enforce the legal-transition matrix here;
	// that is Change 7's HTTP-layer responsibility.
	UpdateRunState(ctx context.Context, runID string, state RunState, endedAt *time.Time) error
	SetLastStepID(ctx context.Context, runID, stepID string) error

	// Steps

	AppendStep(ctx context.Context, s Step) (*Step, error)
	ListStepsForRun(ctx context.Context, runID string) ([]Step, error)

	// Solicitations

	OpenSolicitation(ctx context.Context, s Solicitation) (*Solicitation, error)
	// OpenSolicitationAwaitInput opens the solicitation AND transitions the run
	// running → awaiting_input in ONE transaction. The HTTP handler previously
	// issued these as two separate writes; if the state UPDATE failed after the
	// INSERT committed, the run stayed 'running' with a committed open
	// solicitation, and a client retry (the run-state gate still passing) would
	// mint a SECOND open solicitation for the same pause. Coupling the pair
	// makes the invariant atomic: either the solicitation exists and the run
	// awaits input, or neither.
	OpenSolicitationAwaitInput(ctx context.Context, s Solicitation) (*Solicitation, error)
	GetSolicitation(ctx context.Context, id string) (*Solicitation, error)
	ResolveSolicitation(ctx context.Context, id string, resolutionPayload string, resolvedAt time.Time) error

	// World handles

	RecordWorldHandle(ctx context.Context, h WorldHandle) (*WorldHandle, error)
	// RecordWorldHandleAwaitWorld records the world handle AND transitions the
	// run running → awaiting_world in ONE transaction — the same
	// atomic-pair rationale as OpenSolicitationAwaitInput (a committed handle
	// with the run still 'running' invites a duplicate on retry).
	RecordWorldHandleAwaitWorld(ctx context.Context, h WorldHandle) (*WorldHandle, error)
	GetWorldHandle(ctx context.Context, id string) (*WorldHandle, error)
	ListWorldHandlesForRun(ctx context.Context, runID string) ([]WorldHandle, error)
	ObserveWorldHandle(ctx context.Context, id string, observedState string, polledAt time.Time) error

	// Idempotency keys (§D5)
	//
	// RecordToolIntent persists an 'issued' key for a world-mutating tool
	// call BEFORE the call is issued. It is idempotent on duplicate key:
	// re-issuing the same key returns the existing row unchanged. This is
	// the structural property that lets a crash-and-resume re-attempt the
	// call without double-acting.
	RecordToolIntent(ctx context.Context, key, runID, toolName, argsHash string) (*IdempotencyKey, error)

	// MarkToolCompleted transitions an existing 'issued' key to 'completed'
	// with the given result. Returns an error if the key does not exist or
	// is already in a terminal status — the no-overwrite rule. A key that
	// never reached 'completed' (status stuck on 'issued' because the
	// process crashed mid-call) is allowed to re-attempt: callers may call
	// RecordToolIntent again with the same key, which returns the existing
	// row, and then proceed to MarkToolCompleted.
	MarkToolCompleted(ctx context.Context, key string, result string) error

	// MarkToolFailed transitions an existing 'issued' key to 'failed'.
	// Same no-overwrite rule as MarkToolCompleted.
	MarkToolFailed(ctx context.Context, key string, result string) error

	GetIdempotencyKey(ctx context.Context, key string) (*IdempotencyKey, error)

	// Action ledger (§D8)

	AppendLedger(ctx context.Context, e LedgerEntry) (*LedgerEntry, error)
	ListLedgerForRun(ctx context.Context, runID string) ([]LedgerEntry, error)
}

// ErrAlreadyTerminal is returned by MarkToolCompleted / MarkToolFailed when
// the key has already transitioned to a terminal status. The §D5 no-
// overwrite rule.
var ErrAlreadyTerminal = errors.New("runmodel: idempotency key already in terminal status")

// ErrNotFound is returned when a lookup finds no matching row.
var ErrNotFound = errors.New("runmodel: not found")

// SQLRepository implements Repository on top of *sql.DB, parallel to the
// sessionmodel.SQLRepository pattern. Uses ? placeholders + store.Rebind
// for cross-driver portability per §5b-6 / Invariant 6.
type SQLRepository struct {
	db     *sql.DB
	driver string
}

// NewRepository constructs a SQLRepository.
func NewRepository(db *sql.DB, driver string) *SQLRepository {
	return &SQLRepository{db: db, driver: driver}
}

// sqlExecer abstracts *sql.DB / *sql.Tx so a write helper can run either
// standalone on the pooled handle or inside a caller transaction (mirrors the
// sessionmodel seam of the same name).
type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// --- Runs ---

func (r *SQLRepository) CreateRun(ctx context.Context, run Run) (*Run, error) {
	if run.ID == "" {
		return nil, fmt.Errorf("create run: id required")
	}
	if run.SessionID == "" {
		return nil, fmt.Errorf("create run: session_id required")
	}
	if run.State == "" {
		run.State = RunStateRunning
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}

	var endedAt any
	if run.EndedAt != nil {
		endedAt = run.EndedAt.Format(time.RFC3339)
	}
	var lastStepID any
	if run.LastStepID != nil {
		lastStepID = *run.LastStepID
	}

	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO agent_runs (id, session_id, state, started_at, ended_at, last_step_id)
		VALUES (?, ?, ?, ?, ?, ?)`),
		run.ID, run.SessionID, string(run.State),
		run.StartedAt.Format(time.RFC3339), endedAt, lastStepID)
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	return &run, nil
}

func (r *SQLRepository) GetRun(ctx context.Context, id string) (*Run, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT id, session_id, state, started_at, ended_at, last_step_id
		FROM agent_runs WHERE id = ?`), id)
	return scanRun(row.Scan)
}

func (r *SQLRepository) ListRunsForSession(ctx context.Context, sessionID string) ([]Run, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT id, session_id, state, started_at, ended_at, last_step_id
		FROM agent_runs WHERE session_id = ? ORDER BY started_at`), sessionID)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var out []Run
	for rows.Next() {
		run, err := scanRun(rows.Scan)
		if err != nil {
			return nil, err
		}
		if run != nil {
			out = append(out, *run)
		}
	}
	return out, rows.Err()
}

func (r *SQLRepository) UpdateRunState(ctx context.Context, runID string, state RunState, endedAt *time.Time) error {
	return r.updateRunStateExec(ctx, r.db, runID, state, endedAt)
}

// updateRunStateExec is UpdateRunState on a caller-chosen executor (pooled
// handle or transaction), so the combined solicitation/world-handle + state
// transitions can run it inside their transaction.
func (r *SQLRepository) updateRunStateExec(ctx context.Context, exec sqlExecer, runID string, state RunState, endedAt *time.Time) error {
	var endedAtVal any
	if endedAt != nil {
		endedAtVal = endedAt.Format(time.RFC3339)
	}
	_, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_runs SET state = ?, ended_at = ? WHERE id = ?`),
		string(state), endedAtVal, runID)
	if err != nil {
		return fmt.Errorf("update run state: %w", err)
	}
	return nil
}

func (r *SQLRepository) SetLastStepID(ctx context.Context, runID, stepID string) error {
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE agent_runs SET last_step_id = ? WHERE id = ?`),
		stepID, runID)
	if err != nil {
		return fmt.Errorf("set last step: %w", err)
	}
	return nil
}

func scanRun(scan func(...any) error) (*Run, error) {
	var (
		run          Run
		state        string
		startedAtStr string
		endedAt      sql.NullString
		lastStepID   sql.NullString
	)
	err := scan(&run.ID, &run.SessionID, &state, &startedAtStr, &endedAt, &lastStepID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan run: %w", err)
	}
	run.State = RunState(state)
	run.StartedAt, _ = time.Parse(time.RFC3339, startedAtStr)
	if endedAt.Valid {
		t, _ := time.Parse(time.RFC3339, endedAt.String)
		run.EndedAt = &t
	}
	if lastStepID.Valid {
		run.LastStepID = &lastStepID.String
	}
	return &run, nil
}

// --- Steps ---

func (r *SQLRepository) AppendStep(ctx context.Context, s Step) (*Step, error) {
	if s.ID == "" {
		return nil, fmt.Errorf("append step: id required")
	}
	if s.RunID == "" {
		return nil, fmt.Errorf("append step: run_id required")
	}
	if s.PersistedAt.IsZero() {
		s.PersistedAt = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO run_steps (id, run_id, step_number, kind, payload, persisted_at)
		VALUES (?, ?, ?, ?, ?, ?)`),
		s.ID, s.RunID, s.StepNumber, string(s.Kind), s.Payload,
		s.PersistedAt.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("append step: %w", err)
	}
	return &s, nil
}

func (r *SQLRepository) ListStepsForRun(ctx context.Context, runID string) ([]Step, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT id, run_id, step_number, kind, payload, persisted_at
		FROM run_steps WHERE run_id = ? ORDER BY step_number`), runID)
	if err != nil {
		return nil, fmt.Errorf("list steps: %w", err)
	}
	defer rows.Close()

	var out []Step
	for rows.Next() {
		var (
			s              Step
			kind           string
			persistedAtStr string
		)
		if err := rows.Scan(&s.ID, &s.RunID, &s.StepNumber, &kind, &s.Payload, &persistedAtStr); err != nil {
			return nil, fmt.Errorf("scan step: %w", err)
		}
		s.Kind = StepKind(kind)
		s.PersistedAt, _ = time.Parse(time.RFC3339, persistedAtStr)
		out = append(out, s)
	}
	return out, rows.Err()
}

// --- Solicitations ---

func (r *SQLRepository) OpenSolicitation(ctx context.Context, s Solicitation) (*Solicitation, error) {
	return r.openSolicitationExec(ctx, r.db, s)
}

// OpenSolicitationAwaitInput couples the solicitation INSERT and the
// running → awaiting_input state UPDATE in one transaction (see the interface
// doc for why the pair must be atomic).
func (r *SQLRepository) OpenSolicitationAwaitInput(ctx context.Context, s Solicitation) (created *Solicitation, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("open solicitation: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if created, err = r.openSolicitationExec(ctx, tx, s); err != nil {
		return nil, err
	}
	if err = r.updateRunStateExec(ctx, tx, s.RunID, RunStateAwaitingInput, nil); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("open solicitation: commit: %w", err)
	}
	return created, nil
}

func (r *SQLRepository) openSolicitationExec(ctx context.Context, exec sqlExecer, s Solicitation) (*Solicitation, error) {
	if s.ID == "" {
		return nil, fmt.Errorf("open solicitation: id required")
	}
	if s.RunID == "" {
		return nil, fmt.Errorf("open solicitation: run_id required")
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	var livenessFlag any
	if s.LivenessFlag != nil {
		livenessFlag = string(*s.LivenessFlag)
	}
	_, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO run_solicitations
			(id, run_id, kind, payload, created_at, resolved_at, resolution_payload, liveness_flag)
		VALUES (?, ?, ?, ?, ?, NULL, NULL, ?)`),
		s.ID, s.RunID, string(s.Kind), s.Payload,
		s.CreatedAt.Format(time.RFC3339), livenessFlag)
	if err != nil {
		return nil, fmt.Errorf("open solicitation: %w", err)
	}
	return &s, nil
}

func (r *SQLRepository) GetSolicitation(ctx context.Context, id string) (*Solicitation, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT id, run_id, kind, payload, created_at, resolved_at, resolution_payload, liveness_flag
		FROM run_solicitations WHERE id = ?`), id)
	var (
		s                 Solicitation
		kind              string
		createdAtStr      string
		resolvedAt        sql.NullString
		resolutionPayload sql.NullString
		livenessFlag      sql.NullString
	)
	err := row.Scan(&s.ID, &s.RunID, &kind, &s.Payload, &createdAtStr,
		&resolvedAt, &resolutionPayload, &livenessFlag)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get solicitation: %w", err)
	}
	s.Kind = SolicitationKind(kind)
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	if resolvedAt.Valid {
		t, _ := time.Parse(time.RFC3339, resolvedAt.String)
		s.ResolvedAt = &t
	}
	if resolutionPayload.Valid {
		s.ResolutionPayload = &resolutionPayload.String
	}
	if livenessFlag.Valid {
		lf := LivenessFlag(livenessFlag.String)
		s.LivenessFlag = &lf
	}
	return &s, nil
}

func (r *SQLRepository) ResolveSolicitation(ctx context.Context, id string, resolutionPayload string, resolvedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE run_solicitations
		SET resolved_at = ?, resolution_payload = ?
		WHERE id = ?`),
		resolvedAt.Format(time.RFC3339), resolutionPayload, id)
	if err != nil {
		return fmt.Errorf("resolve solicitation: %w", err)
	}
	return nil
}

// --- World handles ---

func (r *SQLRepository) RecordWorldHandle(ctx context.Context, h WorldHandle) (*WorldHandle, error) {
	return r.recordWorldHandleExec(ctx, r.db, h)
}

// RecordWorldHandleAwaitWorld couples the world-handle INSERT and the
// running → awaiting_world state UPDATE in one transaction (see the interface
// doc for why the pair must be atomic).
func (r *SQLRepository) RecordWorldHandleAwaitWorld(ctx context.Context, h WorldHandle) (created *WorldHandle, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("record world handle: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if created, err = r.recordWorldHandleExec(ctx, tx, h); err != nil {
		return nil, err
	}
	if err = r.updateRunStateExec(ctx, tx, h.RunID, RunStateAwaitingWorld, nil); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("record world handle: commit: %w", err)
	}
	return created, nil
}

func (r *SQLRepository) recordWorldHandleExec(ctx context.Context, exec sqlExecer, h WorldHandle) (*WorldHandle, error) {
	if h.ID == "" {
		return nil, fmt.Errorf("record world handle: id required")
	}
	if h.RunID == "" {
		return nil, fmt.Errorf("record world handle: run_id required")
	}
	if h.RecordedAt.IsZero() {
		h.RecordedAt = time.Now().UTC()
	}
	_, err := exec.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO run_world_handles
			(id, run_id, locator, query_meta, recorded_at, last_poll_at, last_observed_state)
		VALUES (?, ?, ?, ?, ?, NULL, NULL)`),
		h.ID, h.RunID, h.Locator, h.QueryMeta,
		h.RecordedAt.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("record world handle: %w", err)
	}
	return &h, nil
}

func (r *SQLRepository) GetWorldHandle(ctx context.Context, id string) (*WorldHandle, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT id, run_id, locator, query_meta, recorded_at, last_poll_at, last_observed_state
		FROM run_world_handles WHERE id = ?`), id)
	var (
		h                 WorldHandle
		recordedAtStr     string
		lastPollAt        sql.NullString
		lastObservedState sql.NullString
	)
	err := row.Scan(&h.ID, &h.RunID, &h.Locator, &h.QueryMeta,
		&recordedAtStr, &lastPollAt, &lastObservedState)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get world handle: %w", err)
	}
	h.RecordedAt, _ = time.Parse(time.RFC3339, recordedAtStr)
	if lastPollAt.Valid {
		t, _ := time.Parse(time.RFC3339, lastPollAt.String)
		h.LastPollAt = &t
	}
	if lastObservedState.Valid {
		h.LastObservedState = &lastObservedState.String
	}
	return &h, nil
}

func (r *SQLRepository) ListWorldHandlesForRun(ctx context.Context, runID string) ([]WorldHandle, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT id, run_id, locator, query_meta, recorded_at, last_poll_at, last_observed_state
		FROM run_world_handles WHERE run_id = ? ORDER BY recorded_at`), runID)
	if err != nil {
		return nil, fmt.Errorf("list world handles: %w", err)
	}
	defer rows.Close()

	var out []WorldHandle
	for rows.Next() {
		var (
			h                 WorldHandle
			recordedAtStr     string
			lastPollAt        sql.NullString
			lastObservedState sql.NullString
		)
		if err := rows.Scan(&h.ID, &h.RunID, &h.Locator, &h.QueryMeta,
			&recordedAtStr, &lastPollAt, &lastObservedState); err != nil {
			return nil, fmt.Errorf("scan world handle: %w", err)
		}
		h.RecordedAt, _ = time.Parse(time.RFC3339, recordedAtStr)
		if lastPollAt.Valid {
			t, _ := time.Parse(time.RFC3339, lastPollAt.String)
			h.LastPollAt = &t
		}
		if lastObservedState.Valid {
			h.LastObservedState = &lastObservedState.String
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *SQLRepository) ObserveWorldHandle(ctx context.Context, id string, observedState string, polledAt time.Time) error {
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE run_world_handles
		SET last_poll_at = ?, last_observed_state = ?
		WHERE id = ?`),
		polledAt.Format(time.RFC3339), observedState, id)
	if err != nil {
		return fmt.Errorf("observe world handle: %w", err)
	}
	return nil
}

// --- Idempotency keys (§D5) ---

func (r *SQLRepository) RecordToolIntent(ctx context.Context, key, runID, toolName, argsHash string) (*IdempotencyKey, error) {
	if key == "" {
		return nil, fmt.Errorf("record tool intent: key required")
	}
	if runID == "" {
		return nil, fmt.Errorf("record tool intent: run_id required")
	}

	// Idempotent-on-duplicate: if the key already exists, return the
	// existing row unchanged. The cross-driver-portable way to express
	// this is INSERT ... ON CONFLICT DO NOTHING followed by SELECT.
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO tool_idempotency_keys
			(key, run_id, step_id, tool_name, args_hash, created_at, completed_at, result, status)
		VALUES (?, ?, NULL, ?, ?, ?, NULL, NULL, ?)
		ON CONFLICT (key) DO NOTHING`),
		key, runID, toolName, argsHash, now.Format(time.RFC3339),
		string(IdempotencyKeyStatusIssued))
	if err != nil {
		return nil, fmt.Errorf("record tool intent: %w", err)
	}
	return r.GetIdempotencyKey(ctx, key)
}

func (r *SQLRepository) MarkToolCompleted(ctx context.Context, key string, result string) error {
	return r.markToolTerminal(ctx, key, IdempotencyKeyStatusCompleted, result)
}

func (r *SQLRepository) MarkToolFailed(ctx context.Context, key string, result string) error {
	return r.markToolTerminal(ctx, key, IdempotencyKeyStatusFailed, result)
}

// markToolTerminal transitions key -> terminalStatus only if it is currently
// 'issued'. Returns ErrAlreadyTerminal if the row is already terminal, or
// ErrNotFound if the key does not exist. This is the §D5 no-overwrite rule.
func (r *SQLRepository) markToolTerminal(ctx context.Context, key string, terminalStatus IdempotencyKeyStatus, result string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE tool_idempotency_keys
		SET status = ?, result = ?, completed_at = ?
		WHERE key = ? AND status = ?`),
		string(terminalStatus), result, now, key, string(IdempotencyKeyStatusIssued))
	if err != nil {
		return fmt.Errorf("mark tool %s: %w", terminalStatus, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 1 {
		return nil
	}

	// Update didn't hit. Disambiguate: does the row exist at all?
	existing, err := r.GetIdempotencyKey(ctx, key)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrNotFound
	}
	return ErrAlreadyTerminal
}

func (r *SQLRepository) GetIdempotencyKey(ctx context.Context, key string) (*IdempotencyKey, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT key, run_id, step_id, tool_name, args_hash, created_at,
		       completed_at, result, status
		FROM tool_idempotency_keys WHERE key = ?`), key)
	var (
		k            IdempotencyKey
		stepID       sql.NullString
		createdAtStr string
		completedAt  sql.NullString
		result       sql.NullString
		status       string
	)
	err := row.Scan(&k.Key, &k.RunID, &stepID, &k.ToolName, &k.ArgsHash,
		&createdAtStr, &completedAt, &result, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get idempotency key: %w", err)
	}
	if stepID.Valid {
		k.StepID = &stepID.String
	}
	k.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		k.CompletedAt = &t
	}
	if result.Valid {
		k.Result = &result.String
	}
	k.Status = IdempotencyKeyStatus(status)
	return &k, nil
}

// --- Action ledger ---

func (r *SQLRepository) AppendLedger(ctx context.Context, e LedgerEntry) (*LedgerEntry, error) {
	if e.ID == "" {
		return nil, fmt.Errorf("append ledger: id required")
	}
	if e.RecordedAt.IsZero() {
		e.RecordedAt = time.Now().UTC()
	}
	var sourceID any
	if e.ComponentID != nil {
		sourceID = *e.ComponentID
	}
	var completedAt any
	if e.CompletedAt != nil {
		completedAt = e.CompletedAt.Format(time.RFC3339)
	}
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO action_ledger
			(id, run_id, idempotency_key, tool_name, tier, principal, component_id,
			 summary, recorded_at, completed_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		e.ID, e.RunID, e.IdempotencyKey, e.ToolName, int(e.Tier),
		e.Principal, sourceID, e.Summary,
		e.RecordedAt.Format(time.RFC3339), completedAt, e.Status)
	if err != nil {
		return nil, fmt.Errorf("append ledger: %w", err)
	}
	return &e, nil
}

func (r *SQLRepository) ListLedgerForRun(ctx context.Context, runID string) ([]LedgerEntry, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver, `
		SELECT id, run_id, idempotency_key, tool_name, tier, principal,
		       component_id, summary, recorded_at, completed_at, status
		FROM action_ledger WHERE run_id = ? ORDER BY recorded_at`), runID)
	if err != nil {
		return nil, fmt.Errorf("list ledger: %w", err)
	}
	defer rows.Close()

	var out []LedgerEntry
	for rows.Next() {
		var (
			e             LedgerEntry
			tier          int
			sourceID      sql.NullString
			recordedAtStr string
			completedAt   sql.NullString
		)
		if err := rows.Scan(&e.ID, &e.RunID, &e.IdempotencyKey, &e.ToolName,
			&tier, &e.Principal, &sourceID, &e.Summary,
			&recordedAtStr, &completedAt, &e.Status); err != nil {
			return nil, fmt.Errorf("scan ledger: %w", err)
		}
		e.Tier = Tier(tier)
		if sourceID.Valid {
			e.ComponentID = &sourceID.String
		}
		e.RecordedAt, _ = time.Parse(time.RFC3339, recordedAtStr)
		if completedAt.Valid {
			t, _ := time.Parse(time.RFC3339, completedAt.String)
			e.CompletedAt = &t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
