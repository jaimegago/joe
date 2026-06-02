import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { ReactNode } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { apiClient } from '@/api/client';
import { AuthProvider, useAuth } from './AuthContext';

// These tests drive the whole chain — apiClient.request → fetchCurrentUser →
// useCurrentUser → AuthProvider — through a mocked global fetch, so the
// logged-out transition is exercised on the real typed-error status, not on
// a stubbed hook.

interface FakeResponse {
  ok: boolean;
  status: number;
  json: () => Promise<unknown>;
}

function response(status: number, body: unknown): FakeResponse {
  return { ok: status >= 200 && status < 300, status, json: () => Promise.resolve(body) };
}

function renderWithAuth(children: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AuthProvider>{children}</AuthProvider>
    </QueryClientProvider>
  );
}

function Probe() {
  const { status, isAdmin, rbacEnabled, oidcEnabled, principal, login, logout } = useAuth();
  return (
    <div>
      <span data-testid="status">{status}</span>
      <span data-testid="admin">{String(isAdmin)}</span>
      <span data-testid="rbac">{String(rbacEnabled)}</span>
      <span data-testid="oidc">{String(oidcEnabled)}</span>
      <span data-testid="principal">{principal ?? ''}</span>
      <button onClick={() => void apiClient.get('/api/v1/graph').catch(() => undefined)}>ping</button>
      <button onClick={() => void login('break-glass-token').catch(() => undefined)}>login</button>
      <button onClick={() => logout()}>logout</button>
    </div>
  );
}

describe('AuthProvider', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    sessionStorage.clear();
    apiClient.clearToken();
    apiClient.setUnauthorizedHandler(null);
  });

  it('treats RBAC-off as authed regardless of token, with no login prompt', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        response(200, { principal: 'local', is_admin: true, rbac_enabled: false, oidc_enabled: false })
      )
    );
    renderWithAuth(<Probe />);

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('authed'));
    expect(screen.getByTestId('rbac')).toHaveTextContent('false');
    expect(screen.getByTestId('admin')).toHaveTextContent('true');
  });

  it('is authed when RBAC is on and /me succeeds', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(response(200, { principal: 'alice', is_admin: false, rbac_enabled: true, oidc_enabled: false }))
    );
    renderWithAuth(<Probe />);

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('authed'));
    expect(screen.getByTestId('rbac')).toHaveTextContent('true');
    expect(screen.getByTestId('admin')).toHaveTextContent('false');
    expect(screen.getByTestId('principal')).toHaveTextContent('alice');
  });

  it('is unauthed when RBAC is on and /me returns 401', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(401, { message: 'unauthorized' })));
    renderWithAuth(<Probe />);

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('unauthed'));
  });

  it('shows a neutral loading state until /me resolves', () => {
    // A never-resolving fetch keeps the query pending.
    vi.stubGlobal('fetch', vi.fn().mockReturnValue(new Promise<FakeResponse>(() => undefined)));
    renderWithAuth(<Probe />);

    expect(screen.getByTestId('status')).toHaveTextContent('loading');
  });

  it('transitions to unauthed when any request 401s after load', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown) => {
        if (String(url).includes('/api/v1/me')) {
          return Promise.resolve(response(200, { principal: 'alice', is_admin: false, rbac_enabled: true, oidc_enabled: false }));
        }
        return Promise.resolve(response(401, { message: 'session expired' }));
      })
    );
    renderWithAuth(<Probe />);

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('authed'));

    await userEvent.click(screen.getByRole('button', { name: 'ping' }));

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('unauthed'));
  });

  it('logout POSTs the server logout then clears local state', async () => {
    const calls: { url: string; method?: string }[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown, opts?: RequestInit) => {
        calls.push({ url: String(url), method: opts?.method });
        if (String(url).includes('/api/v1/auth/logout')) {
          return Promise.resolve(response(200, { ok: true }));
        }
        return Promise.resolve(response(200, { principal: 'alice', is_admin: false, rbac_enabled: true, oidc_enabled: true }));
      })
    );
    renderWithAuth(<Probe />);

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('authed'));
    sessionStorage.setItem('joe.auth.token', 'some-token');

    await userEvent.click(screen.getByRole('button', { name: 'logout' }));

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('unauthed'));
    // Server-side revoke was issued via POST before local state cleared.
    const logoutCall = calls.find((c) => c.url.includes('/api/v1/auth/logout'));
    expect(logoutCall?.method).toBe('POST');
    // Local credential is gone.
    expect(sessionStorage.getItem('joe.auth.token')).toBeNull();
  });

  it('clears local state even when the server logout POST fails (best-effort)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: unknown) => {
        if (String(url).includes('/api/v1/auth/logout')) {
          // A stale cookie / server error on logout must not block local clear.
          return Promise.resolve(response(500, { message: 'logout failed' }));
        }
        return Promise.resolve(response(200, { principal: 'alice', is_admin: false, rbac_enabled: true, oidc_enabled: true }));
      })
    );
    renderWithAuth(<Probe />);

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('authed'));
    sessionStorage.setItem('joe.auth.token', 'some-token');

    await userEvent.click(screen.getByRole('button', { name: 'logout' }));

    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('unauthed'));
    expect(sessionStorage.getItem('joe.auth.token')).toBeNull();
  });

  it('clears the session-expired flag so a logout then a valid login returns to authed in one session', async () => {
    // /me accepts every call; the only thing that moves auth state here is
    // logout() setting the session-expired flag and login() clearing it. If
    // login() failed to clear the flag, status would stay pinned to unauthed
    // after a successful re-login — this test guards that regression.
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(response(200, { principal: 'alice', is_admin: false, rbac_enabled: true, oidc_enabled: false }))
    );
    renderWithAuth(<Probe />);

    // authed → logout → unauthed
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('authed'));
    await userEvent.click(screen.getByRole('button', { name: 'logout' }));
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('unauthed'));

    // valid login in the same session → authed (flag must be cleared)
    await userEvent.click(screen.getByRole('button', { name: 'login' }));
    await waitFor(() => expect(screen.getByTestId('status')).toHaveTextContent('authed'));
    expect(screen.getByTestId('principal')).toHaveTextContent('alice');
  });
});
