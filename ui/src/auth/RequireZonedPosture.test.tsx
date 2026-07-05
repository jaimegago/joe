import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { RequireZonedPosture } from './RequireZonedPosture';
import { useReadPosture } from '@/hooks/useReadPosture';
import { READ_POSTURE } from '@/api/security';

// The route-level posture gate (RequireZonedPosture) hides the grant-based
// Policies page under the team_flat launch posture (read-posture-latch). It sits
// INSIDE RequireAdmin in App.tsx, so this test drives the posture directly; the
// admin gate is covered by RequireAdmin.test.tsx and is not weakened here.
vi.mock('@/hooks/useReadPosture', () => ({ useReadPosture: vi.fn() }));
const mockUseReadPosture = vi.mocked(useReadPosture);

function setPosture(posture: string | undefined, isLoading = false) {
  mockUseReadPosture.mockReturnValue({
    data: posture,
    isLoading,
  } as ReturnType<typeof useReadPosture>);
}

// Mirrors the shape App.tsx uses: a RequireZonedPosture-wrapped Policies page,
// with an index route standing in for the redirect target.
function renderAt(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route index element={<div>Home (index)</div>} />
        <Route
          path="admin/policies"
          element={
            <RequireZonedPosture>
              <div>Policies page</div>
            </RequireZonedPosture>
          }
        />
      </Routes>
    </MemoryRouter>
  );
}

describe('RequireZonedPosture route gate', () => {
  beforeEach(() => {
    mockUseReadPosture.mockReset();
  });

  it('renders the Policies page under the zoned posture', () => {
    setPosture(READ_POSTURE.zoned);
    renderAt('/admin/policies');
    expect(screen.getByText('Policies page')).toBeInTheDocument();
    expect(screen.queryByText('Home (index)')).not.toBeInTheDocument();
  });

  it('redirects away from the Policies route under the team_flat posture', () => {
    setPosture(READ_POSTURE.teamFlat);
    renderAt('/admin/policies');
    expect(screen.queryByText('Policies page')).not.toBeInTheDocument();
    expect(screen.getByText('Home (index)')).toBeInTheDocument();
  });

  it('shows the loading page (no redirect) while the posture is resolving', () => {
    setPosture(undefined, true);
    renderAt('/admin/policies');
    // Neither the page nor the redirect target renders — the loading state holds
    // so a zoned-install admin hard-reloading the route is not bounced.
    expect(screen.queryByText('Policies page')).not.toBeInTheDocument();
    expect(screen.queryByText('Home (index)')).not.toBeInTheDocument();
  });
});
