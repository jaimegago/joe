import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { useReadPosture } from '@/hooks/useReadPosture';
import { READ_POSTURE } from '@/api/security';
import { AuthProvider } from '@/auth/AuthContext';
import { createWrapper } from '@/test/utils';

vi.mock('@/hooks/useCurrentUser', () => ({ useCurrentUser: vi.fn() }));
const mockUseCurrentUser = vi.mocked(useCurrentUser);

// The Policies nav entry is gated on the install read posture (read-posture-latch):
// it renders only when the posture is `zoned`. The Sidebar reads it via
// useReadPosture, mocked here so each test drives the posture directly.
vi.mock('@/hooks/useReadPosture', () => ({ useReadPosture: vi.fn() }));
const mockUseReadPosture = vi.mocked(useReadPosture);

function setPosture(posture: string | undefined) {
  mockUseReadPosture.mockReturnValue({
    data: posture,
    isLoading: posture === undefined,
  } as ReturnType<typeof useReadPosture>);
}

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
  beforeEach(() => {
    mockUseCurrentUser.mockReset();
    mockUseReadPosture.mockReset();
    // Default to the zoned posture so the full admin nav (including Policies)
    // renders; posture-specific tests override this.
    setPosture(READ_POSTURE.zoned);
  });

  it('renders admin-only entries and the ADMIN badge when the caller is an admin', () => {
    mockUseCurrentUser.mockReturnValue({ data: { is_admin: true } } as ReturnType<
      typeof useCurrentUser
    >);
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
    // Credentials is an admin-only Admin-subgroup child (session
    // admin-nav-consolidation-01): it renders for admins, and structurally it
    // shares the subgroup container with the other admin children (e.g. Zones)
    // rather than sitting in the top-level operator nav.
    expect(screen.getByText('Credentials')).toBeInTheDocument();
    const credentialsParent = screen.getByText('Credentials').closest('a')?.parentElement;
    const zonesParent = screen.getByText('Zones').closest('a')?.parentElement;
    expect(credentialsParent).toBe(zonesParent);
    const chatParent = screen.getByText('Chat').closest('a')?.parentElement;
    expect(credentialsParent).not.toBe(chatParent);
    // The persistent admin indicator shows whenever is_admin is true.
    expect(screen.getByText('ADMIN')).toBeInTheDocument();
    // Operator entries always render; Components and Sessions appear exactly
    // once each (operator entries with inline admin actions, never duplicated
    // under the Admin subgroup).
    expect(screen.getByText('Chat')).toBeInTheDocument();
    expect(screen.getAllByText('Components')).toHaveLength(1);
    expect(screen.getAllByText('Sessions')).toHaveLength(1);
  });

  it('hides the Policies nav entry under the team_flat posture but keeps Zones', () => {
    mockUseCurrentUser.mockReturnValue({ data: { is_admin: true } } as ReturnType<
      typeof useCurrentUser
    >);
    setPosture(READ_POSTURE.teamFlat);
    renderSidebar();
    // Policies is inert under team_flat, so it is hidden.
    expect(screen.queryByText('Policies')).not.toBeInTheDocument();
    // Zones and component-zone assignment retain live read-shaping function under
    // team_flat (zone.Allows gates ahead of the team_flat admit), so Zones stays.
    expect(screen.getByText('Zones')).toBeInTheDocument();
    // The rest of the admin subgroup is unaffected.
    expect(screen.getByText('Autonomous Reads')).toBeInTheDocument();
    expect(screen.getByText('Admins')).toBeInTheDocument();
  });

  it('shows the Policies nav entry under the zoned posture', () => {
    mockUseCurrentUser.mockReturnValue({ data: { is_admin: true } } as ReturnType<
      typeof useCurrentUser
    >);
    setPosture(READ_POSTURE.zoned);
    renderSidebar();
    expect(screen.getByText('Policies')).toBeInTheDocument();
    expect(screen.getByText('Zones')).toBeInTheDocument();
  });

  it('does not flicker the Policies nav entry in before the posture resolves', () => {
    mockUseCurrentUser.mockReturnValue({ data: { is_admin: true } } as ReturnType<
      typeof useCurrentUser
    >);
    setPosture(undefined);
    renderSidebar();
    // Posture unresolved: Policies stays hidden rather than flashing in.
    expect(screen.queryByText('Policies')).not.toBeInTheDocument();
    // Other admin entries still render — only Policies is posture-gated.
    expect(screen.getByText('Zones')).toBeInTheDocument();
  });

  it('hides admin-only entries and the ADMIN badge when the caller is not an admin', () => {
    mockUseCurrentUser.mockReturnValue({ data: { is_admin: false } } as ReturnType<
      typeof useCurrentUser
    >);
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
