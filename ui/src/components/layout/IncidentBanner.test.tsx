import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { IncidentBanner } from './IncidentBanner';
import { useRegime } from '@/hooks/useRegime';

// IncidentBanner renders only while the regime is "incident" and otherwise
// nothing (OPERATOR_SURFACE_VERIFICATION.md item 8). Mock the polling hook to
// drive each state directly.
vi.mock('@/hooks/useRegime', () => ({ useRegime: vi.fn() }));
const mockUseRegime = vi.mocked(useRegime);

function setRegime(data: unknown) {
  // The component only reads `data`; the rest of the query result is unused.
  mockUseRegime.mockReturnValue({ data } as ReturnType<typeof useRegime>);
}

describe('IncidentBanner', () => {
  beforeEach(() => mockUseRegime.mockReset());

  it('renders the incident banner with declarer when mode is incident', () => {
    setRegime({
      mode: 'incident',
      declaredAt: '2026-06-04T12:00:00Z',
      declaredByPrincipal: 'user:alice',
      declaredKind: 'human',
    });
    render(<IncidentBanner />);
    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.getByText(/System is in incident mode/i)).toBeInTheDocument();
    expect(screen.getByText(/user:alice/)).toBeInTheDocument();
    expect(screen.getByText(/Writes may be blocked/i)).toBeInTheDocument();
  });

  it('renders nothing when mode is normal', () => {
    setRegime({ mode: 'normal', declaredAt: null, declaredByPrincipal: null, declaredKind: null });
    const { container } = render(<IncidentBanner />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing before the regime has loaded', () => {
    setRegime(undefined);
    const { container } = render(<IncidentBanner />);
    expect(container).toBeEmptyDOMElement();
  });
});
