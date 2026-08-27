import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createWrapper } from '@/test/utils';
import { ReadPostureControl } from './ReadPostureControl';
import { setReadPosture, READ_POSTURE } from '@/api/security';
import { QUERY_KEYS } from '@/lib/queryKeys';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('@/api/security', async (importOriginal) => {
  // READ_POSTURE and the ReadPosture type are the wire vocabulary, not behaviour —
  // keeping the real ones means a rename of a posture literal breaks this test
  // rather than passing against a stale mock.
  const actual = await importOriginal<typeof import('@/api/security')>();
  return { ...actual, setReadPosture: vi.fn() };
});

const mockSet = vi.mocked(setReadPosture);

function renderControl(
  posture: (typeof READ_POSTURE)[keyof typeof READ_POSTURE],
  grantCount: number | undefined
) {
  const { qc, Wrapper } = createWrapper();
  render(
    <Wrapper>
      <ReadPostureControl posture={posture} grantCount={grantCount} />
    </Wrapper>
  );
  return qc;
}

describe('ReadPostureControl', () => {
  beforeEach(() => {
    mockSet.mockReset();
  });

  it('shows the live posture and offers the opposite one', () => {
    renderControl(READ_POSTURE.teamFlat, 3);
    expect(screen.getByText('Team flat')).toBeInTheDocument();
    expect(screen.getByText(READ_POSTURE.teamFlat)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /switch to zoned/i })).toBeInTheDocument();
  });

  it('offers team flat when the install is already zoned', () => {
    renderControl(READ_POSTURE.zoned, 3);
    expect(screen.getByText('Zoned')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /switch to team flat/i })).toBeInTheDocument();
  });

  it('does not flip on the button alone — it asks first', async () => {
    const user = userEvent.setup();
    renderControl(READ_POSTURE.teamFlat, 3);
    await user.click(screen.getByRole('button', { name: /switch to zoned/i }));
    expect(mockSet).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('states that admins keep reading everything when switching to zoned', async () => {
    const user = userEvent.setup();
    renderControl(READ_POSTURE.teamFlat, 3);
    await user.click(screen.getByRole('button', { name: /switch to zoned/i }));
    const dialog = screen.getByRole('dialog');
    expect(dialog).toHaveTextContent(/admins are unaffected/i);
    expect(dialog).toHaveTextContent(/3 grants are configured/i);
  });

  it('warns that no non-admin will read anything when zoned is chosen with no grants', async () => {
    const user = userEvent.setup();
    renderControl(READ_POSTURE.teamFlat, 0);
    await user.click(screen.getByRole('button', { name: /switch to zoned/i }));
    expect(screen.getByRole('dialog')).toHaveTextContent(
      /no grants are configured.*no non-admin principal will be able to read any component/i
    );
  });

  it('says the grant count is unknown rather than implying zero when it could not be read', async () => {
    const user = userEvent.setup();
    renderControl(READ_POSTURE.teamFlat, undefined);
    await user.click(screen.getByRole('button', { name: /switch to zoned/i }));
    const dialog = screen.getByRole('dialog');
    expect(dialog).toHaveTextContent(/number of configured grants could not be read/i);
    expect(dialog).not.toHaveTextContent(/no grants are configured/i);
  });

  it('names service accounts among the principals team flat admits', async () => {
    const user = userEvent.setup();
    renderControl(READ_POSTURE.zoned, 3);
    await user.click(screen.getByRole('button', { name: /switch to team flat/i }));
    const dialog = screen.getByRole('dialog');
    expect(dialog).toHaveTextContent(/widens read access/i);
    expect(dialog).toHaveTextContent(/service-account API keys/i);
    // Grants survive the flip — the copy must not suggest the flip deletes them.
    expect(dialog).toHaveTextContent(/kept and are not deleted/i);
  });

  it('flips the posture once confirmed', async () => {
    const user = userEvent.setup();
    mockSet.mockResolvedValue(READ_POSTURE.zoned);
    renderControl(READ_POSTURE.teamFlat, 3);
    await user.click(screen.getByRole('button', { name: /switch to zoned/i }));
    const dialog = screen.getByRole('dialog');
    await user.click(within_dialog(dialog));
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith(READ_POSTURE.zoned));
  });

  it('publishes the applied posture to the shared query key so the Policies surface follows', async () => {
    const user = userEvent.setup();
    mockSet.mockResolvedValue(READ_POSTURE.zoned);
    const qc = renderControl(READ_POSTURE.teamFlat, 3);
    await user.click(screen.getByRole('button', { name: /switch to zoned/i }));
    await user.click(within_dialog(screen.getByRole('dialog')));
    // The Sidebar nav entry and the /admin/policies route guard both read this
    // key; without the write they keep showing the pre-flip posture until a reload.
    await waitFor(() => expect(qc.getQueryData(QUERY_KEYS.readPosture)).toBe(READ_POSTURE.zoned));
  });

  it('does not flip when the confirmation is cancelled', async () => {
    const user = userEvent.setup();
    renderControl(READ_POSTURE.teamFlat, 3);
    await user.click(screen.getByRole('button', { name: /switch to zoned/i }));
    await user.click(screen.getByRole('button', { name: /cancel/i }));
    expect(mockSet).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });
});

// within_dialog returns the dialog's own confirm button. Both the card and the
// dialog carry a "Switch to …" button, so a bare getByRole would be ambiguous.
function within_dialog(dialog: HTMLElement): HTMLElement {
  const buttons = Array.from(dialog.querySelectorAll('button')).filter((b) =>
    /^switch to /i.test(b.textContent ?? '')
  );
  if (buttons.length !== 1) {
    throw new Error(`expected one confirm button in the dialog, found ${buttons.length}`);
  }
  return buttons[0];
}
