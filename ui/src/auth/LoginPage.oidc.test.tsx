import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { AuthContextValue } from './AuthContext';
import { LoginPage } from './LoginPage';

// These tests drive LoginPage's OIDC-vs-key presentation directly off the
// auth context's oidcEnabled flag by mocking useAuth — the real provider
// only renders LoginPage while unauthed, where (with /me protected)
// oidcEnabled would never be true. Mocking isolates the view's branching.

const h = vi.hoisted(() => ({
  oidcEnabled: false,
  loginWithOIDC: vi.fn(),
  login: vi.fn(),
}));

vi.mock('./AuthContext', () => ({
  useAuth: (): AuthContextValue => ({
    status: 'unauthed',
    principal: null,
    isAdmin: false,
    rbacEnabled: true,
    zones: [],
    oidcEnabled: h.oidcEnabled,
    login: h.login,
    loginWithOIDC: h.loginWithOIDC,
    logout: vi.fn(),
  }),
}));

describe('LoginPage OIDC presentation', () => {
  beforeEach(() => {
    h.loginWithOIDC.mockReset();
    h.login.mockReset();
    window.history.replaceState(null, '', '/');
  });

  it('shows the OIDC button and hides the key field behind a disclosure when oidc_enabled is true', async () => {
    h.oidcEnabled = true;
    render(<LoginPage />);

    // Primary OIDC button is present and triggers the context navigation.
    const oidcButton = screen.getByRole('button', { name: 'Sign in' });
    expect(oidcButton).toBeInTheDocument();

    // The key field is hidden until the disclosure is used.
    expect(screen.queryByLabelText('Service-account key')).not.toBeInTheDocument();

    await userEvent.click(oidcButton);
    expect(h.loginWithOIDC).toHaveBeenCalledTimes(1);

    // Reveal the break-glass key field via the disclosure control.
    await userEvent.click(screen.getByRole('button', { name: /use a service-account key/i }));
    expect(screen.getByLabelText('Service-account key')).toBeInTheDocument();
  });

  it('shows the key field directly and no OIDC button when oidc_enabled is false', () => {
    h.oidcEnabled = false;
    render(<LoginPage />);

    expect(screen.getByLabelText('Service-account key')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /use a service-account key/i })).not.toBeInTheDocument();
    // The only button is the key-submit; there is no separate OIDC button.
    expect(screen.queryByRole('button', { name: /^sign in$/i })).not.toBeInTheDocument();
  });
});
