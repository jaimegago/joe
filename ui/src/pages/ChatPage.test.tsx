import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import { createWrapper } from '@/test/utils';
import { ChatPage } from './ChatPage';
import { useAuth } from '@/auth/AuthContext';
import type { AuthContextValue } from '@/auth/AuthContext';
import type { ZoneAccess, Session } from '@/api/types';
import { useChat } from '@/hooks/useChat';
import { useRegime } from '@/hooks/useRegime';
import {
  fetchSession,
  updateSessionTitle,
  updateSessionVisibility,
  linkSessionToIncident,
} from '@/api/chat';

// ChatPage renders the access-pending empty state instead of the chat surface
// for a zero-zone, RBAC-enabled, non-admin user (OPERATOR_SURFACE_-
// VERIFICATION.md items 9/11), and gates the Phase 3 sharing controls on
// session ownership/visibility. These tests pin both.
vi.mock('@/auth/AuthContext', () => ({ useAuth: vi.fn() }));
vi.mock('@/hooks/useChat', () => ({ useChat: vi.fn() }));
vi.mock('@/hooks/useRegime', () => ({ useRegime: vi.fn() }));
vi.mock('@/api/chat', () => ({
  fetchSession: vi.fn(),
  updateSessionTitle: vi.fn(),
  updateSessionVisibility: vi.fn(),
  linkSessionToIncident: vi.fn(),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const mockUseAuth = vi.mocked(useAuth);
const mockUseChat = vi.mocked(useChat);
const mockUseRegime = vi.mocked(useRegime);
const mockFetchSession = vi.mocked(fetchSession);
const mockUpdateTitle = vi.mocked(updateSessionTitle);
const mockUpdateVisibility = vi.mocked(updateSessionVisibility);
const mockLinkIncident = vi.mocked(linkSessionToIncident);

// setRegime stubs useRegime; only the `mode` field of `data` is read by ChatPage.
function setRegime(mode: 'normal' | 'incident') {
  mockUseRegime.mockReturnValue({
    data: { mode, declaredAt: null, declaredByPrincipal: null, declaredKind: null },
  } as ReturnType<typeof useRegime>);
}

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
    mockUseRegime.mockReset();
    setRegime('normal');
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
    mockUseRegime.mockReset();
    mockFetchSession.mockReset();
    mockUpdateVisibility.mockReset();
    setRegime('normal');
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

describe('ChatPage URL sync', () => {
  // LocationSpy surfaces the live router pathname so the test can assert the
  // navigation ChatPage performs to keep the address bar on the session in view.
  function LocationSpy() {
    const loc = useLocation();
    return <div data-testid="loc">{loc.pathname}</div>;
  }

  function renderAt(path: string) {
    const { Wrapper } = createWrapper();
    render(
      <Wrapper>
        <MemoryRouter initialEntries={[path]}>
          <LocationSpy />
          <Routes>
            <Route path="/chat" element={<ChatPage />} />
            <Route path="/chat/:sessionId" element={<ChatPage />} />
          </Routes>
        </MemoryRouter>
      </Wrapper>
    );
  }

  beforeEach(() => {
    mockUseAuth.mockReset();
    mockUseChat.mockReset();
    mockUseRegime.mockReset();
    mockFetchSession.mockReset();
    setRegime('normal');
    setAuth({ rbacEnabled: false, isAdmin: true, zones: [] });
    mockFetchSession.mockResolvedValue({
      id: 's-new',
      started_at: '2026-06-06T10:00:00Z',
      message_count: 0,
      visibility: 'private',
    });
  });

  it('navigates a fresh /chat to /chat/{id} once a session is lazily created', async () => {
    // The session minted on the first message must appear in the URL so a
    // refresh or shared link reopens it.
    setChat('s-new');
    renderAt('/chat');
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent('/chat/s-new'));
  });

  it('returns to /chat when the session is cleared (New Session)', async () => {
    setChat(null);
    renderAt('/chat/s1');
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent(/^\/chat$/));
  });

  it('leaves the URL untouched when it already matches the session in view', async () => {
    setChat('s-new');
    renderAt('/chat/s-new');
    await waitFor(() => expect(mockFetchSession).toHaveBeenCalled());
    expect(screen.getByTestId('loc')).toHaveTextContent('/chat/s-new');
  });
});

describe('ChatPage incident linkage (Phase 4)', () => {
  const ownedUnlinked: Session = {
    id: 's1',
    started_at: '2026-06-06T10:00:00Z',
    message_count: 2,
    visibility: 'private',
  };

  beforeEach(() => {
    mockUseAuth.mockReset();
    mockUseChat.mockReset();
    mockUseRegime.mockReset();
    mockFetchSession.mockReset();
    mockLinkIncident.mockReset();
    setAuth({ rbacEnabled: false, isAdmin: true, zones: [] });
    setChat('s1');
  });

  it('shows "Attach to incident" for an owned unlinked session during an incident', async () => {
    setRegime('incident');
    mockFetchSession.mockResolvedValue(ownedUnlinked);
    mockLinkIncident.mockResolvedValue({ ...ownedUnlinked, linked_incident_id: 'inc-1' });
    renderPage();

    const attach = await screen.findByRole('button', { name: /attach to incident/i });
    const user = userEvent.setup();
    await user.click(attach);

    await waitFor(() => expect(mockLinkIncident).toHaveBeenCalledWith('s1'));
  });

  it('hides "Attach to incident" when no incident is active', async () => {
    setRegime('normal');
    mockFetchSession.mockResolvedValue(ownedUnlinked);
    renderPage();

    // The sharing control renders, so the session has loaded; the attach button must not.
    expect(await screen.findByRole('button', { name: /make public/i })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /attach to incident/i })).not.toBeInTheDocument();
  });

  it('shows a "Linked to incident" badge and no attach button once linked', async () => {
    setRegime('incident');
    mockFetchSession.mockResolvedValue({ ...ownedUnlinked, linked_incident_id: 'inc-1' });
    renderPage();

    expect(await screen.findByText(/linked to incident/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /attach to incident/i })).not.toBeInTheDocument();
  });
});

