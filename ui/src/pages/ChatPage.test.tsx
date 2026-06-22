import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route, useLocation, useNavigate } from 'react-router-dom';
import { createWrapper } from '@/test/utils';
import { ChatPage } from './ChatPage';
import { useAuth } from '@/auth/AuthContext';
import type { AuthContextValue } from '@/auth/AuthContext';
import type { ZoneAccess, Session } from '@/api/types';
import { useChat } from '@/hooks/useChat';
import { useRegime } from '@/hooks/useRegime';
import { ApiRequestError } from '@/api/client';
import {
  fetchSession,
  updateSessionTitle,
  linkSessionToIncident,
  promoteSessionToIncident,
} from '@/api/chat';

// ChatPage renders the access-pending empty state instead of the chat surface
// for a zero-zone, RBAC-enabled, non-admin user, and gates owner-only controls
// (rename, copy-link, incident-link) on ownership in the org-wide read model
// (every session readable; only the owner writes). These tests pin both.
vi.mock('@/auth/AuthContext', () => ({ useAuth: vi.fn() }));
vi.mock('@/hooks/useChat', () => ({ useChat: vi.fn() }));
vi.mock('@/hooks/useRegime', () => ({ useRegime: vi.fn() }));
vi.mock('@/api/chat', () => ({
  fetchSession: vi.fn(),
  updateSessionTitle: vi.fn(),
  linkSessionToIncident: vi.fn(),
  promoteSessionToIncident: vi.fn(),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const mockUseAuth = vi.mocked(useAuth);
const mockUseChat = vi.mocked(useChat);
const mockUseRegime = vi.mocked(useRegime);
const mockFetchSession = vi.mocked(fetchSession);
const mockUpdateTitle = vi.mocked(updateSessionTitle);
const mockLinkIncident = vi.mocked(linkSessionToIncident);
const mockPromote = vi.mocked(promoteSessionToIncident);

// setRegime stubs useRegime; ChatPage reads `mode` (regime gate), the captain
// (`declaredByPrincipal`), and the active incident master's id/title
// (`incidentSessionId`/`incidentTitle`) — the affordance function needs the
// master id to tell a session linked to the ACTIVE incident from one linked to a
// now-resolved one.
function setRegime(
  mode: 'normal' | 'incident',
  declaredByPrincipal: string | null = null,
  active: { incidentSessionId?: string | null; incidentTitle?: string | null } = {}
) {
  mockUseRegime.mockReturnValue({
    data: {
      mode,
      declaredAt: null,
      declaredByPrincipal,
      declaredKind: null,
      incidentSessionId: active.incidentSessionId ?? null,
      incidentState: null,
      incidentTitle: active.incidentTitle ?? null,
    },
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

function setChat(sessionId: string | null, locallyCreatedId: string | null = null) {
  mockUseChat.mockReturnValue({
    sessionId,
    locallyCreatedId,
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

// Reset the tab-scoped last-session store before EVERY test. Several describes
// render at a bare /chat with a fixed useChat mock; a lastSession left by an
// earlier test would trigger an unintended restore (and, with the fixed mock,
// a restore↔url-sync navigation loop). Tests that exercise restore set it
// explicitly in their own body after this runs.
beforeEach(() => {
  sessionStorage.clear();
});

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

describe('ChatPage owner controls (org-wide read model)', () => {
  const owned: Session = {
    id: 's1',
    started_at: '2026-06-06T10:00:00Z',
    message_count: 2,
    // The server marks an owned session read_only=false explicitly; owner
    // controls gate on this positive signal.
    read_only: false,
    type: 'default',
  };

  beforeEach(() => {
    mockUseAuth.mockReset();
    mockUseChat.mockReset();
    mockUseRegime.mockReset();
    mockFetchSession.mockReset();
    setRegime('normal');
    setAuth({ rbacEnabled: false, isAdmin: true, zones: [] });
  });

  it('shows owner controls (rename + copy link) for an owned session', async () => {
    setChat('s1');
    mockFetchSession.mockResolvedValue(owned);
    renderPage();

    expect(await screen.findByRole('button', { name: /copy link/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /rename session/i })).toBeInTheDocument();
    // The visibility toggle no longer exists.
    expect(
      screen.queryByRole('button', { name: /make (public|private)/i })
    ).not.toBeInTheDocument();
  });

  it('renders a read-only viewer (no composer, no owner controls) for a non-owner session', async () => {
    setChat('s1');
    mockFetchSession.mockResolvedValue({ ...owned, read_only: true });
    renderPage();

    expect(await screen.findByText(/read-only mode/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /copy link/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /rename session/i })).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText(/message/i)).not.toBeInTheDocument();
    // New Session stays available even while reading someone else's session.
    expect(screen.getByRole('button', { name: /new session/i })).toBeInTheDocument();
  });

  it('shows no owner controls before a session exists', () => {
    setChat(null);
    renderPage();
    expect(screen.queryByRole('button', { name: /copy link/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /rename session/i })).not.toBeInTheDocument();
  });

  it('fails closed: no owner controls when read_only is absent (unconfirmed ownership)', async () => {
    // Ownership requires read_only===false; an object missing the field (a
    // stale/partial cache entry) is treated as NOT owned, so no owner controls.
    setChat('s1');
    mockFetchSession.mockResolvedValue({
      id: 's1',
      started_at: '2026-06-06T10:00:00Z',
      message_count: 2,
      type: 'default',
      // read_only deliberately omitted
    });
    renderPage();

    await waitFor(() => expect(mockFetchSession).toHaveBeenCalled());
    expect(screen.queryByRole('button', { name: /copy link/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /rename session/i })).not.toBeInTheDocument();
  });

  it('shows "Session not found" when the session is gone (deleted)', async () => {
    setChat('s1');
    mockFetchSession.mockRejectedValue(new ApiRequestError(404, 'session not found'));
    renderPage();

    expect(await screen.findByText(/session not found/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /start a new chat/i })).toBeInTheDocument();
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
      read_only: false,
      type: 'default',
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

describe('ChatPage last-session restore', () => {
  // Returning to the Chat tab lands on the bare /chat route; the page must
  // reopen the last session viewed this tab instead of a blank "New chat".
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
    sessionStorage.clear();
    mockUseAuth.mockReset();
    mockUseChat.mockReset();
    mockUseRegime.mockReset();
    mockFetchSession.mockReset();
    setRegime('normal');
    setAuth({ rbacEnabled: false, isAdmin: true, zones: [] });
    // Mirror the real hook: the session in view follows the route param (the
    // restore effect drives the URL, useChat adopts it). The fixed-value
    // setChat() helper would peg sessionId regardless of the route and defeat
    // the activeSessionId↔URL coupling these tests exercise.
    mockUseChat.mockImplementation((initialSessionId?: string) => ({
      sessionId: initialSessionId ?? null,
      locallyCreatedId: null,
      messages: [],
      isLoading: false,
      isSending: false,
      send: vi.fn(),
      startNewSession: vi.fn(),
    }));
    mockFetchSession.mockResolvedValue({
      id: 's-prev',
      started_at: '2026-06-06T10:00:00Z',
      message_count: 2,
      read_only: false,
      type: 'default',
    });
  });

  it('redirects a bare /chat to the last session viewed this tab', async () => {
    sessionStorage.setItem('joe.chat.lastSession', 's-prev');
    renderAt('/chat');
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent('/chat/s-prev'));
  });

  it('stays on a blank /chat when no session was remembered', async () => {
    renderAt('/chat');
    // Give the restore effect a chance to fire before asserting it did nothing.
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent(/^\/chat$/));
  });

  it('remembers the session in view for the next return', async () => {
    renderAt('/chat/s-prev');
    await waitFor(() => expect(sessionStorage.getItem('joe.chat.lastSession')).toBe('s-prev'));
  });

  it('forgets a restored session that 404s and falls back to /chat', async () => {
    // Restore (bare /chat -> /chat/s-gone) into a session that no longer
    // exists: the 404 recovery clears it and returns to a blank chat.
    sessionStorage.setItem('joe.chat.lastSession', 's-gone');
    mockFetchSession.mockRejectedValue(new ApiRequestError(404, 'session not found'));
    renderAt('/chat');
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent(/^\/chat$/));
    expect(sessionStorage.getItem('joe.chat.lastSession')).toBeNull();
  });

  it('forgets a restored session that is read-only (another principal owns it) and falls back to /chat', async () => {
    // The "stuck on someone else's session" bug: a read-only foreign session got
    // saved as the last session and the Chat tab kept reopening it — a dead-end
    // the user could not write to (and, when empty, could not escape). A restored
    // session that resolves read_only=true is now forgotten, same as a 404.
    sessionStorage.setItem('joe.chat.lastSession', 's-shared');
    mockFetchSession.mockResolvedValue({
      id: 's-shared',
      started_at: '2026-06-06T10:00:00Z',
      message_count: 0,
      read_only: true,
      type: 'default',
    });
    renderAt('/chat');
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent(/^\/chat$/));
    expect(sessionStorage.getItem('joe.chat.lastSession')).toBeNull();
  });

  it('does not remember a directly-opened read-only session (it must not become the sticky default)', async () => {
    // Viewing someone else's session directly stays put (the shared link works),
    // but it must never be saved as the last session — otherwise the next Chat
    // tab click would strand the user on it.
    mockFetchSession.mockResolvedValue({
      id: 's-shared',
      started_at: '2026-06-06T10:00:00Z',
      message_count: 0,
      read_only: true,
      type: 'default',
    });
    renderAt('/chat/s-shared');
    await waitFor(() => expect(mockFetchSession).toHaveBeenCalled());
    expect(screen.getByTestId('loc')).toHaveTextContent('/chat/s-shared');
    expect(sessionStorage.getItem('joe.chat.lastSession')).toBeNull();
  });

  it('does not reset a directly-opened session that errors (no restore)', async () => {
    // A session error on a URL the user opened directly (not restored) must
    // never bounce them to a blank "New chat" — this is what regressed the
    // auto-title: a transient/early error reset the live session.
    mockFetchSession.mockRejectedValue(new ApiRequestError(404, 'session not found'));
    renderAt('/chat/s-direct');
    await waitFor(() => expect(mockFetchSession).toHaveBeenCalled());
    expect(screen.getByTestId('loc')).toHaveTextContent('/chat/s-direct');
  });

  it('does not reset a restored session on a transient (non-404) error', async () => {
    // A 5xx/transient blip on the restored session must not nuke it — only a
    // genuine 404 (gone/forbidden) does.
    sessionStorage.setItem('joe.chat.lastSession', 's-prev');
    mockFetchSession.mockRejectedValue(new ApiRequestError(503, 'unavailable'));
    renderAt('/chat');
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent('/chat/s-prev'));
    expect(sessionStorage.getItem('joe.chat.lastSession')).toBe('s-prev');
  });

  it('reopens the last session on an in-place /chat link (reused component, not remount)', async () => {
    // The empty-page regression: navigating /chat/{id} → /chat (sidebar "Chat")
    // reuses ChatPage rather than remounting it, so a mount-only restore never
    // re-fired and the user was stranded on a blank "New chat". Restore is now
    // reactive, so an in-place return to bare /chat reopens the last session.
    sessionStorage.setItem('joe.chat.lastSession', 's-prev');
    function GoChat() {
      const navigate = useNavigate();
      return <button onClick={() => navigate('/chat')}>go chat</button>;
    }
    const { Wrapper } = createWrapper();
    render(
      <Wrapper>
        <MemoryRouter initialEntries={['/chat/s-prev']}>
          <LocationSpy />
          <GoChat />
          <Routes>
            <Route path="/chat" element={<ChatPage />} />
            <Route path="/chat/:sessionId" element={<ChatPage />} />
          </Routes>
        </MemoryRouter>
      </Wrapper>
    );
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent('/chat/s-prev'));

    // Navigate to bare /chat in place; reactive restore must bounce back.
    await userEvent.click(screen.getByRole('button', { name: /go chat/i }));
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent('/chat/s-prev'));
  });
});

describe('ChatPage incident linkage (Phase 4)', () => {
  const ownedUnlinked: Session = {
    id: 's1',
    started_at: '2026-06-06T10:00:00Z',
    message_count: 2,
    read_only: false,
    type: 'default',
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

    // An owner control renders, so the session has loaded; the attach button must not.
    expect(await screen.findByRole('button', { name: /copy link/i })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /attach to incident/i })).not.toBeInTheDocument();
  });

  it('shows a "Linked to incident" badge and no attach button once linked', async () => {
    setRegime('incident', null, { incidentSessionId: 'inc-1' });
    mockFetchSession.mockResolvedValue({ ...ownedUnlinked, linked_incident_id: 'inc-1' });
    renderPage();

    expect(await screen.findByText(/linked to incident/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /attach to incident/i })).not.toBeInTheDocument();
  });
});

