import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Checkbox } from '@/components/ui/checkbox';
import { setReadPromotion } from '@/api/security';
import type { ReadPromotion } from '@/api/types';

const QUERY_KEY = ['read-promotions'] as const;

// ReadPromotionsTable is the operator control over autonomous reads: one toggle per
// component type. Turning a type on lets Joe read components of that type on its own.
// It is a read-admit predicate only — it can never let Joe change anything — and
// every type is off until an operator turns it on. The weight lives in this section
// framing and in the act of flipping a toggle, not in per-row warnings.
//
// Admin-gated by construction: the only host is the admin page, whose whole route is
// behind <RequireAdmin>, so a non-admin never sees or operates this control.
export function ReadPromotionsTable({ promotions }: { promotions: ReadPromotion[] }) {
  const qc = useQueryClient();

  // Stable presentation: the GET's enum order is unspecified, so sort by type name.
  const rows = [...promotions].sort((a, b) => a.component_type.localeCompare(b.component_type));

  // One mutation drives every row. Each toggle is an independent, backend-audited
  // change. Optimistic: flip the cached row immediately, roll the snapshot back on
  // error, and re-fetch on success so the row reflects the persisted state.
  const toggleMut = useMutation({
    mutationFn: ({ componentType, enabled }: { componentType: string; enabled: boolean }) =>
      setReadPromotion(componentType, enabled),
    onMutate: async ({ componentType, enabled }) => {
      await qc.cancelQueries({ queryKey: QUERY_KEY });
      const previous = qc.getQueryData<ReadPromotion[]>(QUERY_KEY);
      qc.setQueryData<ReadPromotion[]>(QUERY_KEY, (old) =>
        (old ?? []).map((p) => (p.component_type === componentType ? { ...p, enabled } : p))
      );
      return { previous };
    },
    onSuccess: (res) => {
      toast.success(
        res.enabled
          ? `Autonomous reads on for ${res.component_type}`
          : `Autonomous reads off for ${res.component_type}`
      );
    },
    onError: (e: Error, _vars, context) => {
      if (context?.previous) qc.setQueryData(QUERY_KEY, context.previous);
      toast.error(e.message);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: QUERY_KEY });
    },
  });

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <h3 className="text-sm font-medium">Autonomous reads</h3>
        <p className="text-muted-foreground max-w-2xl text-sm">
          Turn a component type on to let Joe read components of that type on its own, without an
          operator driving each read. This is read-only — it never lets Joe change anything. Every
          type is off by default; turning one on is recorded.
        </p>
      </div>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Component type</TableHead>
            <TableHead>Autonomous reads</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => {
            const pending =
              toggleMut.isPending && toggleMut.variables?.componentType === row.component_type;
            return (
              <TableRow key={row.component_type}>
                <TableCell className="font-mono text-sm">{row.component_type}</TableCell>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <Checkbox
                      checked={row.enabled}
                      disabled={pending}
                      aria-label={`Autonomous reads for ${row.component_type}`}
                      onCheckedChange={(v) =>
                        toggleMut.mutate({
                          componentType: row.component_type,
                          enabled: v === true,
                        })
                      }
                    />
                    <span className="text-muted-foreground text-sm">
                      {row.enabled ? 'On' : 'Off'}
                    </span>
                  </div>
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}
