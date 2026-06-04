import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { RequireAdmin } from './RequireAdmin';
import { useAuth } from '@/auth/AuthContext';
import type { AuthContextValue } from '@/auth/AuthContext';

// The route-level admin gate (RequireAdmin) is the authoritative gate for
// /llm-settings and /admin (App.tsx:43-44). UsageTab.test.tsx covers the
// in-page per-principal section gating; this test pins the route wrapper
// itself — an admin lands on the page, a non-admin is redirected to "/".
vi.mock('@/auth/AuthContext', () => ({
  useAuth: vi.fn(),
}));
const mockUseAuth = vi.mocked(useAuth);

function setAdmin(isAdmin: boolean) {
  // Only isAdmin is read by RequireAdmin; the rest of the context is filled
  // with inert defaults so the cast is honest about what the gate consumes.
  mockUseAuth.mockReturnValue({
    status: 'authed',
    principal: 'user:alice',
    isAdmin,
    rbacEnabled: true,
    zones: [],
    oidcEnabled: false,
    login: vi.fn(),
    loginWithOIDC: vi.fn(),
    logout: vi.fn(),
  } satisfies AuthContextValue);
}

// Renders the same shape App.tsx uses: a RequireAdmin-wrapped page at
// /llm-settings, with an index route standing in for the redirect target.
function renderAt(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route index element={<div>Home (index)</div>} />
        <Route
          path="llm-settings"
          element={
            <RequireAdmin>
              <div>LLM Settings page</div>
            </RequireAdmin>
          }
        />
      </Routes>
    </MemoryRouter>
  );
}

describe('RequireAdmin route gate', () => {
  beforeEach(() => {
    mockUseAuth.mockReset();
  });

  it('renders the page for an admin navigating to /llm-settings', () => {
    setAdmin(true);
    renderAt('/llm-settings');
    expect(screen.getByText('LLM Settings page')).toBeInTheDocument();
    expect(screen.queryByText('Home (index)')).not.toBeInTheDocument();
  });

  it('redirects a non-admin away from /llm-settings to the index route', () => {
    setAdmin(false);
    renderAt('/llm-settings');
    expect(screen.queryByText('LLM Settings page')).not.toBeInTheDocument();
    expect(screen.getByText('Home (index)')).toBeInTheDocument();
  });
});
