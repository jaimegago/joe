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
    expect(screen.getByText('Admin')).toBeInTheDocument();
    expect(screen.getByText('LLM Settings')).toBeInTheDocument();
    // The persistent admin indicator shows whenever is_admin is true.
    expect(screen.getByText('ADMIN')).toBeInTheDocument();
    // Non-admin entries always render.
    expect(screen.getByText('Chat')).toBeInTheDocument();
  });

  it('hides admin-only entries and the ADMIN badge when the caller is not an admin', () => {
    mockUseCurrentUser.mockReturnValue({ data: { is_admin: false } } as ReturnType<typeof useCurrentUser>);
    renderSidebar();
    expect(screen.queryByText('Admin')).not.toBeInTheDocument();
    expect(screen.queryByText('LLM Settings')).not.toBeInTheDocument();
    expect(screen.queryByText('ADMIN')).not.toBeInTheDocument();
    expect(screen.getByText('Chat')).toBeInTheDocument();
  });

  it('does not flash admin entries or the ADMIN badge before the current-user query resolves', () => {
    mockUseCurrentUser.mockReturnValue({ data: undefined } as ReturnType<typeof useCurrentUser>);
    renderSidebar();
    expect(screen.queryByText('Admin')).not.toBeInTheDocument();
    expect(screen.queryByText('LLM Settings')).not.toBeInTheDocument();
    expect(screen.queryByText('ADMIN')).not.toBeInTheDocument();
  });
});
