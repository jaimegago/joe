import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ObservationBanner } from './ObservationBanner';
import { useMutateStatus } from '@/hooks/useMutateStatus';

// ObservationBanner renders only while the mutate-status reason is
// "observation" and otherwise nothing. Critically it gates on reason, NOT on
// can_mutate (which is also false in safe mode), so it never co-renders with
// SafeModeBanner. Mock the status hook to drive each state directly.
vi.mock('@/hooks/useMutateStatus', () => ({ useMutateStatus: vi.fn() }));
const mockUseMutateStatus = vi.mocked(useMutateStatus);

function setStatus(data: unknown) {
  // The component only reads `data`; the rest of the query result is unused.
  mockUseMutateStatus.mockReturnValue({ data } as ReturnType<typeof useMutateStatus>);
}

describe('ObservationBanner', () => {
  beforeEach(() => mockUseMutateStatus.mockReset());

  it('renders the observation banner when reason is observation', () => {
    setStatus({ can_mutate: false, reason: 'observation' });
    render(<ObservationBanner />);
    expect(screen.getByRole('status')).toBeInTheDocument();
    expect(screen.getByText(/Joe is running in observation mode/i)).toBeInTheDocument();
    expect(screen.getByText(/will not make any changes/i)).toBeInTheDocument();
    // Observation is the intended posture, never a lock to clear — it must
    // never surface the safe-mode unlock wording.
    expect(screen.queryByText(/unlock/i)).not.toBeInTheDocument();
  });

  it('renders nothing in safe mode (can_mutate is false but reason is safe_mode)', () => {
    setStatus({ can_mutate: false, reason: 'safe_mode' });
    const { container } = render(<ObservationBanner />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing in full mode', () => {
    setStatus({ can_mutate: true, reason: 'full' });
    const { container } = render(<ObservationBanner />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing before the status has loaded', () => {
    setStatus(undefined);
    const { container } = render(<ObservationBanner />);
    expect(container).toBeEmptyDOMElement();
  });
});
