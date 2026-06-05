import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { removeAdmin } from '@/api/security';
import { ApiRequestError } from '@/api/client';
import type { Admin } from '@/api/types';

interface AdminsTableProps {
  admins: Admin[];
}

export function AdminsTable({ admins }: AdminsTableProps) {
  const qc = useQueryClient();
  const [removing, setRemoving] = useState<Admin | null>(null);

  // The last-admin rule is enforced server-side (409); pre-disable the control
  // client-side from the roster count so the only-admin case is unreachable by
  // click, with a tooltip explaining why.
  const isLastAdmin = admins.length === 1;

  const removeMut = useMutation({
    mutationFn: (principal: string) => removeAdmin(principal),
    onSuccess: () => {
      toast.success('Admin removed');
      void qc.invalidateQueries({ queryKey: ['admins'] });
    },
    onError: (e: Error) => {
      // 409 covers the two backend guards: the configured bootstrap admin
      // (auth.admin_email must change first — the roster does not flag which
      // principal that is, so it is surfaced reactively) and the last-admin
      // guard (also pre-disabled above). e.message carries the actionable text.
      if (e instanceof ApiRequestError && e.status === 409) {
        toast.error(e.message);
        return;
      }
      toast.error(e.message);
    },
  });

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Principal</TableHead>
            <TableHead>Granted By</TableHead>
            <TableHead>Granted At</TableHead>
            <TableHead>Reason</TableHead>
            <TableHead></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {admins.map((a) => (
            <TableRow key={a.principal}>
              <TableCell className="font-medium">{a.principal}</TableCell>
              <TableCell className="text-muted-foreground text-sm">{a.granted_by || '—'}</TableCell>
              <TableCell className="text-muted-foreground text-sm">
                {new Date(a.granted_at).toLocaleString()}
              </TableCell>
              <TableCell className="text-muted-foreground text-sm">{a.reason || '—'}</TableCell>
              <TableCell>
                <div className="flex justify-end">
                  <Button
                    variant="destructive"
                    size="sm"
                    disabled={isLastAdmin}
                    title={
                      isLastAdmin
                        ? 'Cannot remove the last remaining admin. Grant another principal admin first.'
                        : undefined
                    }
                    onClick={() => setRemoving(a)}
                  >
                    Remove
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(o) => !o && setRemoving(null)}
        title="Remove admin"
        description={
          removing
            ? `Remove admin authority from "${removing.principal}"? They keep any explicitly granted zones but lose admin access.`
            : ''
        }
        confirmLabel="Remove"
        variant="destructive"
        onConfirm={() => removing && removeMut.mutate(removing.principal)}
      />
    </>
  );
}
