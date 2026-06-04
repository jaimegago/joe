import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ChatPage } from './ChatPage';
import { useAuth } from '@/auth/AuthContext';
import type { AuthContextValue } from '@/auth/AuthContext';
import type { ZoneAccess } from '@/api/types';

// ChatPage renders the access-pending empty state instead of the chat surface
// for a zero-zone, RBAC-enabled, non-admin user (OPERATOR_SURFACE_-
// VERIFICATION.md items 9/11). These tests pin that rendering condition.
vi.mock('@/auth/AuthContext', () => ({ useAuth: vi.fn() }));
vi.mock('@/hooks/useChat', () => ({
  useChat: () => ({
    sessionId: null,
    messages: [],
    isLoading: false,
    isSending: false,
    send: vi.fn(),
    startNewSession: vi.fn(),
  }),
}));
const mockUseAuth = vi.mocked(useAuth);

function setAuth(opts: { rbacEnabled: boolean; isAdmin: boolean; zones: ZoneAccess[] }) {
  mockUseAuth.mockReturnValue({
    status: 'authed',
    principal: 'user:bob',
    isAdmin: opts.isAdmin,
    rbacEnabled: opts.rbacEnabled,
    zones: opts.zones,
    oidcEnabled: false,
    login: vi.fn(),
    loginWithOIDC: vi.fn(),
    logout: vi.fn(),
  } satisfies AuthContextValue);
}

function renderPage() {
  render(
    <MemoryRouter>
      <ChatPage />
    </MemoryRouter>
  );
}

describe('ChatPage access-pending empty state', () => {
  beforeEach(() => mockUseAuth.mockReset());

  it('shows the access-pending empty state for a zero-zone non-admin (RBAC on)', () => {
    setAuth({ rbacEnabled: true, isAdmin: false, zones: [] });
    renderPage();
    expect(screen.getByText('Access pending')).toBeInTheDocument();
    expect(screen.getByText(/user:bob/)).toBeInTheDocument();
    expect(screen.getByText(/no zones have been assigned/i)).toBeInTheDocument();
  });

  it('shows the chat surface when the user has at least one zone', () => {
    setAuth({ rbacEnabled: true, isAdmin: false, zones: [{ id: 'prod-readonly', allowed_actions: ['read'] }] });
    renderPage();
    expect(screen.queryByText('Access pending')).not.toBeInTheDocument();
  });

  it('does not block an admin even with no zones', () => {
    setAuth({ rbacEnabled: true, isAdmin: true, zones: [] });
    renderPage();
    expect(screen.queryByText('Access pending')).not.toBeInTheDocument();
  });

  it('does not block in auth-disabled local dev (rbac off)', () => {
    setAuth({ rbacEnabled: false, isAdmin: true, zones: [] });
    renderPage();
    expect(screen.queryByText('Access pending')).not.toBeInTheDocument();
  });
});
