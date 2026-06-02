package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
)

// auditSourceOIDC names the credential mechanism recorded in the Source
// column of an OIDC-login audit row.
const auditSourceOIDC = "oidc"

// auditSourceAdminBootstrap names the mechanism recorded in the Source column
// of an admin-grant audit row: the auth.admin_email match in the OIDC callback
// that bootstraps a logging-in user to admin for the first time.
const auditSourceAdminBootstrap = "admin-bootstrap"

// Handlers serves the three OIDC endpoints the Web UI front end calls:
// login initiation, IdP callback, and logout. The front end is separate; this
// package provides the complete, testable server flow.
type Handlers struct {
	provider          Provider
	sessions          *SessionManager
	repo              Repository
	provisioner       *Provisioner
	adminEmail        string
	postLoginRedirect string
	now               func() time.Time
	// audit records one auth_login row per successful login (Stream H3).
	// nil when no audit store is wired (dev/local, tests) — the login
	// proceeds exactly as before in that case.
	audit audit.Repository
}

// HandlerConfig wires the dependencies for Handlers.
type HandlerConfig struct {
	Provider          Provider
	Sessions          *SessionManager
	Repo              Repository
	RBAC              rbac.Repository
	AdminEmail        string
	PostLoginRedirect string
	// Audit is the append-only audit trail. nil-safe: a nil repository
	// disables the auth_login write and leaves the login flow unchanged.
	Audit audit.Repository
}

// NewHandlers builds the auth Handlers. PostLoginRedirect defaults to "/".
func NewHandlers(cfg HandlerConfig) *Handlers {
	redirect := cfg.PostLoginRedirect
	if redirect == "" {
		redirect = "/"
	}
	return &Handlers{
		provider:          cfg.Provider,
		sessions:          cfg.Sessions,
		repo:              cfg.Repo,
		provisioner:       NewProvisioner(cfg.RBAC),
		adminEmail:        cfg.AdminEmail,
		postLoginRedirect: redirect,
		now:               time.Now,
		audit:             cfg.Audit,
	}
}

// RegisterRoutes mounts the auth endpoints under prefix (e.g. /api/v1).
// Logout is POST because it mutates server state (revokes the session); the API
// performs no state change via GET (design §2.3 CSRF posture).
func (h *Handlers) RegisterRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(fmt.Sprintf("GET %s/auth/login", prefix), h.Login)
	mux.HandleFunc(fmt.Sprintf("GET %s/auth/callback", prefix), h.Callback)
	mux.HandleFunc(fmt.Sprintf("POST %s/auth/logout", prefix), h.Logout)
}

// Login initiates the OIDC authorization-code + PKCE flow: it generates the
// state, nonce, and PKCE verifier, persists the in-flight flow, sets the
// state cookie (login-CSRF binding), and redirects the browser to the IdP.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	state, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to start login")
		return
	}
	nonce, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to start login")
		return
	}
	verifier := oauth2.GenerateVerifier()

	now := h.now().UTC()
	if err := h.repo.CreateFlow(ctx, LoginFlow{
		State:        state,
		CodeVerifier: verifier,
		Nonce:        nonce,
		CreatedAt:    now,
		ExpiresAt:    now.Add(flowTTL),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to start login")
		return
	}

	authURL, err := h.provider.AuthCodeURL(ctx, state, nonce, verifier)
	if err != nil {
		// IdP discovery failed / unreachable: existing sessions are unaffected,
		// only new logins fail (design §4).
		_ = h.repo.DeleteFlow(ctx, state)
		writeError(w, http.StatusServiceUnavailable, "oidc_unavailable", "identity provider is unavailable")
		return
	}

	h.setStateCookie(w, state)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback completes the flow: validate state (CSRF) against the cookie and the
// server-side flow, exchange the code (PKCE), verify the ID token (JWKS), check
// the nonce, enforce email_verified, optionally bootstrap admin, then mint the
// session and set the cookie.
func (h *Handlers) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	if idpErr := q.Get("error"); idpErr != "" {
		writeError(w, http.StatusBadRequest, "oidc_error",
			fmt.Sprintf("identity provider returned an error: %s", idpErr))
		return
	}
	state := q.Get("state")
	code := q.Get("code")
	if state == "" || code == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "missing state or code")
		return
	}

	// CSRF: the state in the query must match the one bound to this browser.
	if cs := cookieValue(r, stateCookieName); cs == "" || cs != state {
		h.clearStateCookie(w)
		writeError(w, http.StatusBadRequest, "invalid_state", "state mismatch")
		return
	}

	flow, err := h.repo.GetFlow(ctx, state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "login flow lookup failed")
		return
	}
	// Single-use: drop the flow and the state cookie regardless of outcome.
	defer func() { _ = h.repo.DeleteFlow(ctx, state) }()
	h.clearStateCookie(w)

	if flow == nil || !h.now().UTC().Before(flow.ExpiresAt) {
		writeError(w, http.StatusBadRequest, "invalid_state", "unknown or expired login flow")
		return
	}

	rawIDToken, err := h.provider.Exchange(ctx, code, flow.CodeVerifier)
	if err != nil {
		writeError(w, http.StatusBadGateway, "exchange_failed", "could not exchange authorization code")
		return
	}

	vt, err := h.provider.Verify(ctx, rawIDToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "id token verification failed")
		return
	}
	if vt.Nonce != flow.Nonce {
		writeError(w, http.StatusBadRequest, "invalid_nonce", "id token nonce mismatch")
		return
	}

	principal, err := PrincipalFromClaims(vt.Claims)
	if err != nil {
		// email_verified absent/false is the hard rejection: no session minted.
		if errors.Is(err, ErrEmailNotVerified) {
			writeError(w, http.StatusForbidden, "email_unverified",
				"the identity provider has not verified this email address")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_identity", "could not derive identity from token")
		return
	}

	// Admin bootstrap: the configured admin email gains admin authority on
	// (every) login (design §2.9). A grant failure must not silently masquerade
	// as a working admin, so it fails the login loudly.
	adminWasNew := false
	if h.adminEmail != "" && strings.EqualFold(vt.Claims.Email, h.adminEmail) {
		wasNew, err := h.provisioner.GrantAdmin(ctx, principal)
		if err != nil {
			slog.Error("auth: admin bootstrap grant failed", "principal", principal, "error", err)
			writeError(w, http.StatusInternalServerError, "bootstrap_failed", "failed to provision admin authority")
			return
		}
		adminWasNew = wasNew
		slog.Info("auth: admin bootstrap granted", "principal", principal, "first_grant", wasNew)
	}

	session, err := h.sessions.Mint(ctx, principal)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create session")
		return
	}
	h.sessions.SetCookie(w, session)
	slog.Info("auth: login succeeded", "principal", principal)
	// Stream H3: record credential use. Mint fires exactly once per login
	// episode, so this writes exactly one auth_login row per login. The
	// write is fail-open-but-loud — an audit failure must not block the
	// login or the redirect.
	h.recordLoginAudit(ctx, principal, vt.Claims.Email, r)
	// Privilege-escalation audit: record the admin grant only when it was a
	// real first-time escalation (wasNew). Repeat admin logins are not
	// escalation events and must produce no per-login noise. Fail-open-but-
	// loud, like recordLoginAudit.
	if adminWasNew {
		h.recordAdminGrantAudit(ctx, principal, vt.Claims.Email, r)
	}
	http.Redirect(w, r, h.postLoginRedirect, http.StatusFound)
}

