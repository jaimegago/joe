import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { EmptyState } from '@/components/common/EmptyState';
import { Activity } from 'lucide-react';
import { groupByCurrency, formatNanoCost } from '@/lib/usage';
import type { UsageBreakdown } from '@/api/types';

// dimension is the row's grouping key — the column whose value labels each
// row (e.g. the model name or the principal). aggregate rollups have no
// such dimension and pass undefined.
type Dimension = 'model' | 'principal';

interface UsageTableProps {
  rows: UsageBreakdown[];
  dimension?: Dimension;
}

const dimensionHeader: Record<Dimension, string> = {
  model: 'Model',
  principal: 'Principal',
};

// UsageTable groups rows by their own currency and renders one section per
// currency with a per-currency subtotal. It NEVER renders a cross-currency
// grand total — cost in different currencies is not addable. At launch a
// single currency yields one section; a second currency would simply yield
// a second section with its own subtotal.
export function UsageTable({ rows, dimension }: UsageTableProps) {
  if (rows.length === 0) {
    return (
      <EmptyState
        icon={Activity}
        title="No usage recorded"
        description="No LLM calls in this window yet."
      />
    );
  }
  const groups = groupByCurrency(rows);
  const header = dimension ? dimensionHeader[dimension] : undefined;

  return (
    <div className="space-y-6">
      {groups.map((group) => (
        <div key={group.currency} data-testid={`currency-group-${group.currency}`}>
          <div className="mb-2 text-sm font-medium text-muted-foreground">
            Currency: {group.currency}
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                {header && <TableHead>{header}</TableHead>}
                <TableHead className="text-right">Calls</TableHead>
                <TableHead className="text-right">Input tokens</TableHead>
                <TableHead className="text-right">Output tokens</TableHead>
                <TableHead className="text-right">Est. cost</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {group.rows.map((row, i) => (
                <TableRow key={dimension ? (row[dimension] ?? i) : i}>
                  {header && (
                    <TableCell className="font-medium">
                      {dimension && row[dimension] ? row[dimension] : '—'}
                    </TableCell>
                  )}
                  <TableCell className="text-right">{row.calls.toLocaleString()}</TableCell>
                  <TableCell className="text-right">{row.input_tokens.toLocaleString()}</TableCell>
                  <TableCell className="text-right">{row.output_tokens.toLocaleString()}</TableCell>
                  <TableCell className="text-right">
                    {formatNanoCost(row.estimated_cost_nano, group.currency)}
                  </TableCell>
                </TableRow>
              ))}
              <TableRow className="font-semibold">
                {header && <TableCell>Subtotal ({group.currency})</TableCell>}
                <TableCell className="text-right">
                  {group.rows.reduce((n, r) => n + r.calls, 0).toLocaleString()}
                </TableCell>
                <TableCell className="text-right">
                  {group.rows.reduce((n, r) => n + r.input_tokens, 0).toLocaleString()}
                </TableCell>
                <TableCell className="text-right">
                  {group.rows.reduce((n, r) => n + r.output_tokens, 0).toLocaleString()}
                </TableCell>
                <TableCell className="text-right" data-testid={`subtotal-${group.currency}`}>
                  {formatNanoCost(group.subtotalNano, group.currency)}
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      ))}
    </div>
  );
}
