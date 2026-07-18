// Package sessionsweeper implements the §12.5 retention sweeper: the single
// automated expiration driver for the redesigned session subsystem
// (DESIGN-CHAT-SESSIONS.md §12.5, ledger node B007). §12 wins over earlier
// sections of that document where they conflict.
//
// THE SWEEPER IS THE ONLY AUTOMATED DRIVER. On its interval it, in order:
//
//  1. Inactivity expiry (§12.5, against last_activity_at) — ONLY when the
//     retention policy's inactivity window is enabled (it is OFF / nil by
//     default, the regulated-posture default). Active sessions older than the
//     window get the policy terminal action: trash_then_purge → trash now (a
//     later cycle purges them past purge_after); archive → the §12.6
//     provider-backed archive (B007c) when an archive provider is wired (read
//     the live transcript, write the versioned artifact, stamp the archive
//     columns + remove the hot rows in one audited transaction); when no provider
//     is configured it falls back to an honest leave-active-and-log seam — the
//     session is never trashed and never falsely marked archived (see the archive
//     case below).
//
//  2. Trash-grace purge (§12.5, against purge_after) — the unconditional second
//     pass: it purges every trashed session whose purge_after has passed,
//     whether the sweeper trashed it (under trash_then_purge) or an owner
//     soft-deleted it manually. Independent of the terminal action.
//
//  3. Abandoned-login-flow drain (§12.5) — a DISTINCT responsibility on the
//     authentication-session table (auth_login_flows), NOT a chat-session table.
//     It is never governed by the chat retention policy; its drain condition is
//     the flow's own expires_at. One worker runs both sweeps, but the two
//     subsystems are not conflated.
//
// SERVICE PRINCIPAL (§12.5 / §12.7). The sweeper acts under a boot-minted
// service principal (rbac.SessionSweeperPrincipal, svc:sweeper:sessions) so
// every automated expiration is attributed. It is a system actor: neither owner
// nor admin, it bypasses relationship resolution for its policy-authorized
// transitions and names itself in every audit row.
//
// EFFECT↔AUDIT COUPLING OUTSIDE HTTP. The sweeper runs outside an HTTP handler,
// so it cannot use the handler-shaped (*api.Server).mutateWithAudit. It carries
// its own same-transaction wrapper (mutateWithAudit below) that commits the
// lifecycle effect and its audit row in ONE transaction under the service
// principal — fail-closed: an audit-write failure rolls the effect back, exactly
// as the HTTP path does. There is NO divergent transition logic: the sweeper
// drives the SAME B007a *Tx store methods the manual owner/admin routes use.
//
// FAIL-SAFE. Each candidate session is processed in its OWN transaction. A
// per-session error is logged and skipped; it never aborts the rest of the
// batch and never leaves a partial cross-session state.
//
// LEGACY TABLES. The sweep scans ONLY agent_sessions (via the B007b scan
// queries) and auth_login_flows. It never reads, scans, drops, or alters the
// legacy migration-001 `sessions` / `session_messages` tables (§13 hard
// constraint).
package sessionsweeper

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
	"github.com/jaimegago/joe/internal/sessionarchive"
	"github.com/jaimegago/joe/internal/sessionmodel"
)

// SessionStore is the narrow slice of sessionmodel.Repository the sweeper needs:
// the retention policy, the two B007b expiration scans, and the two B007a *Tx
// transitions it drives. Taking an interface (not the concrete repo) keeps the
// sweeper unit-testable with fakes for the fail-safe / rollback cases.
type SessionStore interface {
	GetRetentionPolicy(ctx context.Context) (*sessionmodel.RetentionPolicy, error)
	ListSessionsInactiveBefore(ctx context.Context, cutoff time.Time) ([]sessionmodel.AgentSession, error)
	ListPurgeableSessions(ctx context.Context, now time.Time) ([]sessionmodel.AgentSession, error)
	TrashSessionTx(ctx context.Context, tx *sql.Tx, id, by string, purgeAfter *time.Time) error
	PurgeSessionTx(ctx context.Context, tx *sql.Tx, id string) error
}