describe('ChatPage promote-this-session affordance (§12.10)', () => {
  const owned: Session = {
    id: 's1',
    started_at: '2026-06-06T10:00:00Z',
    message_count: 2,
    read_only: false,
    type: 'default',
  };

  beforeEach(() => {
    mockUseAuth.mockReset();
    mockUseChat.mockReset();
    mockUseRegime.mockReset();
    mockFetchSession.mockReset();
    mockPromote.mockReset();
    setAuth({ rbacEnabled: false, isAdmin: true, zones: [] });
    setChat('s1');
  });

  it('promote-this-session invokes the promote-incident route when no incident is active', async () => {
    setRegime('normal');
    mockFetchSession.mockResolvedValue(owned);
    mockPromote.mockResolvedValue({
      session_id: 's1',
      captain_id: 'cap1',
      declared_by: 'user:bob',
    });
    renderPage();

    const declare = await screen.findByRole('button', { name: /declare incident/i });
    const user = userEvent.setup();
    await user.click(declare);

    await waitFor(() => expect(mockPromote).toHaveBeenCalledWith('s1'));
  });

  it('hides the chat-view declare control while an incident is already active', async () => {
    setRegime('incident');
    mockFetchSession.mockResolvedValue(owned);
    renderPage();

    // The session loads (copy-link owner control renders); the in-chat declare
    // button must not, since there is already an active incident.
    expect(await screen.findByRole('button', { name: /copy link/i })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /declare incident/i })).not.toBeInTheDocument();
  });
});

