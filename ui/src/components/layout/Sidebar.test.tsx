import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { AuthProvider } from '@/auth/AuthContext';
import { createWrapper } from '@/test/utils';

vi.mock('@/hooks/useCurrentUser', () => ({ useCurrentUser: vi.fn() }));
const mockUseCurrentUser = vi.mocked(useCurrentUser);

// Sidebar consumes both useCurrentUser (admin-link visibility) and the auth
// context (logout control), so it renders inside an AuthProvider. AuthProvider
// reads the same mocked useCurrentUser, so a single mock drives both.
function renderSidebar() {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <AuthProvider>
        <MemoryRouter>
          <Sidebar />
        </MemoryRouter>
      </AuthProvider>
    </Wrapper>
  );
}

describe('Sidebar', () => {
  beforeEach(() => mockUseCurrentUser.mockReset());

  it('renders admin-only entries and the ADMIN badge when the caller is an admin', () => {
    mockUseCurrentUser.mockReturnValue({ data: { is_admin: true } } as ReturnType<typeof useCurrentUser>);
    renderSidebar();
    // The Admin subgroup header and its children render for admins.
    expect(screen.getByText('Admin')).toBeInTheDocument();
    expect(screen.getByText('Zones')).toBeInTheDocument();
    expect(screen.getByText('Policies')).toBeInTheDocument();
    expect(screen.getByText('Autonomous Reads')).toBeInTheDocument();
    expect(screen.getByText('Skills')).toBeInTheDocument();
    expect(screen.getByText('Admins')).toBeInTheDocument();
    expect(screen.getByText('Users')).toBeInTheDocument();
    expect(screen.getByText('LLM Settings')).toBeInTheDocument();
    // Credentials is a top-level admin-gated entry, not an Admin-subgroup child.
    expect(screen.getByText('Credentials')).toBeInTheDocument();
    // The persistent admin indicator shows whenever is_admin is true.
    expect(screen.getByText('ADMIN')).toBeInTheDocument();
    // Operator entries always render; Components and Sessions appear exactly
    // once each (operator entries with inline admin actions, never duplicated
    // under the Admin subgroup).
    expect(screen.getByText('Chat')).toBeInTheDocument();
    expect(screen.getAllByText('Components')).toHaveLength(1);
    expect(screen.getAllByText('Sessions')).toHaveLength(1);
  });

  it('hides admin-only entries and the ADMIN badge when the caller is not an admin', () => {
    mockUseCurrentUser.mockReturnValue({ data: { is_admin: false } } as ReturnType<typeof useCurrentUser>);
    renderSidebar();
    expect(screen.queryByText('Admin')).not.toBeInTheDocument();
    expect(screen.queryByText('LLM Settings')).not.toBeInTheDocument();
    expect(screen.queryByText('Zones')).not.toBeInTheDocument();
    // Credentials is admin-gated and hidden from non-admins.
    expect(screen.queryByText('Credentials')).not.toBeInTheDocument();
    expect(screen.queryByText('ADMIN')).not.toBeInTheDocument();
    // Operator views remain intact for non-admins.
    expect(screen.getByText('Chat')).toBeInTheDocument();
    expect(screen.getByText('Sessions')).toBeInTheDocument();
    expect(screen.getByText('Components')).toBeInTheDocument();
  });

  it('does not flash admin entries or the ADMIN badge before the current-user query resolves', () => {
    mockUseCurrentUser.mockReturnValue({ data: undefined } as ReturnType<typeof useCurrentUser>);
    renderSidebar();
    expect(screen.queryByText('Admin')).not.toBeInTheDocument();
    expect(screen.queryByText('LLM Settings')).not.toBeInTheDocument();
    expect(screen.queryByText('ADMIN')).not.toBeInTheDocument();
  });
});
