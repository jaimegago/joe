import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createWrapper } from '@/test/utils';
import { RetentionPolicyEditor } from './RetentionPolicyEditor';
import { fetchRetentionPolicy, updateRetentionPolicy } from '@/api/adminSessions';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('@/api/adminSessions', () => ({
  fetchRetentionPolicy: vi.fn(),
  updateRetentionPolicy: vi.fn(),
}));

const mockGet = vi.mocked(fetchRetentionPolicy);
const mockPut = vi.mocked(updateRetentionPolicy);

function renderEditor() {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <RetentionPolicyEditor />
    </Wrapper>
  );
}

describe('RetentionPolicyEditor', () => {
  beforeEach(() => {
    mockGet.mockReset();
    mockPut.mockReset();
  });

  it('seeds the three knobs from the policy and round-trips a save', async () => {
    mockGet.mockResolvedValue({
      inactivity_days: null, // OFF (the default)
      inactivity_window: 'off',
      trash_grace_days: 30,
      terminal_action: 'trash_then_purge',
    });
    mockPut.mockResolvedValue({
      inactivity_days: 14,
      inactivity_window: '14d',
      trash_grace_days: 45,
      terminal_action: 'archive',
    });
    renderEditor();

    // Inactivity defaults to OFF.
    const offBox = await screen.findByLabelText(/off \(nothing auto-expires\)/i);
    expect(offBox).toBeChecked();

    const user = userEvent.setup();
    // Turn inactivity on and set 14 days.
    await user.click(offBox);
    const inactivity = screen.getByLabelText(/inactivity window in days/i);
    await user.clear(inactivity);
    await user.type(inactivity, '14');

    // Bump trash-grace to 45.
    const grace = screen.getByLabelText(/trash grace in days/i);
    await user.clear(grace);
    await user.type(grace, '45');

    // Switch the terminal action to archive — which surfaces the archive-directory
    // dependency note (§12.6).
    await user.click(screen.getByLabelText(/terminal action/i));
    await user.click(await screen.findByRole('option', { name: /archive/i }));
    expect(screen.getByText(/requires a configured archive directory/i)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /save policy/i }));
    await waitFor(() =>
      expect(mockPut).toHaveBeenCalledWith({
        inactivity_days: 14,
        trash_grace_days: 45,
        terminal_action: 'archive',
      })
    );
  });

  it('saves inactivity OFF as null', async () => {
    mockGet.mockResolvedValue({
      inactivity_days: 7,
      inactivity_window: '7d',
      trash_grace_days: 30,
      terminal_action: 'trash_then_purge',
    });
    mockPut.mockResolvedValue({
      inactivity_days: null,
      inactivity_window: 'off',
      trash_grace_days: 30,
      terminal_action: 'trash_then_purge',
    });
    renderEditor();

    // Seeded from a 7-day window, so the OFF box starts unchecked; tick it.
    const offBox = await screen.findByLabelText(/off \(nothing auto-expires\)/i);
    await waitFor(() => expect(offBox).not.toBeChecked());

    const user = userEvent.setup();
    await user.click(offBox);
    await user.click(screen.getByRole('button', { name: /save policy/i }));

    await waitFor(() =>
      expect(mockPut).toHaveBeenCalledWith({
        inactivity_days: null,
        trash_grace_days: 30,
        terminal_action: 'trash_then_purge',
      })
    );
  });
});
