import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createWrapper } from '@/test/utils';
import { SettingsTab } from './SettingsTab';
import { setCostLimit, setContextBudget } from '@/api/llm';
import type { LLMCostLimit, LLMRunawayCeiling, LLMContextBudget, LLMProvider } from '@/api/types';

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));
vi.mock('@/api/llm', () => ({
  setActiveModel: vi.fn(),
  setCostLimit: vi.fn(),
  setRunawayCeiling: vi.fn(),
  setContextBudget: vi.fn(),
}));
const mockSetCostLimit = vi.mocked(setCostLimit);
const mockSetContextBudget = vi.mocked(setContextBudget);

const ceiling: LLMRunawayCeiling = { stored_raw: 0, state: 'backstop_fallback', effective: 200000 };
const contextBudget: LLMContextBudget = { stored_raw: 0, state: 'backstop_fallback', effective: 0.7 };
const models: LLMProvider[] = [
  { name: 'claude', provider: 'anthropic', model: 'claude-x', configured: true, key_present: true },
];

function renderTab(costLimits: LLMCostLimit[], budget: LLMContextBudget = contextBudget) {
  const { qc, Wrapper } = createWrapper();
  render(
    <Wrapper>
      <SettingsTab
        activeModel="claude"
        costLimits={costLimits}
        runawayCeiling={ceiling}
        contextBudget={budget}
        models={models}
      />
    </Wrapper>
  );
  return qc;
}

describe('SettingsTab limit labelling', () => {
  beforeEach(() => mockSetCostLimit.mockReset());

  it('labels a backstop-fallback limit as default and shows the effective backstop value', () => {
    renderTab([{ window: 'hourly', stored_raw: 0, state: 'backstop_fallback', effective: 100_000_000_000 }]);
    const card = within(screen.getByTestId('limit-hourly'));
    expect(card.getByText('Default (backstop)')).toBeInTheDocument();
    // 100e9 nano = 100.00 currency units — the substituted backstop, not zero.
    expect(card.getByText('100.00 nano')).toBeInTheDocument();
  });

  it('labels a configured positive limit as operator-set', () => {
    renderTab([{ window: 'daily', stored_raw: 50_000_000_000, state: 'configured', effective: 50_000_000_000 }]);
    const card = within(screen.getByTestId('limit-daily'));
    expect(card.getByText('Operator-set limit')).toBeInTheDocument();
    expect(card.getByText('50.00 nano')).toBeInTheDocument();
  });

  it('labels a configured-zero (explicit-disable) limit as disabled', () => {
    renderTab([{ window: 'monthly', stored_raw: -1, state: 'configured', effective: 0 }]);
    const card = within(screen.getByTestId('limit-monthly'));
    expect(card.getByText('Disabled')).toBeInTheDocument();
    expect(card.getByText('No limit in force')).toBeInTheDocument();
  });
});

describe('SettingsTab cost-limit write', () => {
  beforeEach(() => mockSetCostLimit.mockReset());

  it('calls setCostLimit with the window and value and invalidates the settings query', async () => {
    mockSetCostLimit.mockResolvedValue({ window: 'hourly', value: 2_000_000_000 });
    const qc = renderTab([{ window: 'hourly', stored_raw: 0, state: 'backstop_fallback', effective: 100_000_000_000 }]);
    const invalidate = vi.spyOn(qc, 'invalidateQueries');

    const input = screen.getByLabelText(/Set limit/);
    await userEvent.type(input, '2000000000');
    const form = input.closest('form')!;
    await userEvent.click(within(form).getByRole('button', { name: 'Set' }));

    expect(mockSetCostLimit).toHaveBeenCalledWith('hourly', 2_000_000_000);
    await waitFor(() =>
      expect(invalidate).toHaveBeenCalledWith({ queryKey: ['llm-settings'] })
    );
  });

  it('disables the submit button while the write is pending', async () => {
    let resolve!: (v: { window: string; value: number }) => void;
    mockSetCostLimit.mockReturnValue(new Promise((r) => (resolve = r)));
    renderTab([{ window: 'hourly', stored_raw: 0, state: 'backstop_fallback', effective: 100_000_000_000 }]);

    const input = screen.getByLabelText(/Set limit/);
    await userEvent.type(input, '2000000000');
    const form = input.closest('form')!;
    const button = within(form).getByRole('button', { name: 'Set' });
    await userEvent.click(button);

    await waitFor(() => expect(button).toBeDisabled());
    resolve({ window: 'hourly', value: 2_000_000_000 });
  });
});

