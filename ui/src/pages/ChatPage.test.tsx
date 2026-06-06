import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { createWrapper } from '@/test/utils';
import { ChatPage } from './ChatPage';
import { useAuth } from '@/auth/AuthContext';
import type { AuthContextValue } from '@/auth/AuthContext';
import type { ZoneAccess, Session } from '@/api/types';
import { useChat } from '@/hooks/useChat';
import { fetchSession, updateSessionVisibility } from '@/api/chat';

// ChatPage renders the access-pending empty state instead of the chat surface
// for a zero-zone, RBAC-enabled, non-admin user (OPERATOR_SURFACE_-
// VERIFICATION.md items 9/11), and gates the Phase 3 sharing controls on
// session ownership/visibility. These tests pin both.
vi.mock('@/auth/AuthContext', () => ({ useAuth: vi.fn() }));
vi.mock('@/hooks/useChat', () => ({ useChat: vi.fn() }));
vi.mock('@/api/chat', () => ({
  fetchSession: vi.fn(),
  updateSessionVisibility: vi.fn(),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const mockUseAuth = vi.mocked(useAuth);
const mockUseChat = vi.mocked(useChat);
const mockFetchSession = vi.mocked(fetchSession);
const mockUpdateVisibility = vi.mocked(updateSessionVisibility);

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

function setChat(sessionId: string | null) {
  mockUseChat.mockReturnValue({
    sessionId,
    messages: [],
    isLoading: false,
    isSending: false,
    send: vi.fn(),
    startNewSession: vi.fn(),
  });
}

function renderPage() {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <MemoryRouter>
        <ChatPage />
      </MemoryRouter>
    </Wrapper>
  );
}

describe('ChatPage access-pending empty state', () => {
  beforeEach(() => {
    mockUseAuth.mockReset();
    mockUseChat.mockReset();
    setChat(null);
  });

  it('shows the access-pending empty state for a zero-zone non-admin (RBAC on)', () => {
    setAuth({ rbacEnabled: true, isAdmin: false, zones: [] });
    renderPage();
    expect(screen.getByText('Access pending')).toBeInTheDocument();
    expect(screen.getByText(/user:bob/)).toBeInTheDocument();
    expect(screen.getByText(/no zones have been assigned/i)).toBeInTheDocument();
  });

  it('shows the chat surface when the user has at least one zone', () => {
    setAuth({
      rbacEnabled: true,
      isAdmin: false,
      zones: [{ id: 'prod-readonly', allowed_actions: ['read'] }],
    });
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

describe('ChatPage sharing controls (Phase 3)', () => {
  const ownedPrivate: Session = {
    id: 's1',
    started_at: '2026-06-06T10:00:00Z',
    message_count: 2,
    visibility: 'private',
  };

  beforeEach(() => {
    mockUseAuth.mockReset();
    mockUseChat.mockReset();
    mockFetchSession.mockReset();
    mockUpdateVisibility.mockReset();
    setAuth({ rbacEnabled: false, isAdmin: true, zones: [] });
  });

  it('shows "Make public" for an owned private session and toggles it', async () => {
    setChat('s1');
    mockFetchSession.mockResolvedValue(ownedPrivate);
    mockUpdateVisibility.mockResolvedValue({ ...ownedPrivate, visibility: 'public' });
    renderPage();

    const makePublic = await screen.findByRole('button', { name: /make public/i });
    const user = userEvent.setup();
    await user.click(makePublic);

    await waitFor(() => expect(mockUpdateVisibility).toHaveBeenCalledWith('s1', 'public'));
  });

  it('shows "Make private" and a copy-link control for an owned public session', async () => {
    setChat('s1');
    mockFetchSession.mockResolvedValue({ ...ownedPrivate, visibility: 'public' });
    renderPage();

    expect(await screen.findByRole('button', { name: /make private/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /copy link/i })).toBeInTheDocument();
  });

  it('renders a read-only viewer with no composer for a non-owner public session', async () => {
    setChat('s1');
    mockFetchSession.mockResolvedValue({ ...ownedPrivate, visibility: 'public', read_only: true });
    renderPage();

    expect(await screen.findByText(/read-only mode/i)).toBeInTheDocument();
    // No share controls, no composer for a non-owner.
    expect(screen.queryByRole('button', { name: /make/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /new session/i })).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText(/message/i)).not.toBeInTheDocument();
  });

  it('shows no sharing controls before a session exists', () => {
    setChat(null);
    renderPage();
    expect(screen.queryByRole('button', { name: /make public/i })).not.toBeInTheDocument();
  });
});
