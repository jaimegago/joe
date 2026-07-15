import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { fetchComponentZones, removeZone } from '@/api/security';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { QUERY_KEYS } from '@/lib/queryKeys';

export function ComponentZoneAssign() {
  const qc = useQueryClient();
  const { data = [] } = useQuery({
    queryKey: QUERY_KEYS.componentZones,
    queryFn: fetchComponentZones,
  });
  const [unassigning, setUnassigning] = useState<string | null>(null);

  const removeMut = useMutation({
    mutationFn: (componentId: string) => removeZone(componentId),
    onSuccess: () => {
      toast.success('Component unassigned');
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.componentZones });
      // The source returns to the unassigned pool — refresh that list too.
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.unassigned });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Component</TableHead>
            <TableHead>Zone</TableHead>
            <TableHead>Assigned By</TableHead>
            <TableHead>Assigned At</TableHead>
            <TableHead></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {data.map((a) => (
            <TableRow key={a.component_id}>
              <TableCell className="font-mono text-sm">{a.component_id}</TableCell>
              <TableCell>{a.zone_id}</TableCell>
              <TableCell className="text-muted-foreground text-sm">{a.assigned_by}</TableCell>
              <TableCell className="text-muted-foreground text-sm">
                {new Date(a.assigned_at).toLocaleString()}
              </TableCell>
              <TableCell>
                <div className="flex justify-end">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setUnassigning(a.component_id)}
                  >
                    Unassign
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <ConfirmDialog
        open={unassigning !== null}
        onOpenChange={(o) => !o && setUnassigning(null)}
        title="Unassign component"
        description={
          unassigning
            ? `Remove the zone assignment for "${unassigning}"? It will fall back to the default unassigned zone until reassigned.`
            : ''
        }
        confirmLabel="Unassign"
        variant="destructive"
        onConfirm={() => unassigning && removeMut.mutate(unassigning)}
      />
    </>
  );
}
