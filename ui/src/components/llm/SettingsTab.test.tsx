import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createWrapper } from '@/test/utils';
import { SettingsTab } from './SettingsTab';
import { setCostLimit } from '@/api/llm';
import type { LLMCostLimit, LLMRunawayCeiling, LLMProvider } from '@/api/types';

vi.mock('@/api/llm', () => ({
  setActiveModel: vi.fn(),
  setCostLimit: vi.fn(),
  setRunawayCeiling: vi.fn(),
}));
const mockSetCostLimit = vi.mocked(setCostLimit);

const ceiling: LLMRunawayCeiling = { stored_raw: 0, state: 'backstop_fallback', effective: 200000 };
const models: LLMProvider[] = [
  { name: 'claude', provider: 'anthropic', model: 'claude-x', configured: true, key_present: true },
];

function renderTab(costLimits: LLMCostLimit[]) {
  const { qc, Wrapper } = createWrapper();
  render(
    <Wrapper>
      <SettingsTab
        activeModel="claude"
        costLimits={costLimits}
        runawayCeiling={ceiling}
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
