import type { UsageBreakdown } from '@/api/types';

// Nano scale: costs are stored and returned in nano-units of the
// configured currency (1 currency unit = 1e9 nano-units), matching the
// backend's llm.CostNanoUnitsPerUnit. Divide to render currency units.
export const NANO_PER_UNIT = 1_000_000_000;

// formatNanoCost renders a nano-unit cost as currency units with its
// currency label. It never converts between currencies — the caller must
// only pass a value and the currency that value is denominated in.
export function formatNanoCost(nano: number, currency: string): string {
  const units = nano / NANO_PER_UNIT;
  const formatted = units.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 6,
  });
  return `${formatted} ${currency}`;
}

export interface CurrencyGroup {
  currency: string;
  rows: UsageBreakdown[];
  subtotalNano: number;
}

// groupByCurrency partitions usage rows by their own currency and
// subtotals the estimated cost within each currency. It deliberately
// returns a per-currency subtotal and NEVER a cross-currency total: cost
// in different currencies is not addable. At launch a single currency is
// in use so the result has one group, but the grouping stays correct if a
// second currency ever appears. Insertion order of first appearance is
// preserved for stable rendering.
export function groupByCurrency(rows: UsageBreakdown[]): CurrencyGroup[] {
  const order: string[] = [];
  const byCurrency = new Map<string, CurrencyGroup>();
  for (const row of rows) {
    let group = byCurrency.get(row.currency);
    if (!group) {
      group = { currency: row.currency, rows: [], subtotalNano: 0 };
      byCurrency.set(row.currency, group);
      order.push(row.currency);
    }
    group.rows.push(row);
    group.subtotalNano += row.estimated_cost_nano;
  }
  return order.map((c) => byCurrency.get(c)!);
}