// These pin the INCIDENT-CHROME-AFFORDANCES defects directly at the chat header:
// an incident-type session (active OR resolved) must never offer attach/declare;
// a resolved master is a terminal record (resolved badge only); a linked session
// names and links to its master.
describe('ChatPage incident affordance matrix (INCIDENT-CHROME-AFFORDANCES)', () => {
  beforeEach(() => {
    mockUseAuth.mockReset();
    mockUseChat.mockReset();
    mockUseRegime.mockReset();
    mockFetchSession.mockReset();
    mockPromote.mockReset();
    mockLinkIncident.mockReset();
    setAuth({ rbacEnabled: false, isAdmin: true, zones: [] });
    setChat('s1');
  });

  it('an active incident master offers neither attach nor declare (defects 1 & 3b)', async () => {
    setRegime('incident', 'user:bob', { incidentSessionId: 's1' });
    mockFetchSession.mockResolvedValue({
      id: 's1',
      started_at: '2026-06-06T10:00:00Z',
      message_count: 2,
      read_only: false,
      type: 'incident',
      incident_state: 'declared',
    });
    renderPage();

    // The incident-session badge confirms the master loaded.
    expect(await screen.findByText(/incident session/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /attach to incident/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /declare incident/i })).not.toBeInTheDocument();
  });

  it('a resolved incident master shows a resolved badge and no resolve/declare/attach (defects 1, 5)', async () => {
    // Post-resolve: regime is back to normal, the master is type=incident state=resolved.
    setRegime('normal');
    mockFetchSession.mockResolvedValue({
      id: 's1',
      started_at: '2026-06-06T10:00:00Z',
      message_count: 2,
      read_only: false,
      type: 'incident',
      incident_state: 'resolved',
    });
    renderPage();

    expect(await screen.findByText(/incident · resolved/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /declare incident/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /attach to incident/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /resolve incident/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /mark mitigated/i })).not.toBeInTheDocument();
  });

  it('a linked session names and links to its incident master (defect 2)', async () => {
    setRegime('incident', 'user:carol', { incidentSessionId: 'inc-1', incidentTitle: 'DB outage' });
    mockFetchSession.mockResolvedValue({
      id: 's1',
      started_at: '2026-06-06T10:00:00Z',
      message_count: 2,
      read_only: false,
      type: 'default',
      linked_incident_id: 'inc-1',
      linked_incident_title: 'DB outage',
    });
    renderPage();

    const link = await screen.findByRole('link', { name: /linked to db outage/i });
    expect(link).toHaveAttribute('href', '/chat/inc-1');
    expect(screen.queryByRole('button', { name: /attach to incident/i })).not.toBeInTheDocument();
  });

  it('a session linked to a now-resolved incident shows a muted linked badge and no attach', async () => {
    // A different incident is active now; this session is linked to the OLD,
    // resolved one — it must not offer attach (re-linking is a deferred node).
    setRegime('incident', 'user:carol', { incidentSessionId: 'inc-new' });
    mockFetchSession.mockResolvedValue({
      id: 's1',
      started_at: '2026-06-06T10:00:00Z',
      message_count: 2,
      read_only: false,
      type: 'default',
      linked_incident_id: 'inc-old',
      linked_incident_title: 'Old outage',
    });
    renderPage();

    const link = await screen.findByRole('link', { name: /linked to old outage/i });
    expect(link).toHaveAttribute('href', '/chat/inc-old');
    expect(screen.queryByRole('button', { name: /attach to incident/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /declare incident/i })).not.toBeInTheDocument();
  });
});