// FlowDrainer is the narrow slice of auth.Repository the login-flow drain needs.
// It is a SEPARATE dependency from SessionStore precisely so the two
// responsibilities cannot be conflated: the drain physically cannot reach a
// chat-session table through this interface.
type FlowDrainer interface {
	DeleteExpiredFlows(ctx context.Context, now time.Time) (int64, error)
}

// AuditSink is the transactional audit path (audit.Repository.InsertTx). It may
// be nil in a dev/local harness without the audit table — the same no-op
// carve-out (*api.Server).mutateWithAudit uses; production boot always wires it,
// so production is always fail-closed.
type AuditSink interface {
	InsertTx(ctx context.Context, tx *sql.Tx, e audit.Event) error
}

// Config wires the sweeper's dependencies.
type Config struct {
	// DB is the handle the sweeper opens its per-session effect+audit
	// transactions on (the same DB the store methods write through).
	DB *sql.DB
	// Sessions / Flows are the two distinct subsystems the sweep touches.
	Sessions SessionStore
	Flows    FlowDrainer
	// Archive is the §12.6 archive provider+store coupling the inactivity-expiry
	// archive branch drives (B007c). When nil (no archive directory configured)
	// the archive terminal action keeps its honest leave-active-and-log seam — a
	// session is NEVER falsely marked archived without a real artifact.
	Archive *sessionarchive.Archiver
	// Audit is the transactional audit sink (may be nil — see AuditSink).
	Audit AuditSink
	// Principal is the boot-minted service principal the sweeper acts under
	// (rbac.SessionSweeperPrincipal). Required — an empty principal would write
	// unattributed audit rows, defeating §12.5.
	Principal rbac.Principal
	Logger    *slog.Logger
	// Interval is the sweep period. Defaults to 1h when zero.
	Interval time.Duration
	// Now is the clock, injectable so tests drive expiry deterministically.
	// Defaults to time.Now.
	Now func() time.Time
}

// Sweeper is the §12.5 background retention worker. Construct with New; drive it
// with Start (background loop) or Sweep (one deterministic cycle, for tests).
type Sweeper struct {
	db        *sql.DB
	sessions  SessionStore
	flows     FlowDrainer
	archive   *sessionarchive.Archiver
	audit     AuditSink
	principal rbac.Principal
	logger    *slog.Logger
	interval  time.Duration
	now       func() time.Time

	cancel context.CancelFunc
	doneCh chan struct{}
}

// New builds a Sweeper from cfg, applying the defaults (1h interval, time.Now).
func New(cfg Config) *Sweeper {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = time.Hour
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	return &Sweeper{
		db:        cfg.DB,
		sessions:  cfg.Sessions,
		flows:     cfg.Flows,
		archive:   cfg.Archive,
		audit:     cfg.Audit,
		principal: cfg.Principal,
		logger:    logger.With("component", "session-sweeper"),
		interval:  interval,
		now:       nowFn,
		doneCh:    make(chan struct{}),
	}
}

// Start begins the background sweep loop. The principal is expected to ride ctx
// already (boot stamps it via rbac.WithPrincipal, mirroring the Core Agent
// refresh), but the sweeper also carries it explicitly for audit attribution, so
// the loop does not depend on the context carry for correctness.
func (s *Sweeper) Start(ctx context.Context) error {
	if s.db == nil || s.sessions == nil || s.flows == nil {
		return fmt.Errorf("session sweeper: refusing to start with unwired dependencies (db/sessions/flows)")
	}
	if s.principal == "" {
		return fmt.Errorf("session sweeper: refusing to start without a service principal (automated expirations would be unattributed)")
	}
	s.logger.Info("starting session retention sweeper", "interval", s.interval, "principal", string(s.principal))
	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	go s.loop(loopCtx)
	return nil
}

// Stop gracefully stops the background loop, mirroring coreagent.Refresher.Stop.
func (s *Sweeper) Stop(_ context.Context) error {
	s.logger.Info("stopping session retention sweeper")
	if s.cancel != nil {
		s.cancel()
	}
	select {
	case <-s.doneCh:
		s.logger.Info("session retention sweeper stopped")
		return nil
	case <-time.After(10 * time.Second):
		s.logger.Warn("session retention sweeper stop timeout")
		return fmt.Errorf("timeout waiting for session sweeper to stop")
	}
}

