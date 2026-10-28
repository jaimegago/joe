package autorun

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// Repository is the durable surface for the run record and the schedules. The
// one-live-run-per-scope rule is the database's (the partial unique index in
// migration 034), not this interface's: CreateRun surfaces that refusal as
// ErrRunLive, and nothing here can admit a second live run on a scope.
type Repository interface {
	CreateRun(ctx context.Context, r Run) (*Run, error)
	// FinishRun records the terminal state of a run: coverage, spend, stop
	// reason, error, and the session it wrote.
	FinishRun(ctx context.Context, r Run) error
	// SetRunSession links the session a run wrote to its record. Separate from
	// FinishRun because the session is created after the record (the record
	// is what refuses a concurrent trigger, so it must exist first) and before
	// the pass.
	SetRunSession(ctx context.Context, runID, sessionID string) error
	GetRun(ctx context.Context, id string) (*Run, error)
	ListRuns(ctx context.Context, limit int) ([]Run, error)
	// FailInterrupted closes every 'running' row as failed/interrupted. Called
	// once at service start: a row left running by a process that ended under
	// it would hold its scope live forever.
	FailInterrupted(ctx context.Context, now time.Time) (int64, error)

	CreateSchedule(ctx context.Context, s Schedule) (*Schedule, error)
	GetSchedule(ctx context.Context, id string) (*Schedule, error)
	ListSchedules(ctx context.Context) ([]Schedule, error)
	ListDueSchedules(ctx context.Context, now time.Time) ([]Schedule, error)
	DeleteSchedule(ctx context.Context, id string) error
	// MarkScheduleFired records the tick's outcome and advances next_run_at.
	MarkScheduleFired(ctx context.Context, id string, firedAt, nextRunAt time.Time, outcome string) error
}

// SQLRepository implements Repository on *sql.DB, parallel to the runmodel and
// sessionmodel repositories: ? placeholders rebound per driver, RFC3339 times,
// JSON-encoded TEXT for the list columns.
type SQLRepository struct {
	db     *sql.DB
	driver string
}

// NewRepository constructs a SQLRepository.
func NewRepository(db *sql.DB, driver string) *SQLRepository {
	return &SQLRepository{db: db, driver: driver}
}