describe('SettingsTab context-budget control', () => {
  beforeEach(() => mockSetContextBudget.mockReset());

  it('renders the current effective fraction and the backstop state', () => {
    renderTab([]);
    const card = within(screen.getByTestId('limit-context-budget'));
    // 0.7 backstop default, formatted to two decimals.
    expect(card.getByText('0.70')).toBeInTheDocument();
    expect(card.getByText('Default (backstop)')).toBeInTheDocument();
  });

  it('submits a valid new fraction and invalidates the settings query', async () => {
    mockSetContextBudget.mockResolvedValue({ fraction: 0.5 });
    const qc = renderTab([]);
    const invalidate = vi.spyOn(qc, 'invalidateQueries');

    const input = screen.getByLabelText(/Set fraction/);
    await userEvent.type(input, '0.5');
    const form = input.closest('form')!;
    await userEvent.click(within(form).getByRole('button', { name: 'Set' }));

    expect(mockSetContextBudget).toHaveBeenCalledWith(0.5);
    await waitFor(() =>
      expect(invalidate).toHaveBeenCalledWith({ queryKey: ['llm-settings'] })
    );
  });

  it('rejects an out-of-range fraction client-side without calling the API', async () => {
    renderTab([]);
    const input = screen.getByLabelText(/Set fraction/);
    const form = input.closest('form')!;

    // Above the inclusive 1.0 upper bound.
    await userEvent.type(input, '1.5');
    await userEvent.click(within(form).getByRole('button', { name: 'Set' }));
    expect(within(form).getByText(/at most 1\.0/)).toBeInTheDocument();
    expect(mockSetContextBudget).not.toHaveBeenCalled();
  });

  it('disables the input while the write is pending', async () => {
    let resolve!: (v: { fraction: number }) => void;
    mockSetContextBudget.mockReturnValue(new Promise((r) => (resolve = r)));
    renderTab([]);

    const input = screen.getByLabelText(/Set fraction/);
    await userEvent.type(input, '0.5');
    const form = input.closest('form')!;
    await userEvent.click(within(form).getByRole('button', { name: 'Set' }));

    await waitFor(() => expect(input).toBeDisabled());
    // Settle the mutation within this test so its onSuccess state updates do
    // not leak into the next test (which would fire outside act and confuse
    // vitest's per-test error attribution).
    resolve({ fraction: 0.5 });
    await waitFor(() => expect(input).not.toBeDisabled());
  });

});

// The backend-rejection test lives in its own describe WITHOUT a beforeEach.
// A describe-level beforeEach changes vitest's per-test async tracking such
// that react-query's (already-handled) mutation rejection is mis-attributed to
// the test as a failure; an inline mock reset sidesteps that while keeping the
// reset semantics. See the other context-budget tests for the common cases.
describe('SettingsTab context-budget backend error', () => {
  it('surfaces a backend 400 rejection inline', async () => {
    mockSetContextBudget.mockReset();
    // Reject only after react-query has subscribed (a pending promise we
    // control). The passive .catch marks the promise handled for the
    // unhandled-rejection detector; react-query still routes the error to
    // onError, which renders the backend 400 message inline.
    let reject!: (e: Error) => void;
    const pending = new Promise<{ fraction: number }>((_, r) => (reject = r));
    pending.catch(() => undefined);
    mockSetContextBudget.mockReturnValue(pending);
    renderTab([]);

    const input = screen.getByLabelText(/Set fraction/);
    await userEvent.type(input, '0.5');
    const form = input.closest('form')!;
    await userEvent.click(within(form).getByRole('button', { name: 'Set' }));

    reject(new Error('fraction must be greater than 0 and at most 1.0'));
    // Passively drain a macrotask tick so the rejection settles into onError
    // before asserting synchronously (polling matchers would race it). The
    // substring keeps the assertion off the full message.
    await new Promise((r) => setTimeout(r, 0));
    expect(within(form).getByText(/at most 1\.0/)).toBeInTheDocument();
  });
});
