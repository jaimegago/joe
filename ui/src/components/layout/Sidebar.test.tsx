import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { useCurrentUser } from '@/hooks/useCurrentUser';

vi.mock('@/hooks/useCurrentUser', () => ({ useCurrentUser: vi.fn() }));
const mockUseCurrentUser = vi.mocked(useCurrentUser);

function renderSidebar() {
  render(
    <MemoryRouter>
      <Sidebar />
    </MemoryRouter>
  );
}

describe('Sidebar', () => {
  beforeEach(() => mockUseCurrentUser.mockReset());

  it('renders admin-only entries when the caller is an admin', () => {
    mockUseCurrentUser.mockReturnValue({ data: { is_admin: true } } as ReturnType<typeof useCurrentUser>);
    renderSidebar();
    expect(screen.getByText('Admin')).toBeInTheDocument();
    expect(screen.getByText('LLM Settings')).toBeInTheDocument();
    // Non-admin entries always render.
    expect(screen.getByText('Dashboard')).toBeInTheDocument();
  });

  it('hides admin-only entries when the caller is not an admin', () => {
    mockUseCurrentUser.mockReturnValue({ data: { is_admin: false } } as ReturnType<typeof useCurrentUser>);
    renderSidebar();
    expect(screen.queryByText('Admin')).not.toBeInTheDocument();
    expect(screen.queryByText('LLM Settings')).not.toBeInTheDocument();
    expect(screen.getByText('Dashboard')).toBeInTheDocument();
  });

  it('does not flash admin entries before the current-user query resolves', () => {
    mockUseCurrentUser.mockReturnValue({ data: undefined } as ReturnType<typeof useCurrentUser>);
    renderSidebar();
    expect(screen.queryByText('Admin')).not.toBeInTheDocument();
    expect(screen.queryByText('LLM Settings')).not.toBeInTheDocument();
  });
});
