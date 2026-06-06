import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { createWrapper } from '@/test/utils';
import { SessionsPage } from './SessionsPage';
import { fetchSessions, updateSessionTitle, deleteSession } from '@/api/chat';
import type { Session } from '@/api/types';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('@/api/chat', () => ({
  fetchSessions: vi.fn(),
  updateSessionTitle: vi.fn(),
  deleteSession: vi.fn(),
}));

const mockFetch = vi.mocked(fetchSessions);
const mockRename = vi.mocked(updateSessionTitle);
const mockDelete = vi.mocked(deleteSession);

const sessions: Session[] = [
  {
    id: 's1',
    started_at: '2026-06-06T10:00:00Z',
    last_activity_at: '2026-06-06T11:00:00Z',
    title: 'Payment Service Crashloop',
    message_count: 4,
  },
  {
    id: 's2',
    started_at: '2026-06-05T09:00:00Z',
    last_activity_at: '2026-06-05T09:30:00Z',
    message_count: 1,
  },
];

function renderPage() {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <MemoryRouter>
        <SessionsPage />
      </MemoryRouter>
    </Wrapper>
  );
}

describe('SessionsPage', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    mockRename.mockReset();
    mockDelete.mockReset();
  });

  it('lists sessions labelled by title with a fallback for untitled ones', async () => {
    mockFetch.mockResolvedValue(sessions);
    renderPage();

    expect(await screen.findByText('Payment Service Crashloop')).toBeInTheDocument();
    // s2 has no title/summary, so it falls back.
    expect(screen.getByText('Untitled session')).toBeInTheDocument();
    expect(screen.getByText(/4 messages/)).toBeInTheDocument();
    expect(screen.getByText(/1 message$/)).toBeInTheDocument();
  });

  it('shows the empty state when there are no sessions', async () => {
    mockFetch.mockResolvedValue([]);
    renderPage();
    expect(await screen.findByText('No sessions yet')).toBeInTheDocument();
  });

  it('renames a session through the inline editor', async () => {
    mockFetch.mockResolvedValue(sessions);
    mockRename.mockResolvedValue({ ...sessions[0], title: 'New Name' });
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

  it('rejects an empty rename without calling the API', async () => {
    mockFetch.mockResolvedValue(sessions);
    renderPage();
    await screen.findByText('Payment Service Crashloop');

    const user = userEvent.setup();
    await user.click(screen.getAllByLabelText('Rename session')[0]);
    const input = screen.getByLabelText('Session title');
    await user.clear(input);
    await user.click(screen.getByLabelText('Save title'));

    expect(mockRename).not.toHaveBeenCalled();
  });

  it('deletes a session after confirmation', async () => {
    mockFetch.mockResolvedValue(sessions);
    mockDelete.mockResolvedValue();
    renderPage();
    await screen.findByText('Payment Service Crashloop');

    const user = userEvent.setup();
    await user.click(screen.getAllByLabelText('Delete session')[0]);

    // Confirm dialog appears; click its Delete button.
    const confirm = await screen.findByRole('button', { name: 'Delete' });
    await user.click(confirm);

    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith('s1'));
  });
});
