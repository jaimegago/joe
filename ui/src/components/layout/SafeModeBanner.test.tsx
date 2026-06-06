import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SafeModeBanner } from './SafeModeBanner';
import { usePanicStatus } from '@/hooks/usePanicStatus';

// SafeModeBanner renders only while safe mode is active and otherwise nothing,
// the panic-axis counterpart to IncidentBanner. Mock the polling hook to drive
// each state directly.
vi.mock('@/hooks/usePanicStatus', () => ({ usePanicStatus: vi.fn() }));
const mockUsePanicStatus = vi.mocked(usePanicStatus);

function setStatus(data: unknown) {
  // The component only reads `data`; the rest of the query result is unused.
  mockUsePanicStatus.mockReturnValue({ data } as ReturnType<typeof usePanicStatus>);
}

describe('SafeModeBanner', () => {
  beforeEach(() => mockUsePanicStatus.mockReset());

  it('renders the safe-mode banner with trigger detail and unlock path', () => {
    setStatus({
      safeMode: true,
      triggeredAt: '2026-06-04T12:00:00Z',
      triggerSource: 'cli',
      triggerReason: 'manual shutdown',
    });
    render(<SafeModeBanner />);
    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.getByText(/System is in safe mode/i)).toBeInTheDocument();
    expect(screen.getByText(/Only read-only operations are permitted/i)).toBeInTheDocument();
    expect(screen.getByText(/joe unlock/i)).toBeInTheDocument();
  });

  it('renders nothing when safe mode is inactive', () => {
    setStatus({ safeMode: false, triggeredAt: null, triggerSource: null, triggerReason: null });
    const { container } = render(<SafeModeBanner />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing before the status has loaded', () => {
    setStatus(undefined);
    const { container } = render(<SafeModeBanner />);
    expect(container).toBeEmptyDOMElement();
  });
});
