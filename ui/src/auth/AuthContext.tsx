import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { apiClient, API_BASE } from '@/api/client';
import { loadToken, saveToken, clearStoredToken } from '@/api/tokenStorage';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { useAuthConfig } from '@/hooks/useAuthConfig';

// Stream H1 — web UI authentication context.
//
// Owns the app-wide auth state derived from GET /api/v1/me and the
// dev / break-glass bearer-token login. Behaviour by case:
//   - rbac_enabled=false → always 'authed' (permit-all local dev); no login.
//   - rbac_enabled=true + /me 200 → 'authed'.
//   - rbac_enabled=true + /me 401 → 'unauthed' → LoginPage.
//   - any runtime 401 from any request → 'unauthed' (token cleared).
// The unauthed transition is driven by the typed ApiRequestError status,
// never by matching error message strings.

export type AuthStatus = 'loading' | 'authed' | 'unauthed';

export interface AuthContextValue {
  status: AuthStatus;
  principal: string | null;
  isAdmin: boolean;
  rbacEnabled: boolean;
  // oidcEnabled reflects the server's app-wide OIDC-configured flag, read
  // from the public GET /api/v1/auth/config endpoint (NOT /me). /me sits
  // behind the edge gate and 401s pre-auth, so a /me-sourced flag would
  // never be true on the cold logged-out shell — the OIDC button could not
  // appear. The public endpoint resolves with no credential, so the login
  // view reads it to decide whether to offer the OIDC button; false until
  // the public config resolves.
  oidcEnabled: boolean;
  // login sets the bearer token, persists it, and re-fetches /me. It
  // rejects (without persisting an invalid token) if /me does not accept
  // the credential, so the caller can surface an inline failure message.
  login: (token: string) => Promise<void>;
  // loginWithOIDC starts the human OIDC login by navigating the whole
  // browser to the server's /api/v1/auth/login endpoint. It is a full-page
  // navigation, not a fetch — the IdP round-trip (redirect to the IdP and
  // back to the callback) cannot happen inside fetch. The function does not
  // return; the page unloads.
  loginWithOIDC: () => void;
  // logout best-effort POSTs the server logout (revoking the server-side
  // session and clearing the cookie), then clears local state and
  // transitions to unauthed. A failed POST (e.g. a stale cookie) still
  // clears local state.
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  // Rehydrate any persisted token into apiClient synchronously, before the
  // first render commits and therefore before the /me query's queryFn runs,
  // so a valid persisted token does not produce a spurious 401 on load. A
  // lazy useState initializer runs exactly once, on first render.
  useState(() => {
    const persisted = loadToken();
    if (persisted) apiClient.setToken(persisted);
    return null;
  });

  // sessionExpired flips a previously-authed session to logged-out when a
  // runtime 401 (or explicit logout) occurs, without re-triggering the /me
  // query in a loop. Cleared on a successful login.
  const [sessionExpired, setSessionExpired] = useState(false);

  const meQ = useCurrentUser();
  // The OIDC-button signal comes from the public auth-config endpoint, fetched
  // unauthed on load so it is available on the cold logged-out shell before
  // any /me result. /me drives the authed state below; only this login-style
  // capability flag reads from the public endpoint.
  const authConfigQ = useAuthConfig();

  // Register the global 401 handler: clear the credential and force the
  // logged-out state from anywhere a request 401s after load.
  useEffect(() => {
    apiClient.setUnauthorizedHandler(() => {
      apiClient.clearToken();
      clearStoredToken();
      setSessionExpired(true);
    });
    return () => apiClient.setUnauthorizedHandler(null);
  }, []);

  const login = useCallback(
    async (token: string) => {
      apiClient.setToken(token);
      saveToken(token);
      setSessionExpired(false);
      const res = await meQ.refetch();
      if (res.status === 'error') {
        // Do not persist a credential that /me rejected.
        apiClient.clearToken();
        clearStoredToken();
        throw res.error ?? new Error('login failed');
      }
    },
    [meQ]
  );

  const loginWithOIDC = useCallback(() => {
    // Full-page navigation to the server login endpoint, which 302s to the
    // IdP. Not a fetch: the IdP round-trip ends by redirecting the browser
    // back to the callback, which sets the cookie and lands on "/".
    window.location.assign(`${API_BASE}/api/v1/auth/login`);
  }, []);

  // logout returns void (callers wire it straight to onClick) but does
  // async work: it best-effort POSTs the server logout before clearing
  // local state. A failed POST (e.g. a stale or missing cookie) must NOT
  // block the local clear, so it is swallowed.
  const logout = useCallback(() => {
    void (async () => {
      try {
        await apiClient.post('/api/v1/auth/logout', {});
      } catch {
        // ignore — proceed to clear local state regardless
      }
      apiClient.clearToken();
      clearStoredToken();
      setSessionExpired(true);
    })();
  }, []);

  const value = useMemo<AuthContextValue>(() => {
    const data = meQ.data;
    let status: AuthStatus;
    if (sessionExpired) {
      status = 'unauthed';
    } else if (meQ.isError) {
      status = 'unauthed';
    } else if (data) {
      status = 'authed';
    } else {
      status = 'loading';
    }

    return {
      status,
      principal: data?.principal ?? null,
      isAdmin: data?.is_admin ?? false,
      rbacEnabled: data?.rbac_enabled ?? false,
      oidcEnabled: authConfigQ.data?.oidc_enabled ?? false,
      login,
      loginWithOIDC,
      logout,
    };
  }, [meQ.data, meQ.isError, authConfigQ.data, sessionExpired, login, loginWithOIDC, logout]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider');
  return ctx;
}
