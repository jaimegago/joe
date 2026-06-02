import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { apiClient } from '@/api/client';
import { loadToken, saveToken, clearStoredToken } from '@/api/tokenStorage';
import { useCurrentUser } from '@/hooks/useCurrentUser';

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
  // login sets the bearer token, persists it, and re-fetches /me. It
  // rejects (without persisting an invalid token) if /me does not accept
  // the credential, so the caller can surface an inline failure message.
  login: (token: string) => Promise<void>;
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

  const logout = useCallback(() => {
    apiClient.clearToken();
    clearStoredToken();
    setSessionExpired(true);
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
      login,
      logout,
    };
  }, [meQ.data, meQ.isError, sessionExpired, login, logout]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider');
  return ctx;
}