describe('ChatPage incident participants — creator vs captain (§12.3)', () => {
  beforeEach(() => {
    mockUseAuth.mockReset();
    mockUseChat.mockReset();
    mockUseRegime.mockReset();
    mockFetchSession.mockReset();
    setAuth({ rbacEnabled: false, isAdmin: true, zones: [] });
    setChat('s1');
  });

  it('renders the creator and the captain as distinct principals on an incident session', async () => {
    // Owner viewing (read_only=false → creator is the caller, user:bob), linked to
    // an incident whose captain (the regime declarer) is a DIFFERENT principal.
    setRegime('incident', 'user:carol', { incidentSessionId: 'inc-1' });
    mockFetchSession.mockResolvedValue({
      id: 's1',
      started_at: '2026-06-06T10:00:00Z',
      message_count: 2,
      read_only: false,
      linked_incident_id: 'inc-1',
      type: 'default',
    });
    renderPage();

    expect(await screen.findByText(/created by/i)).toHaveTextContent('bob');
    const captain = screen.getByText(/incident captain/i);
    expect(captain).toHaveTextContent('carol');
    // The two roles are distinct principals — never conflated.
    expect(captain).not.toHaveTextContent('bob');
  });
});

describe('ChatPage inline title rename', () => {
  const owned: Session = {
    id: 's1',
    started_at: '2026-06-06T10:00:00Z',
    message_count: 2,
    title: 'Old title',
    read_only: false,
    type: 'default',
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