func (s *Sweeper) loop(ctx context.Context) {
	defer close(s.doneCh)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.Sweep(ctx)
		case <-ctx.Done():
			s.logger.Info("session sweep loop stopping")
			return
		}
	}
}

// Sweep runs ONE full retention cycle. It is exported and side-effect-scoped so
// tests can drive it directly with an injected clock instead of waiting on the
// real ticker. Every step is independent and fail-safe: a failure in one step
// (or one session) is logged and never aborts the others.
func (s *Sweeper) Sweep(ctx context.Context) {
	now := s.now().UTC()

	// Steps 1+2 (chat-session retention) read the one admin policy. If that read
	// fails, skip the chat sweep this cycle but STILL drain login flows — the
	// drain is an independent subsystem and must not be held hostage by the chat
	// retention policy (§12.5).
	policy, err := s.sessions.GetRetentionPolicy(ctx)
	if err != nil {
		s.logger.Error("sweep: load retention policy failed; skipping chat-session expiry this cycle", "error", err)
	} else {
		s.sweepInactivity(ctx, policy, now)
		s.sweepTrashGrace(ctx, now)
	}

	s.drainLoginFlows(ctx, now)
}

// sweepInactivity applies the §12.5 inactivity expiry. It is a NO-OP unless the
// inactivity window is enabled (default OFF / nil): nothing auto-expires until an
// admin opts in.
func (s *Sweeper) sweepInactivity(ctx context.Context, policy *sessionmodel.RetentionPolicy, now time.Time) {
	if policy == nil || policy.InactivityDays == nil {
		// §12.5: inactivity window OFF by default — nothing auto-expires.
		return
	}
	cutoff := now.Add(-time.Duration(*policy.InactivityDays) * 24 * time.Hour)
	candidates, err := s.sessions.ListSessionsInactiveBefore(ctx, cutoff)
	if err != nil {
		s.logger.Error("sweep: scan inactive sessions failed", "error", err)
		return
	}
	for _, sess := range candidates {
		switch policy.TerminalAction {
		case sessionmodel.TerminalActionTrashThenPurge:
			purgeAfter := s.trashGraceDeadline(policy, now)
			ev := lifecycleEvent(s.principal, audit.ActionSessionTrash, sess.ID, "sweeper_inactivity_expiry")
			if err := s.mutateWithAudit(ctx, ev, func(tx *sql.Tx) error {
				return s.sessions.TrashSessionTx(ctx, tx, sess.ID, string(s.principal), purgeAfter)
			}); err != nil {
				// Fail-safe: log and skip this session; the batch continues.
				s.logger.Error("sweep: trash inactive session failed; skipping", "session_id", sess.ID, "error", err)
				continue
			}
			s.logger.Info("sweep: trashed inactivity-expired session", "session_id", sess.ID)
		case sessionmodel.TerminalActionArchive:
			if s.archive == nil {
				// No archive provider wired (no archive directory configured). The
				// honest seam holds: marking the session archived without a real
				// archive_ref and without moving the transcript would be a lying
				// state, and trashing it would route it to purge (data loss). So
				// leave it ACTIVE and log — never falsely archived, never destroyed.
				s.logger.Warn("sweep: archive provider not configured; leaving inactivity-expired session ACTIVE (not trashed, not archived)",
					"session_id", sess.ID)
				continue
			}
			// Real archive (B007c): read the live transcript, write the versioned
			// artifact, then stamp archived_at/archived_by/archive_ref AND remove the
			// hot rows in ONE transaction coupled to the §12.5 lifecycle audit row,
			// under the service principal. Fail-safe per session: a failure is logged
			// and skipped (the artifact is cleaned up by the archiver on rollback),
			// and the batch continues.
			ev := lifecycleEvent(s.principal, audit.ActionSessionArchive, sess.ID, "sweeper_inactivity_expiry")
			commit := func(mutate func(*sql.Tx) error) error {
				return s.mutateWithAudit(ctx, ev, mutate)
			}
			if _, err := s.archive.Archive(ctx, sess, string(s.principal), commit); err != nil {
				s.logger.Error("sweep: archive inactive session failed; skipping", "session_id", sess.ID, "error", err)
				continue
			}
			s.logger.Info("sweep: archived inactivity-expired session", "session_id", sess.ID)
		default:
			s.logger.Warn("sweep: unknown terminal action; leaving session active",
				"session_id", sess.ID, "terminal_action", string(policy.TerminalAction))
		}
	}
}

