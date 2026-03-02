import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { deletePolicy } from '@/api/security';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import type { RbacPolicy } from '@/api/types';

interface PoliciesTableProps {
  policies: RbacPolicy[];
}

export function PoliciesTable({ policies }: PoliciesTableProps) {
  const qc = useQueryClient();
  const [deleting, setDeleting] = useState<number | null>(null);

  const deleteMut = useMutation({
    mutationFn: (id: number) => deletePolicy(id),
    onSuccess: () => {
      toast.success('Policy deleted');
      void qc.invalidateQueries({ queryKey: ['policies'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Principal</TableHead>
            <TableHead>Zone</TableHead>
            <TableHead>Created</TableHead>
            <TableHead></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {policies.map((p) => (
            <TableRow key={p.id}>
              <TableCell className="font-medium">{p.principal}</TableCell>
              <TableCell>
                <Badge variant="secondary">{p.zone_id}</Badge>
              </TableCell>
              <TableCell className="text-muted-foreground text-sm">
                {new Date(p.created_at).toLocaleString()}
              </TableCell>
              <TableCell>
                <Button variant="destructive" size="sm" onClick={() => setDeleting(p.id)}>Delete</Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
        title="Delete policy"
        description="This will remove the RBAC policy."
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={() => deleting !== null && deleteMut.mutate(deleting)}
      />
    </>
  );
}
