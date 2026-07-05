import { useState } from 'react';
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
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { ZoneForm } from '@/components/admin/ZoneForm';
import { updateZone, deleteZone } from '@/api/security';
import { ApiRequestError } from '@/api/client';
import { QUERY_KEYS } from '@/lib/queryKeys';
import type { SecurityZone } from '@/api/types';

interface ZonesTableProps {
  zones: SecurityZone[];
}

export function ZonesTable({ zones }: ZonesTableProps) {
  const qc = useQueryClient();
  const [editing, setEditing] = useState<SecurityZone | null>(null);
  const [deleting, setDeleting] = useState<SecurityZone | null>(null);

  const updateMut = useMutation({
    mutationFn: ({
      id,
      patch,
    }: {
      id: string;
      patch: { name?: string; description?: string; allowed_actions?: string[] };
    }) => updateZone(id, patch),
    onSuccess: () => {
      toast.success('Zone updated');
      setEditing(null);
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.zones });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const deleteMut = useMutation({
    mutationFn: (zone: SecurityZone) => deleteZone(zone.id),
    onSuccess: () => {
      toast.success('Zone deleted');
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.zones });
      // A delete cascades grants and frees components, so refresh the dependent lists.
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.policies });
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.componentZones });
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.unassigned });
    },
    onError: (e: Error, zone) => {
      // The zone-in-use case is a 409: the zone still has source assignments and
      // the RESTRICT FK refuses the delete. Surface the actionable reassign-first
      // message rather than a generic failure toast.
      if (e instanceof ApiRequestError && e.status === 409) {
        toast.error(
          `Zone "${zone.name || zone.id}" still has source assignments and cannot be deleted. ` +
            `Reassign those components to another zone first (Components tab), then delete it.`
        );
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
            <TableHead>Zone</TableHead>
            <TableHead>Description</TableHead>
            <TableHead>Actions</TableHead>
            <TableHead>Components</TableHead>
            <TableHead></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {zones.map((z) => (
            <TableRow key={z.id}>
              <TableCell className="font-medium">{z.name || z.id}</TableCell>
              <TableCell className="text-muted-foreground text-sm">{z.description}</TableCell>
              <TableCell>
                <div className="flex flex-wrap gap-1">
                  {z.allowed_actions.map((a) => (
                    <Badge key={a} variant="secondary" className="text-xs">
                      {a}
                    </Badge>
                  ))}
                </div>
              </TableCell>
              <TableCell>{z.sourceCount ?? 0}</TableCell>
              <TableCell>
                <div className="flex justify-end gap-2">
                  <Button variant="outline" size="sm" onClick={() => setEditing(z)}>
                    Edit
                  </Button>
                  <Button variant="destructive" size="sm" onClick={() => setDeleting(z)}>
                    Delete
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      {editing && (
        <ZoneForm
          open={editing !== null}
          onOpenChange={(o) => !o && setEditing(null)}
          initial={editing}
          onSubmit={(data) =>
            updateMut.mutate({
              id: editing.id,
              patch: {
                name: data.name,
                description: data.description,
                allowed_actions: data.allowed_actions,
              },
            })
          }
          isLoading={updateMut.isPending}
        />
      )}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
        title="Delete zone"
        description={
          deleting
            ? `Delete zone "${deleting.name || deleting.id}"? Any policies granting this zone are removed. Components assigned to it must be reassigned first.`
            : ''
        }
        confirmLabel="Delete"
        variant="destructive"
        onConfirm={() => deleting && deleteMut.mutate(deleting)}
      />
    </>
  );
}
