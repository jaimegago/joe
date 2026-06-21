import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { createWrapper } from '@/test/utils';
import { SessionsPage } from './SessionsPage';
import {
  fetchSessions,
  fetchTrash,
  updateSessionTitle,
  deleteSession,
  restoreSession,
  promoteSessionToIncident,
  createSession,
} from '@/api/chat';
import type { Session } from '@/api/types';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('@/api/chat', () => ({
  fetchSessions: vi.fn(),
  fetchTrash: vi.fn(),
  updateSessionTitle: vi.fn(),
  deleteSession: vi.fn(),
  restoreSession: vi.fn(),
  promoteSessionToIncident: vi.fn(),
  createSession: vi.fn(),
}));

const mockFetch = vi.mocked(fetchSessions);
const mockFetchTrash = vi.mocked(fetchTrash);
const mockRename = vi.mocked(updateSessionTitle);
const mockDelete = vi.mocked(deleteSession);
const mockRestore = vi.mocked(restoreSession);
const mockPromote = vi.mocked(promoteSessionToIncident);
const mockCreate = vi.mocked(createSession);

const owned: Session[] = [
  {
    id: 's1',
    started_at: '2026-06-06T10:00:00Z',
    last_activity_at: '2026-06-06T11:00:00Z',
    title: 'Payment Service Crashloop',
    message_count: 4,
    read_only: false,
  },
  {
    id: 's2',
    started_at: '2026-06-05T09:00:00Z',
    last_activity_at: '2026-06-05T09:30:00Z',
    message_count: 1,
    read_only: false,
  },
];

function renderPage(initialEntry = '/sessions') {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <MemoryRouter initialEntries={[initialEntry]}>
        <SessionsPage />
      </MemoryRouter>
    </Wrapper>
  );
}

describe('SessionsPage', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    mockFetchTrash.mockReset();
    mockRename.mockReset();
    mockDelete.mockReset();
    mockRestore.mockReset();
    mockPromote.mockReset();
    mockCreate.mockReset();
    mockFetchTrash.mockResolvedValue([]);
  });

  it('lists the team-wide sessions labelled by title with a fallback for untitled ones', async () => {
    mockFetch.mockResolvedValue(owned);
    renderPage();

    expect(await screen.findByText('Payment Service Crashloop')).toBeInTheDocument();
    expect(screen.getByText('Untitled session')).toBeInTheDocument();
    expect(screen.getByText(/4 messages/)).toBeInTheDocument();
    expect(screen.getByText(/1 message$/)).toBeInTheDocument();
    // Default view is the team-wide list (mine=false).
    expect(mockFetch).toHaveBeenCalledWith({ mine: false, limit: 50 });
  });

  it('marks a session owned by another principal read-only and owner-attributed', async () => {
    mockFetch.mockResolvedValue([
      {
        id: 'x1',
        started_at: '2026-06-06T08:00:00Z',
        title: 'Cluster upgrade runbook',
        message_count: 7,
        read_only: true,
        shared_by: 'user:alice@example.com',
      },
    ]);
    renderPage();

    expect(await screen.findByText('Cluster upgrade runbook')).toBeInTheDocument();
    expect(screen.getByText('Read-only')).toBeInTheDocument();
    expect(screen.getByText(/owned by alice@example\.com/)).toBeInTheDocument();
    // A read-only row exposes no owner-only mutators.
    expect(screen.queryByLabelText('Rename session')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Delete session')).not.toBeInTheDocument();
  });

  it('the "Mine" filter narrows to the caller\'s own sessions', async () => {
    mockFetch.mockResolvedValue(owned);
    renderPage();
    await screen.findByText('Payment Service Crashloop');

    const user = userEvent.setup();
    await user.click(screen.getByRole('tab', { name: 'Mine' }));

    await waitFor(() => expect(mockFetch).toHaveBeenCalledWith({ mine: true, limit: 50 }));
  });

  it('shows the empty state when there are no sessions', async () => {
    mockFetch.mockResolvedValue([]);
    renderPage();
    expect(await screen.findByText('No sessions yet')).toBeInTheDocument();
  });

  it('renames a session through the inline editor', async () => {
    mockFetch.mockResolvedValue(owned);
    mockRename.mockResolvedValue({ ...owned[0], title: 'New Name' });
    renderPage();
    await screen.findByText('Payment Service Crashloop');

    const user = userEvent.setup();
    await user.click(screen.getAllByLabelText('Rename session')[0]);
    const input = screen.getByLabelText('Session title');
    await user.clear(input);
    await user.type(input, 'New Name');
    await user.click(screen.getByLabelText('Save title'));

    await waitFor(() => expect(mockRename).toHaveBeenCalledWith('s1', 'New Name'));
  });

  it('delete TRASHES the session (soft-delete) rather than destroying it', async () => {
    mockFetch.mockResolvedValue(owned);
    mockDelete.mockResolvedValue();
    renderPage();
    await screen.findByText('Payment Service Crashloop');

    const user = userEvent.setup();
    await user.click(screen.getAllByLabelText('Delete session')[0]);

    // The confirm is framed as a move-to-trash (recoverable), not a destroy.
    expect(await screen.findByText(/will be moved to trash/i)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Move to trash' }));

    // It calls the soft-delete endpoint (DELETE = trash), not a hard delete.
    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith('s1'));
  });

  it('the Trash view shows trashed sessions with remaining time and restores them', async () => {
    mockFetch.mockResolvedValue([]);
    mockFetchTrash.mockResolvedValue([
      {
        id: 't1',
        started_at: '2026-06-01T10:00:00Z',
        title: 'Old debugging session',
        message_count: 3,
        purge_after: '2026-07-01T10:00:00Z',
      },
    ]);
    mockRestore.mockResolvedValue({
      id: 't1',
      started_at: '2026-06-01T10:00:00Z',
      message_count: 3,
    });
    renderPage();

    const user = userEvent.setup();
    await user.click(screen.getByRole('tab', { name: 'Trash' }));

    expect(await screen.findByText('Old debugging session')).toBeInTheDocument();
    // Remaining-time-before-purge is rendered from purge_after.
    expect(screen.getByText(/purges/i)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /restore/i }));
    await waitFor(() => expect(mockRestore).toHaveBeenCalledWith('t1'));
  });

  it('declare mode: a per-row promote invokes the promote-incident route', async () => {
    mockFetch.mockResolvedValue(owned);
    mockPromote.mockResolvedValue({
      session_id: 's1',
      captain_id: 'cap1',
      declared_by: 'user:bob',
    });
    renderPage('/sessions?declare=1');
    await screen.findByText('Payment Service Crashloop');

    const user = userEvent.setup();
    await user.click(screen.getAllByRole('button', { name: /promote to incident/i })[0]);

    await waitFor(() => expect(mockPromote).toHaveBeenCalledWith('s1'));
  });

  it('declare mode: start-new sequences create-empty-then-promote', async () => {
    mockFetch.mockResolvedValue(owned);
    mockCreate.mockResolvedValue({
      id: 'new1',
      started_at: '2026-06-06T12:00:00Z',
      message_count: 0,
      read_only: false,
    });
    mockPromote.mockResolvedValue({
      session_id: 'new1',
      captain_id: 'cap1',
      declared_by: 'user:bob',
    });
    renderPage('/sessions?declare=1');
    await screen.findByText('Payment Service Crashloop');

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /start a new incident/i }));

    // create-empty THEN promote-in-place: two existing primitives sequenced in the UI.
    await waitFor(() => expect(mockCreate).toHaveBeenCalled());
    await waitFor(() => expect(mockPromote).toHaveBeenCalledWith('new1'));
  });
});