// recordLoginAudit writes one auth_login audit row for a successful OIDC
// human login. It is fail-open-but-loud: on a write failure it calls
// FailurePosture (which logs loudly) and discards the return so the login
// still completes. nil-safe — a nil audit repository skips the write.
func (h *Handlers) recordLoginAudit(ctx context.Context, principal rbac.Principal, email string, r *http.Request) {
	if h.audit == nil {
		return
	}
	blob, _ := json.Marshal(map[string]string{
		"email":      email,
		"remote":     r.RemoteAddr,
		"user_agent": r.UserAgent(),
	})
	err := h.audit.Insert(ctx, audit.Event{
		Principal: string(principal),
		Action:    audit.ActionOIDCLogin,
		Source:    auditSourceOIDC,
		Decision:  audit.DecisionAllow,
		Reason:    "oidc_login",
		Kind:      audit.KindAuthLogin,
		Context:   string(blob),
	})
	// Discard the return: a login action is not read-class, so
	// FailurePosture would otherwise signal fail-closed. The `_ =`-discard
	// gives fail-open-but-loud without changing FailurePosture.
	_ = audit.FailurePosture(ctx, audit.ActionOIDCLogin, err, "auth:oidc_login")
}

// recordAdminGrantAudit writes one auth_login audit row recording a privilege
// escalation: the first-time admin bootstrap of a logging-in user via the
// auth.admin_email match. The caller invokes this ONLY when GrantAdmin
// reported wasNew (a real escalation), so it never fires on repeat admin
// logins. Like recordLoginAudit it is fail-open-but-loud — on a write failure
// it calls FailurePosture (loud log) and discards the return so the login
// still completes and redirects. nil-safe — a nil audit repository skips the
// write.
func (h *Handlers) recordAdminGrantAudit(ctx context.Context, principal rbac.Principal, email string, r *http.Request) {
	if h.audit == nil {
		return
	}
	blob, _ := json.Marshal(map[string]string{
		"email":      email,
		"remote":     r.RemoteAddr,
		"user_agent": r.UserAgent(),
	})
	err := h.audit.Insert(ctx, audit.Event{
		Principal: string(principal),
		Action:    audit.ActionAdminGranted,
		Source:    auditSourceAdminBootstrap,
		Decision:  audit.DecisionAllow,
		Reason:    "admin_granted",
		Kind:      audit.KindAuthLogin,
		Context:   string(blob),
	})
	// Discard the return: an admin-grant escalation is not read-class, so
	// FailurePosture would otherwise signal fail-closed. The `_ =`-discard
	// gives fail-open-but-loud without changing FailurePosture.
	_ = audit.FailurePosture(ctx, audit.ActionAdminGranted, err, "auth:admin_granted")
}

// Logout revokes the server-side session (immediate, by deleting the row) and
// clears the cookie. It is reachable without a valid session so a stale cookie
// can always be cleared.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	id := cookieValue(r, SessionCookieName)
	if id != "" {
		if err := h.sessions.Revoke(r.Context(), id); err != nil {
			slog.Warn("auth: session revoke failed", "error", err)
		}
	}
	h.sessions.ClearCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func (h *Handlers) setStateCookie(w http.ResponseWriter, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     stateCookiePath,
		MaxAge:   int(flowTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handlers) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     stateCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
