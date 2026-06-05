// Package auth implements human authentication at the edge (Identity Phase C,
// docs/joe-identity-design.md §2.1–§2.3, §2.9).
//
// A human logs in through a single configurable OIDC issuer; the verified
// `email` claim becomes a `user:<email>` principal (rbac.UserPrincipal). That
// principal is carried by a server-side session: a row in SQLite plus an
// HttpOnly, Secure, SameSite=Lax cookie holding only the opaque session id.
// The principal then flows through the SAME context mechanism Phase B
// established (rbac.WithPrincipal → rbac.PrincipalFromContext), so both the
// EnforcementMiddleware and the guarded accessor see the real caller.
//
// This package authenticates ONLY at the external HTTP edge. It does not touch
// the agentic loop, the loopback, or service-account API keys (Phases D/E).
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jaimegago/joe/internal/store"
)

// Session is a minted server-side login session. The cookie carries only ID;
// Principal and ExpiresAt are authoritative here, never trusted from the client.
type Session struct {
	ID        string
	Principal string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// LoginFlow is the short-lived, in-flight OIDC authorization-code state. It
// holds the PKCE code verifier and the OIDC nonce, keyed by the opaque `state`,
// so the callback can validate state (CSRF), finish the PKCE exchange, and
// check the nonce. Rows are single-use and short-lived.
type LoginFlow struct {
	State        string
	CodeVerifier string
	Nonce        string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// Repository persists auth sessions and in-flight login flows. It follows the
// rbac.SQLRepository pattern (own *sql.DB handle, store.Rebind for driver
// portability) rather than threading through the store.Store struct.
type Repository interface {
	CreateSession(ctx context.Context, s Session) error
	// GetSession returns the row or (nil, nil) if absent. It does NOT apply the
	// expiry policy — the caller (SessionManager) compares ExpiresAt against a
	// controllable clock so expiry is deterministically testable.
	GetSession(ctx context.Context, id string) (*Session, error)
	DeleteSession(ctx context.Context, id string) error
	// DeleteSessionsForPrincipal deletes every session held by principal and
	// returns the number removed. It is the instant-revocation primitive the
	// identity-disable path uses: server-side sessions resolve by row lookup,
	// so removing the rows invalidates the principal's live sessions
	// immediately — no per-request status check is required.
	DeleteSessionsForPrincipal(ctx context.Context, principal string) (int64, error)

	CreateFlow(ctx context.Context, f LoginFlow) error
	GetFlow(ctx context.Context, state string) (*LoginFlow, error)
	DeleteFlow(ctx context.Context, state string) error
}

// SQLRepository is the SQLite/Postgres-backed Repository.
type SQLRepository struct {
	db     *sql.DB
	driver string
}

// NewRepository builds a Repository over the given database handle. The tables
// are created by migration 014.
func NewRepository(db *sql.DB, driver string) *SQLRepository {
	return &SQLRepository{db: db, driver: driver}
}

func (r *SQLRepository) CreateSession(ctx context.Context, s Session) error {
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO auth_sessions (id, principal, created_at, expires_at)
		VALUES (?, ?, ?, ?)`),
		s.ID, s.Principal, s.CreatedAt.UTC().Format(time.RFC3339), s.ExpiresAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("create auth session: %w", err)
	}
	return nil
}

func (r *SQLRepository) GetSession(ctx context.Context, id string) (*Session, error) {
	var s Session
	var createdAt, expiresAt string
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT id, principal, created_at, expires_at FROM auth_sessions WHERE id = ?`), id).
		Scan(&s.ID, &s.Principal, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get auth session: %w", err)
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	s.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	return &s, nil
}

func (r *SQLRepository) DeleteSession(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver,
		`DELETE FROM auth_sessions WHERE id = ?`), id)
	if err != nil {
		return fmt.Errorf("delete auth session: %w", err)
	}
	return nil
}

func (r *SQLRepository) DeleteSessionsForPrincipal(ctx context.Context, principal string) (int64, error) {
	res, err := r.db.ExecContext(ctx, store.Rebind(r.driver,
		`DELETE FROM auth_sessions WHERE principal = ?`), principal)
	if err != nil {
		return 0, fmt.Errorf("delete auth sessions for principal: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}

func (r *SQLRepository) CreateFlow(ctx context.Context, f LoginFlow) error {
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver, `
		INSERT INTO auth_login_flows (state, code_verifier, nonce, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`),
		f.State, f.CodeVerifier, f.Nonce,
		f.CreatedAt.UTC().Format(time.RFC3339), f.ExpiresAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("create login flow: %w", err)
	}
	return nil
}

func (r *SQLRepository) GetFlow(ctx context.Context, state string) (*LoginFlow, error) {
	var f LoginFlow
	var createdAt, expiresAt string
	err := r.db.QueryRowContext(ctx, store.Rebind(r.driver, `
		SELECT state, code_verifier, nonce, created_at, expires_at
		FROM auth_login_flows WHERE state = ?`), state).
		Scan(&f.State, &f.CodeVerifier, &f.Nonce, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get login flow: %w", err)
	}
	f.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	f.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	return &f, nil
}

func (r *SQLRepository) DeleteFlow(ctx context.Context, state string) error {
	_, err := r.db.ExecContext(ctx, store.Rebind(r.driver,
		`DELETE FROM auth_login_flows WHERE state = ?`), state)
	if err != nil {
		return fmt.Errorf("delete login flow: %w", err)
	}
	return nil
}
