-- Identity Phase C: OIDC login + server-side sessions.
-- See docs/reference/joe-identity-design.md §2.3 (server-side session + cookie) and
-- docs/project/DECISIONS.md D-0006.
--
-- Two tables, both keyed on opaque random identifiers:
--
--   auth_sessions    — the post-login server-side session. The session cookie
--                      carries only the id; everything authoritative (the
--                      principal, the bounded expiry) lives here. Deleting a
--                      row revokes the session immediately (logout / forced
--                      revocation), which is the whole reason a server-side
--                      session was chosen over a stateless JWT (§2.3).
--
--   auth_login_flows — the short-lived, in-flight OIDC authorization-code
--                      state. Holds the PKCE code_verifier and the OIDC nonce
--                      keyed by the opaque `state` value, so the callback can
--                      validate state (CSRF), complete the PKCE exchange, and
--                      check the nonce. Rows are single-use: deleted on
--                      callback (success or failure) and expire quickly.

CREATE TABLE auth_sessions (
    id         TEXT PRIMARY KEY,
    principal  TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE INDEX idx_auth_sessions_expires_at ON auth_sessions (expires_at);

CREATE TABLE auth_login_flows (
    state         TEXT PRIMARY KEY,
    code_verifier TEXT NOT NULL,
    nonce         TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    expires_at    TEXT NOT NULL
);

CREATE INDEX idx_auth_login_flows_expires_at ON auth_login_flows (expires_at);