// sweepTrashGrace applies the §12.5 trash-grace purge: the unconditional second
// pass that purges every trashed session past its purge_after deadline. It does
// not consult the terminal action — manually owner-trashed sessions are purged
// after grace just like sweeper-trashed ones.
func (s *Sweeper) sweepTrashGrace(ctx context.Context, now time.Time) {
	candidates, err := s.sessions.ListPurgeableSessions(ctx, now)
	if err != nil {
		s.logger.Error("sweep: scan purgeable sessions failed", "error", err)
		return
	}
	for _, sess := range candidates {
		ev := lifecycleEvent(s.principal, audit.ActionSessionPurge, sess.ID, "sweeper_trash_grace_purge")
		if err := s.mutateWithAudit(ctx, ev, func(tx *sql.Tx) error {
			return s.sessions.PurgeSessionTx(ctx, tx, sess.ID)
		}); err != nil {
			s.logger.Error("sweep: purge trashed session failed; skipping", "session_id", sess.ID, "error", err)
			continue
		}
		s.logger.Info("sweep: purged trashed session past grace", "session_id", sess.ID)
	}
}

// drainLoginFlows removes abandoned auth_login_flows past expires_at (§12.5). It
// touches ONLY the authentication-session table and writes no chat-session audit
// row — login flows are CSRF/PKCE state, not governed lifecycle entities.
func (s *Sweeper) drainLoginFlows(ctx context.Context, now time.Time) {
	n, err := s.flows.DeleteExpiredFlows(ctx, now)
	if err != nil {
		s.logger.Error("sweep: drain abandoned login flows failed", "error", err)
		return
	}
	if n > 0 {
		s.logger.Info("sweep: drained abandoned login flows", "count", n)
	}
}

// trashGraceDeadline computes purge_after = now + trash-grace for a
// sweeper-applied trash, mirroring (*api.Server).trashGraceDeadline. Returns nil
// when the grace is non-positive (no auto-purge deadline).
func (s *Sweeper) trashGraceDeadline(policy *sessionmodel.RetentionPolicy, now time.Time) *time.Time {
	if policy == nil || policy.TrashGraceDays <= 0 {
		return nil
	}
	deadline := now.Add(time.Duration(policy.TrashGraceDays) * 24 * time.Hour)
	return &deadline
}

// mutateWithAudit commits the lifecycle effect and its audit row in ONE
// transaction under the service principal — the out-of-HTTP twin of
// (*api.Server).mutateWithAudit. Fail-closed: an audit-write failure rolls the
// effect back. A nil audit sink skips the row but still commits the effect (the
// dev/local carve-out).
func (s *Sweeper) mutateWithAudit(ctx context.Context, ev audit.Event, mutate func(*sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = mutate(tx); err != nil {
		return err
	}
	if s.audit != nil {
		if err = s.audit.InsertTx(ctx, tx, ev); err != nil {
			return fmt.Errorf("audit insert: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// lifecycleEvent builds the §12.5 audit row for a sweeper-applied transition. It
// carries KindSessionLifecycle (the owner/sweeper-transition kind, migration
// 027) and names the service principal as actor and the target session.
func lifecycleEvent(principal rbac.Principal, action, sessionID, reason string) audit.Event {
	blob, _ := json.Marshal(audit.Details{Target: "session:" + sessionID})
	return audit.Event{
		Principal: string(principal),
		Action:    action,
		Decision:  audit.DecisionAllow,
		Reason:    reason,
		Kind:      audit.KindSessionLifecycle,
		Context:   string(blob),
	}
}
