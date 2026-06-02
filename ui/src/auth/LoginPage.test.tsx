import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { apiClient } from '@/api/client';
import { AuthProvider, useAuth } from './AuthContext';
import { LoginPage } from './LoginPage';

// Renders the gate exactly as the app does: LoginPage while unauthed, an
// "authed" marker otherwise — so the test asserts the real status-driven
// transition rather than calling login() directly.
function Gate() {
  const { status } = useAuth();
  if (status === 'authed') return <div data-testid="app">app</div>;
  return <LoginPage />;
}

function renderLogin() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <Gate />
      </AuthProvider>
    </QueryClientProvider>
  );
}

describe('LoginPage', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    sessionStorage.clear();
    apiClient.clearToken();
    apiClient.setUnauthorizedHandler(null);
  });

  it('shows an inline failure and does not persist the token on a 401', async () => {
    // /me is 401 until a valid key is supplied; here every attempt 401s.
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
      json: () => Promise.resolve({ message: 'unauthorized' }),
    }));
    renderLogin();

    await waitFor(() => expect(screen.getByLabelText('Service-account key')).toBeInTheDocument());
    await userEvent.type(screen.getByLabelText('Service-account key'), 'bad-key');
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }));

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/authentication failed/i));
    expect(screen.queryByTestId('app')).not.toBeInTheDocument();
    expect(sessionStorage.getItem('joe.auth.token')).toBeNull();
  });

  it('transitions to the app and persists the token on a valid key', async () => {
    let validKeyAccepted = false;
    vi.stubGlobal(
      'fetch',
      vi.fn((_url: unknown, opts?: RequestInit) => {
        const auth = (opts?.headers as Record<string, string> | undefined)?.Authorization;
        if (auth === 'Bearer good-key') validKeyAccepted = true;
        return Promise.resolve(
          validKeyAccepted
            ? {
                ok: true,
                status: 200,
                json: () =>
                  Promise.resolve({
                    principal: 'alice',
                    is_admin: false,
                    rbac_enabled: true,
                    oidc_enabled: false,
                  }),
              }
            : { ok: false, status: 401, json: () => Promise.resolve({ message: 'unauthorized' }) }
        );
      })
    );
    renderLogin();

    await waitFor(() => expect(screen.getByLabelText('Service-account key')).toBeInTheDocument());
    await userEvent.type(screen.getByLabelText('Service-account key'), 'good-key');
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }));

    await waitFor(() => expect(screen.getByTestId('app')).toBeInTheDocument());
    expect(sessionStorage.getItem('joe.auth.token')).toBe('good-key');
  });
});
