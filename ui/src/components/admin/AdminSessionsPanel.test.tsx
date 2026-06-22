import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createWrapper } from '@/test/utils';
import { AdminSessionsPanel } from './AdminSessionsPanel';
import {
  fetchAdminSessions,
  fetchAdminTrash,
  fetchRetentionPolicy,
  previewPurge,
  confirmPurge,
  archiveSession,
  restoreArchive,
} from '@/api/adminSessions';
import { useRegime } from '@/hooks/useRegime';
import type { Session } from '@/api/types';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('@/hooks/useRegime', () => ({ useRegime: vi.fn() }));
vi.mock('@/api/adminSessions', () => ({
  fetchAdminSessions: vi.fn(),
  fetchAdminTrash: vi.fn(),
  fetchRetentionPolicy: vi.fn(),
  updateRetentionPolicy: vi.fn(),
  previewPurge: vi.fn(),
  confirmPurge: vi.fn(),
  archiveSession: vi.fn(),
  restoreArchive: vi.fn(),
}));

const mockList = vi.mocked(fetchAdminSessions);
const mockTrash = vi.mocked(fetchAdminTrash);
const mockPolicy = vi.mocked(fetchRetentionPolicy);
const mockPreview = vi.mocked(previewPurge);
const mockConfirm = vi.mocked(confirmPurge);
const mockArchive = vi.mocked(archiveSession);
const mockRestoreArchive = vi.mocked(restoreArchive);
const mockUseRegime = vi.mocked(useRegime);

const active: Session = {
  id: 'a1',
  started_at: '2026-06-06T10:00:00Z',
  title: 'Active session',
  message_count: 5,
  creator_principal: 'user:alice@example.com',
  type: 'default',
};

function renderPanel() {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <AdminSessionsPanel />
    </Wrapper>
  );
}

describe('AdminSessionsPanel', () => {
  beforeEach(() => {
    mockList.mockReset();
    mockTrash.mockReset();
    mockPolicy.mockReset();
    mockPreview.mockReset();
    mockConfirm.mockReset();
    mockArchive.mockReset();
    mockRestoreArchive.mockReset();
    mockUseRegime.mockReset();
    mockUseRegime.mockReturnValue({ data: { mode: 'normal' } } as ReturnType<typeof useRegime>);
    mockPolicy.mockResolvedValue({
      inactivity_days: null,
      inactivity_window: 'off',
      trash_grace_days: 30,
      terminal_action: 'trash_then_purge',
    });
    mockList.mockResolvedValue([active]);
    mockTrash.mockResolvedValue([]);
  });

  it('shows the creator principal on a cross-tenant row', async () => {
    renderPanel();
    expect(await screen.findByText('Active session')).toBeInTheDocument();
    expect(screen.getByText('alice@example.com')).toBeInTheDocument();
    // Default state filter is active.
    await waitFor(() => expect(mockList).toHaveBeenCalledWith({ state: 'active', limit: 200 }));
  });

  it('the state filter switches active / trashed / archived', async () => {
    mockTrash.mockResolvedValue([
      {
        id: 't1',
        started_at: '2026-06-01T10:00:00Z',
        title: 'Trashed one',
        message_count: 1,
        purge_after: '2026-07-01T10:00:00Z',
        type: 'default',
      },
    ]);
    renderPanel();
    await screen.findByText('Active session');

    const user = userEvent.setup();
    await user.click(screen.getByRole('tab', { name: 'Trashed' }));
    // Trashed goes through the dedicated all-trash route (carries purge_after).
    await waitFor(() => expect(mockTrash).toHaveBeenCalled());
    expect(await screen.findByText('Trashed one')).toBeInTheDocument();

    await user.click(screen.getByRole('tab', { name: 'Archived' }));
    await waitFor(() => expect(mockList).toHaveBeenCalledWith({ state: 'archived', limit: 200 }));
  });

  it('the purge two-step requires explicit confirm after showing the manifest', async () => {
    mockPreview.mockResolvedValue({
      status: 'confirm_required',
      requires_confirm: true,
      manifest: { messages_destroyed: 12, linked_children_severed: 2 },
    });
    mockConfirm.mockResolvedValue({
      status: 'purged',
      manifest: { messages_destroyed: 12, linked_children_severed: 2 },
    });
    renderPanel();
    await screen.findByText('Active session');

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /purge/i }));

    // Step one: the manifest dry run runs and destroys nothing yet.
    await waitFor(() => expect(mockPreview).toHaveBeenCalledWith('a1'));
    expect(await screen.findByText(/permanently destroy/i)).toBeInTheDocument();
    expect(screen.getByText('12')).toBeInTheDocument();
    expect(mockConfirm).not.toHaveBeenCalled();

    // Step two: only the explicit confirm fires the irreversible expunge.
    await user.click(screen.getByRole('button', { name: /purge permanently/i }));
    await waitFor(() => expect(mockConfirm).toHaveBeenCalledWith('a1'));
  });

  it('the archived list restore drives restore-archive', async () => {
    mockList.mockResolvedValue([
      {
        id: 'ar1',
        started_at: '2026-05-01T10:00:00Z',
        title: 'Archived session',
        message_count: 8,
        creator_principal: 'user:bob',
        archived_at: '2026-06-01T10:00:00Z',
        type: 'default',
      },
    ]);
    mockRestoreArchive.mockResolvedValue();
    renderPanel();

    const user = userEvent.setup();
    await user.click(screen.getByRole('tab', { name: 'Archived' }));

    expect(await screen.findByText('Archived session')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /restore/i }));
    await waitFor(() => expect(mockRestoreArchive).toHaveBeenCalledWith('ar1'));
  });

  it('names the captain distinctly from row creators when an incident is active', async () => {
    mockUseRegime.mockReturnValue({
      data: {
        mode: 'incident',
        declaredByPrincipal: 'user:carol',
        declaredAt: null,
        declaredKind: null,
      },
    } as ReturnType<typeof useRegime>);
    renderPanel();

    // The row creator (alice) is a separate principal from the captain (carol).
    expect(await screen.findByText('alice@example.com')).toBeInTheDocument();
    const card = screen.getByText(/active incident — captain/i);
    expect(card).toHaveTextContent('carol');
    expect(card).not.toHaveTextContent('alice');
  });
});