func encodeList(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func decodeList(s string) []string {
	var out []string
	if s == "" {
		return []string{}
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}

func encodeSelection(s Selection) string {
	b, _ := json.Marshal(s.Normalize())
	return string(b)
}

func decodeSelection(s string) Selection {
	var out Selection
	_ = json.Unmarshal([]byte(s), &out)
	return out.Normalize()
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func nullTime(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	t := parseTime(ns.String)
	return &t
}

// isUniqueViolation reports whether err is a unique-constraint violation from
// SQLite (modernc) or Postgres (pgx). Neither driver exposes a stable typed
// error, so this string-matches, the same way internal/api/runs.go does for
// agent_runs' one-running-per-session index.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{"unique constraint failed", "unique constraint", "duplicate key"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

const runColumns = `id, activator, principal, selection, scope_key, resolved_components,
	token_ceiling, wall_clock_seconds, report_reserve_tokens, state, stop_reason,
	covered, not_covered, tokens_used, report_tokens, session_id, error, started_at, ended_at`

// CreateRun inserts a running row. A live run on the same scope makes the
// insert fail the partial unique index, surfaced as ErrRunLive.
func (r *SQLRepository) CreateRun(ctx context.Context, run Run) (*Run, error) {
	if run.ID == "" {
		return nil, fmt.Errorf("create run: id required")
	}
	if run.Activator == "" || run.Principal == "" {
		return nil, fmt.Errorf("create run: activator and principal required")
	}
	if run.State == "" {
		run.State = StateRunning
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if run.ScopeKey == "" {
		run.ScopeKey = run.Selection.Key()
	}
	var sessionID any
	if run.SessionID != "" {
		sessionID = run.SessionID
	}
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO auto_runs (`+runColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`),
		run.ID, run.Activator, run.Principal, encodeSelection(run.Selection), run.ScopeKey,
		encodeList(run.ResolvedComponents), run.TokenCeiling, run.WallClockSeconds,
		run.ReportReserveTokens, run.State, run.StopReason, encodeList(run.Covered),
		encodeList(run.NotCovered), run.TokensUsed, run.ReportTokens, sessionID, run.Error,
		fmtTime(run.StartedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrRunLive
		}
		return nil, fmt.Errorf("create run: %w", err)
	}
	return &run, nil
}

func (r *SQLRepository) SetRunSession(ctx context.Context, runID, sessionID string) error {
	res, err := r.db.ExecContext(ctx, store.Rebind(r.driver,
		`UPDATE auto_runs SET session_id = ? WHERE id = ?`), sessionID, runID)
	if err != nil {
		return fmt.Errorf("set run session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLRepository) FinishRun(ctx context.Context, run Run) error {
	if run.State == StateRunning {
		return fmt.Errorf("finish run: terminal state required, got %q", run.State)
	}
	ended := time.Now().UTC()
	if run.EndedAt != nil {
		ended = *run.EndedAt
	}
	var sessionID any
	if run.SessionID != "" {
		sessionID = run.SessionID
	}
	res, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE auto_runs SET state = ?, stop_reason = ?, covered = ?, not_covered = ?,
			tokens_used = ?, report_tokens = ?, session_id = ?, error = ?, ended_at = ?
		WHERE id = ?`),
		run.State, run.StopReason, encodeList(run.Covered), encodeList(run.NotCovered),
		run.TokensUsed, run.ReportTokens, sessionID, run.Error, fmtTime(ended), run.ID)
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanRun(scan func(...any) error) (*Run, error) {
	var (
		run                            Run
		selection, resolved, cov, ncov string
		sessionID, endedAt             sql.NullString
		startedAt                      string
	)
	err := scan(&run.ID, &run.Activator, &run.Principal, &selection, &run.ScopeKey, &resolved,
		&run.TokenCeiling, &run.WallClockSeconds, &run.ReportReserveTokens, &run.State, &run.StopReason,
		&cov, &ncov, &run.TokensUsed, &run.ReportTokens, &sessionID, &run.Error, &startedAt, &endedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan run: %w", err)
	}
	run.Selection = decodeSelection(selection)
	run.ResolvedComponents = decodeList(resolved)
	run.Covered = decodeList(cov)
	run.NotCovered = decodeList(ncov)
	run.SessionID = sessionID.String
	run.StartedAt = parseTime(startedAt)
	run.EndedAt = nullTime(endedAt)
	return &run, nil
}

func (r *SQLRepository) GetRun(ctx context.Context, id string) (*Run, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver,
		`SELECT `+runColumns+` FROM auto_runs WHERE id = ?`), id)
	return scanRun(row.Scan)
}

func (r *SQLRepository) ListRuns(ctx context.Context, limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver,
		`SELECT `+runColumns+` FROM auto_runs ORDER BY started_at DESC, id LIMIT ?`), limit)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		run, err := scanRun(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	return out, rows.Err()
}

func (r *SQLRepository) FailInterrupted(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE auto_runs SET state = ?, stop_reason = ?, error = ?, ended_at = ?
		WHERE state = ?`),
		StateFailed, StopReasonInterrupted, "process ended while the run was live", fmtTime(now), StateRunning)
	if err != nil {
		return 0, fmt.Errorf("fail interrupted runs: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

const scheduleColumns = `id, selection, scope_key, interval_seconds, token_ceiling, wall_clock_seconds,
	report_reserve_tokens, installed_by, installed_at, next_run_at, last_run_at, last_outcome`

func (r *SQLRepository) CreateSchedule(ctx context.Context, s Schedule) (*Schedule, error) {
	if s.ID == "" {
		return nil, fmt.Errorf("create schedule: id required")
	}
	if s.IntervalSeconds <= 0 {
		return nil, fmt.Errorf("create schedule: positive interval required")
	}
	if s.InstalledAt.IsZero() {
		s.InstalledAt = time.Now().UTC()
	}
	if s.NextRunAt.IsZero() {
		s.NextRunAt = s.InstalledAt.Add(time.Duration(s.IntervalSeconds) * time.Second)
	}
	if s.ScopeKey == "" {
		s.ScopeKey = s.Selection.Key()
	}
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO auto_schedules (`+scheduleColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, '')`),
		s.ID, encodeSelection(s.Selection), s.ScopeKey, s.IntervalSeconds, s.TokenCeiling,
		s.WallClockSeconds, s.ReportReserveTokens, s.InstalledBy, fmtTime(s.InstalledAt), fmtTime(s.NextRunAt))
	if err != nil {
		return nil, fmt.Errorf("create schedule: %w", err)
	}
	return &s, nil
}

func scanSchedule(scan func(...any) error) (*Schedule, error) {
	var (
		s                               Schedule
		selection, installedAt, nextRun string
		lastRun                         sql.NullString
	)
	err := scan(&s.ID, &selection, &s.ScopeKey, &s.IntervalSeconds, &s.TokenCeiling, &s.WallClockSeconds,
		&s.ReportReserveTokens, &s.InstalledBy, &installedAt, &nextRun, &lastRun, &s.LastOutcome)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan schedule: %w", err)
	}
	s.Selection = decodeSelection(selection)
	s.InstalledAt = parseTime(installedAt)
	s.NextRunAt = parseTime(nextRun)
	s.LastRunAt = nullTime(lastRun)
	return &s, nil
}

func (r *SQLRepository) GetSchedule(ctx context.Context, id string) (*Schedule, error) {
	row := r.db.QueryRowContext(ctx, store.Rebind(r.driver,
		`SELECT `+scheduleColumns+` FROM auto_schedules WHERE id = ?`), id)
	return scanSchedule(row.Scan)
}

func (r *SQLRepository) listSchedules(ctx context.Context, where string, args ...any) ([]Schedule, error) {
	rows, err := r.db.QueryContext(ctx, store.Rebind(r.driver,
		`SELECT `+scheduleColumns+` FROM auto_schedules `+where+` ORDER BY next_run_at, id`), args...)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()
	out := []Schedule{}
	for rows.Next() {
		s, err := scanSchedule(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (r *SQLRepository) ListSchedules(ctx context.Context) ([]Schedule, error) {
	return r.listSchedules(ctx, "")
}

func (r *SQLRepository) ListDueSchedules(ctx context.Context, now time.Time) ([]Schedule, error) {
	return r.listSchedules(ctx, "WHERE next_run_at <= ?", fmtTime(now))
}

func (r *SQLRepository) DeleteSchedule(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, store.Rebind(r.driver,
		`DELETE FROM auto_schedules WHERE id = ?`), id)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLRepository) MarkScheduleFired(ctx context.Context, id string, firedAt, nextRunAt time.Time, outcome string) error {
	res, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		UPDATE auto_schedules SET last_run_at = ?, next_run_at = ?, last_outcome = ? WHERE id = ?`),
		fmtTime(firedAt), fmtTime(nextRunAt), outcome, id)
	if err != nil {
		return fmt.Errorf("mark schedule fired: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
