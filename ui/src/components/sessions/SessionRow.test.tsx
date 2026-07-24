import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { SessionRow, type SessionRowActions } from './SessionRow';
import type { Session } from '@/api/types';

// SessionRow gates its owner-only controls (rename / move-to-trash) on the
// POSITIVE ownership signal read_only === false, and MUST fail closed — an
// absent read_only means "not owner", so a defaulted/legacy row never exposes
// mutating controls to a non-owner. These tests pin that boundary.

const noopActions: SessionRowActions = {
  editingId: null,
  draftTitle: '',
  setDraftTitle: vi.fn(),
  onStartEdit: vi.fn(),
  onCommitEdit: vi.fn(),
  onCancelEdit: vi.fn(),
  onDelete: vi.fn(),
  onPromote: vi.fn(),
  renamePending: false,
  promotePending: false,
};

function baseSession(over: Partial<Session>): Session {
  return {
    id: 's1',
    started_at: '2026-06-06T10:00:00Z',
    message_count: 2,
    title: 'A session',
    type: 'default',
    ...over,
  };
}

function renderRow(session: Session) {
  return render(
    <MemoryRouter>
      <SessionRow {...noopActions} session={session} />
    </MemoryRouter>
  );
}

describe('SessionRow ownership gating', () => {
  it('shows owner controls only when read_only === false', () => {
    renderRow(baseSession({ read_only: false }));
    expect(screen.getByLabelText('Rename session')).toBeInTheDocument();
    expect(screen.getByLabelText('Delete session')).toBeInTheDocument();
    expect(screen.queryByText('Read-only')).not.toBeInTheDocument();
  });

  it('treats read_only === true as a non-owner reader (no controls, read-only badge)', () => {
    renderRow(baseSession({ read_only: true, shared_by: 'user:alice@example.com' }));
    expect(screen.queryByLabelText('Rename session')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Delete session')).not.toBeInTheDocument();
    expect(screen.getByText('Read-only')).toBeInTheDocument();
  });

  it('fails CLOSED when read_only is absent: no owner controls, read-only badge shown', () => {
    // The pre-fix bug used `read_only !== true`, which treated an absent flag as
    // OWNER (fail-open). The fix gates on read_only === false, so an absent flag
    // is a non-owner.
    renderRow(baseSession({ read_only: undefined }));
    expect(screen.queryByLabelText('Rename session')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Delete session')).not.toBeInTheDocument();
    expect(screen.getByText('Read-only')).toBeInTheDocument();
  });
});
