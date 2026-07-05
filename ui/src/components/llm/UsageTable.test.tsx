import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { UsageTable } from './UsageTable';
import type { UsageBreakdown } from '@/api/types';

function row(currency: string, costNano: number, model: string): UsageBreakdown {
  return {
    calls: 1,
    input_tokens: 10,
    output_tokens: 5,
    estimated_cost_nano: costNano,
    currency,
    model,
  };
}

describe('UsageTable currency grouping', () => {
  it('renders one currency group with a subtotal at launch (single currency)', () => {
    render(
      <UsageTable
        rows={[row('USD', 1_000_000_000, 'a'), row('USD', 2_000_000_000, 'b')]}
        dimension="model"
      />
    );
    expect(screen.getByTestId('currency-group-USD')).toBeInTheDocument();
    expect(screen.getByTestId('subtotal-USD')).toHaveTextContent('3.00 USD');
  });

  it('renders two currency groups with separate subtotals and no cross-currency total', () => {
    render(
      <UsageTable
        rows={[
          row('USD', 1_000_000_000, 'a'),
          row('USD', 2_000_000_000, 'b'),
          row('EUR', 5_000_000_000, 'c'),
        ]}
        dimension="model"
      />
    );

    // Two distinct currency groups.
    expect(screen.getByTestId('currency-group-USD')).toBeInTheDocument();
    expect(screen.getByTestId('currency-group-EUR')).toBeInTheDocument();

    // Each subtotal is per-currency: USD = 1+2, EUR = 5. Never summed.
    expect(screen.getByTestId('subtotal-USD')).toHaveTextContent('3.00 USD');
    expect(screen.getByTestId('subtotal-EUR')).toHaveTextContent('5.00 EUR');

    // Exactly two subtotals and no combined grand-total node exists.
    expect(screen.getAllByText(/Subtotal \(/)).toHaveLength(2);
    expect(screen.queryByText('8.00 USD')).not.toBeInTheDocument();
    expect(screen.queryByText('8.00 EUR')).not.toBeInTheDocument();
  });

  it('renders an empty state when there are no rows', () => {
    render(<UsageTable rows={[]} dimension="model" />);
    expect(screen.getByText('No usage recorded')).toBeInTheDocument();
  });
});
