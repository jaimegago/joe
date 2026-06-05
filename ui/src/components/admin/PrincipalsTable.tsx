import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { disablePrincipal, enablePrincipal } from '@/api/security';
import { ApiRequestError } from '@/api/client';
import type { PrincipalRecord } from '@/api/types';

// PrincipalRow joins the registry record with the zones and admin status the
// page composed client-side from the policies list and admin roster.
export interface PrincipalRow {
  record: PrincipalRecord;
  zones: string[];
  isAdmin: boolean;
}

interface PrincipalsTableProps {
  rows: PrincipalRow[];
  // The authenticated principal (from /me). Its Disable control is hard-disabled
  // to prevent self-lockout; null in auth-disabled local mode.
  selfPrincipal: string | null;
}

function fmt(ts?: string): string {
  return ts ? new Date(ts).toLocaleString() : '—';
}

export function PrincipalsTable({ rows, selfPrincipal }: PrincipalsTableProps) {
  const qc = useQueryClient();
  const [disabling, setDisabling] = useState<PrincipalRecord | null>(null);

  const disableMut = useMutation({
    mutationFn: (principal: string) => disablePrincipal(principal),
    onSuccess: () => {
      toast.success('Principal disabled; active sessions revoked');
      void qc.invalidateQueries({ queryKey: ['principals'] });
    },
    onError: (e: Error) => {
      // Self-disable is blocked client-side, but a 409 backstop handles a stale
      // self (e.g. principal changed between render and click).
      if (e instanceof ApiRequestError && e.status === 409) {
        toast.error(e.message);
        return;
      }
      toast.error(e.message);
    },
  });

  const enableMut = useMutation({
    mutationFn: (principal: string) => enablePrincipal(principal),
    onSuccess: () => {
      toast.success('Principal enabled');
      void qc.invalidateQueries({ queryKey: ['principals'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Principal</TableHead>
            <TableHead>Display Name</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Last Seen</TableHead>
            <TableHead>Zones</TableHead>
            <TableHead>Admin</TableHead>
            <TableHead></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map(({ record, zones, isAdmin }) => {
            const isSelf = selfPrincipal !== null && record.principal === selfPrincipal;
            const disabled = record.status === 'disabled';
            return (
              <TableRow key={record.principal}>
                <TableCell className="font-medium">{record.principal}</TableCell>
                <TableCell className="text-muted-foreground text-sm">
                  {record.display_name ?? '—'}
                </TableCell>
                <TableCell>
                  {disabled ? (
                    <div className="space-y-0.5">
                      <Badge variant="destructive">disabled</Badge>
                      <p className="text-xs text-muted-foreground">
                        by {record.disabled_by ?? 'unknown'} · {fmt(record.disabled_at)}
                      </p>
                    </div>
                  ) : (
                    <Badge variant="secondary">active</Badge>
                  )}
                </TableCell>
                <TableCell className="text-muted-foreground text-sm">{fmt(record.last_seen_at)}</TableCell>
                <TableCell>
                  {isAdmin ? (
                    <span className="text-muted-foreground text-sm">all zones (admin)</span>
                  ) : zones.length > 0 ? (
                    <div className="flex flex-wrap gap-1">
                      {zones.map((z) => (
                        <Badge key={z} variant="secondary" className="text-xs">{z}</Badge>
                      ))}
                    </div>
                  ) : (
                    <span className="text-muted-foreground text-sm">—</span>
                  )}
                </TableCell>
                <TableCell>
                  {isAdmin ? <Badge>admin</Badge> : <span className="text-muted-foreground text-sm">—</span>}
                </TableCell>
                <TableCell>
                  <div className="flex justify-end">
                    {disabled ? (
                      <Button variant="outline" size="sm" onClick={() => enableMut.mutate(record.principal)}>
                        Enable
                      </Button>
                    ) : (
                      <Button
                        variant="destructive"
                        size="sm"
                        disabled={isSelf}
                        title={isSelf ? 'You cannot disable your own account.' : undefined}
                        onClick={() => setDisabling(record)}
                      >
                        Disable
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>

      <ConfirmDialog
        open={disabling !== null}
        onOpenChange={(o) => !o && setDisabling(null)}
        title="Disable principal"
        description={
          disabling
            ? `Disable "${disabling.principal}"? This immediately revokes their active sessions — they are logged out at once and cannot log back in until re-enabled.`
            : ''
        }
        confirmLabel="Disable"
        variant="destructive"
        onConfirm={() => disabling && disableMut.mutate(disabling.principal)}
      />
    </>
  );
}