describe('ChatPage inline title rename', () => {
  const owned: Session = {
    id: 's1',
    started_at: '2026-06-06T10:00:00Z',
    message_count: 2,
    visibility: 'private',
    title: 'Old title',
  };

  beforeEach(() => {
    mockUseAuth.mockReset();
    mockUseChat.mockReset();
    mockUseRegime.mockReset();
    mockFetchSession.mockReset();
    mockUpdateTitle.mockReset();
    setRegime('normal');
    setAuth({ rbacEnabled: false, isAdmin: true, zones: [] });
    setChat('s1');
  });

  it('renames an owned session inline from the header', async () => {
    mockFetchSession.mockResolvedValue(owned);
    mockUpdateTitle.mockResolvedValue({ ...owned, title: 'New name' });
    renderPage();

    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /rename session/i }));

    const input = screen.getByRole('textbox', { name: /session title/i });
    await user.clear(input);
    await user.type(input, 'New name');
    await user.click(screen.getByRole('button', { name: /save title/i }));

    await waitFor(() => expect(mockUpdateTitle).toHaveBeenCalledWith('s1', 'New name'));
  });

  it('rejects an empty title without calling the API', async () => {
    mockFetchSession.mockResolvedValue(owned);
    renderPage();

    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /rename session/i }));
    await user.clear(screen.getByRole('textbox', { name: /session title/i }));
    await user.click(screen.getByRole('button', { name: /save title/i }));

    expect(mockUpdateTitle).not.toHaveBeenCalled();
  });

  it('offers no rename affordance to a read-only viewer', async () => {
    mockFetchSession.mockResolvedValue({ ...owned, read_only: true });
    renderPage();

    // Wait for the session to load (read-only viewer banner), then assert no
    // rename pencil for a non-owner.
    expect(await screen.findByText(/viewing a shared session/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /rename session/i })).not.toBeInTheDocument();
  });
});
