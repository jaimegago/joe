import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { fetchSourceZones, removeZone } from '@/api/security';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';

export function SourceZoneAssign() {
  const qc = useQueryClient();
  const { data = [] } = useQuery({ queryKey: ['source-zones'], queryFn: fetchSourceZones });
  const [unassigning, setUnassigning] = useState<string | null>(null);

  const removeMut = useMutation({
    mutationFn: (sourceId: string) => removeZone(sourceId),
    onSuccess: () => {
      toast.success('Source unassigned');
      void qc.invalidateQueries({ queryKey: ['source-zones'] });
      // The source returns to the unassigned pool — refresh that list too.
      void qc.invalidateQueries({ queryKey: ['unassigned'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Source</TableHead>
            <TableHead>Zone</TableHead>
            <TableHead>Assigned By</TableHead>
            <TableHead>Assigned At</TableHead>
            <TableHead></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {data.map((a) => (
            <TableRow key={a.source_id}>
              <TableCell className="font-mono text-sm">{a.source_id}</TableCell>
              <TableCell>{a.zone_id}</TableCell>
              <TableCell className="text-muted-foreground text-sm">{a.assigned_by}</TableCell>
              <TableCell className="text-muted-foreground text-sm">
                {new Date(a.assigned_at).toLocaleString()}
              </TableCell>
              <TableCell>
                <div className="flex justify-end">
                  <Button variant="outline" size="sm" onClick={() => setUnassigning(a.source_id)}>
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
        title="Unassign source"
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
