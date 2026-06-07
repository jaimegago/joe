package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/jaimegago/joe/internal/rbac"
)

const (
	// SessionCookieName carries only the opaque session id.
	SessionCookieName = "joe_session"
	// stateCookieName binds the browser to a single in-flight OIDC login,
	// protecting the callback against login CSRF (design §2.3). It is scoped to
	// the auth path and lives only for the duration of the redirect dance.
	stateCookieName = "joe_oidc_state"
	// stateCookiePath scopes the temporary state cookie to the auth endpoints.
	// SameSite=Lax still sends it on the top-level GET navigation back from the
	// IdP (the callback), which is exactly when it is needed.
	stateCookiePath = "/api/v1/auth"
	// flowTTL bounds how long an initiated login may sit unfinished.
	flowTTL = 10 * time.Minute
	// defaultSessionTTL is used when the configured TTL is non-positive, so a
	// session can never be unbounded by misconfiguration (design §2.3).
	defaultSessionTTL = 12 * time.Hour
)

// SessionManager mints, resolves, and revokes server-side sessions and sets the
// session cookie. The cookie is HttpOnly, Secure, SameSite=Lax (design §2.3):
//   - HttpOnly: not readable by JS, so XSS cannot exfiltrate it.
//   - Secure: only sent over TLS. Defaults on; SetSecureCookies(false) drops it
//     for local HTTP dev (see ServerConfig.InsecureCookies).
//   - SameSite=Lax (NOT Strict): Strict would break the OIDC callback, because
//     the browser would not send the session cookie on the cross-site
//     navigation returning from the IdP. Lax sends it on top-level GET
//     navigations while still blocking cross-site POSTs.
type SessionManager struct {
	repo   Repository
	ttl    time.Duration
	now    func() time.Time
	secure bool
}

// NewSessionManager builds a SessionManager. A non-positive ttl falls back to
// defaultSessionTTL so a session lifetime is always bounded. Cookies are Secure
// by default; call SetSecureCookies(false) for local HTTP dev only.
func NewSessionManager(repo Repository, ttl time.Duration) *SessionManager {
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	return &SessionManager{repo: repo, ttl: ttl, now: time.Now, secure: true}
}

// SetSecureCookies toggles the Secure attribute on the session cookie. It is
// true by default; pass false ONLY for local HTTP dev (ServerConfig.
// InsecureCookies), never in production.
func (m *SessionManager) SetSecureCookies(secure bool) {
	m.secure = secure
}

// Mint creates and persists a new session for principal and returns it.
func (m *SessionManager) Mint(ctx context.Context, principal rbac.Principal) (*Session, error) {
	id, err := randomToken()
	if err != nil {
		return nil, err
	}
	now := m.now().UTC()
	s := Session{
		ID:        id,
		Principal: string(principal),
		CreatedAt: now,
		ExpiresAt: now.Add(m.ttl),
	}
	if err := m.repo.CreateSession(ctx, s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Resolve returns the principal for a session id, or (Unknown, false) if the
// session is absent, deleted, or past its bounded lifetime. An expired session
// is best-effort deleted so it cannot be probed repeatedly.
func (m *SessionManager) Resolve(ctx context.Context, id string) (rbac.Principal, bool) {
	if id == "" {
		return rbac.Unknown, false
	}
	s, err := m.repo.GetSession(ctx, id)
	if err != nil || s == nil {
		return rbac.Unknown, false
	}
	if !m.now().UTC().Before(s.ExpiresAt) {
		_ = m.repo.DeleteSession(ctx, id)
		return rbac.Unknown, false
	}
	return rbac.Principal(s.Principal), true
}

// Revoke deletes a session immediately (logout / forced revocation). Deleting
// the row is the whole point of server-side sessions over JWTs (design §2.3).
func (m *SessionManager) Revoke(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return m.repo.DeleteSession(ctx, id)
}

// SetCookie writes the session cookie for s.
func (m *SessionManager) SetCookie(w http.ResponseWriter, s *Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    s.ID,
		Path:     "/",
		Expires:  s.ExpiresAt,
		MaxAge:   int(time.Until(s.ExpiresAt).Seconds()),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie expires the session cookie on the client (logout).
func (m *SessionManager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// cookieValue returns the session id from the request cookie, or "".
func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// randomToken returns a 256-bit cryptographically random, URL-safe token used
// for session ids and OIDC state values.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
