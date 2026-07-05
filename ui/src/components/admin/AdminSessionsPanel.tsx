import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { formatDistanceToNow } from 'date-fns';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { QueryError } from '@/components/common/QueryError';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { RetentionPolicyEditor } from './RetentionPolicyEditor';
import { useAdminSessions } from '@/hooks/useAdminSessions';
import { useRegime } from '@/hooks/useRegime';
import {
  previewPurge,
  confirmPurge,
  archiveSession,
  restoreArchive,
  type SessionState,
} from '@/api/adminSessions';
import { ApiRequestError } from '@/api/client';
import type { Session, PurgeManifest } from '@/api/types';
import { Database, Archive, Trash2, RotateCcw, AlertTriangle } from 'lucide-react';

function sessionLabel(s: Session): string {
  return s.title ?? s.summary ?? 'Untitled session';
}

function formatPrincipal(principal?: string): string {
  if (!principal) return 'unknown';
  return principal.replace(/^user:/, '');
}

interface PurgeTarget {
  session: Session;
  manifest: PurgeManifest;
}

// AdminSessionsPanel is the §12.10 admin sessions console: the cross-tenant list
// filterable by principal/type/state, the purge two-step (manifest then confirm),
// archive and restore-archive, the all-trash view, and the retention-policy editor.
// It drives only existing admin endpoints — no backend authorization, lifecycle,
// sweeper, or archive logic is changed here.
//
// Creator vs captain (§12.3): every row shows its creator_principal (the owner).
// When an incident is active, the active-incident card names the captain (the
// regime declarer) — a SEPARATE principal from any row's creator, never conflated.
export function AdminSessionsPanel() {
  const qc = useQueryClient();
  const [state, setState] = useState<SessionState>('active');
  const [principalFilter, setPrincipalFilter] = useState('');
  const [typeFilter, setTypeFilter] = useState<string>('');
  const [purgeTarget, setPurgeTarget] = useState<PurgeTarget | null>(null);

  const { data: regime } = useRegime();
  const incidentActive = regime?.mode === 'incident';

  const sessionsQ = useAdminSessions({
    state,
    principal: principalFilter.trim() || undefined,
    type: typeFilter || undefined,
  });

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: ['admin-sessions'] });
  };

  // Purge step one: dry-run the manifest (destroys nothing), then open the
  // hard-stop confirm dialog (§12.5).
  const previewMut = useMutation({
    mutationFn: (s: Session) => previewPurge(s.id).then((p) => ({ s, p })),
    onSuccess: ({ s, p }) => setPurgeTarget({ session: s, manifest: p.manifest }),
    onError: (e: Error) => toast.error(e.message),
  });

  // Purge step two: the explicit confirm fires the irreversible expunge.
  const confirmPurgeMut = useMutation({
    mutationFn: (id: string) => confirmPurge(id),
    onSuccess: () => {
      toast.success('Session purged');
      setPurgeTarget(null);
      invalidate();
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const archiveMut = useMutation({
    mutationFn: (id: string) => archiveSession(id),
    onSuccess: () => {
      toast.success('Session archived');
      invalidate();
    },
    onError: (e: Error) =>
      toast.error(
        e instanceof ApiRequestError && e.status === 503
          ? 'No archive directory is configured — set one before archiving (§12.6).'
          : e.message
      ),
  });

  const restoreArchiveMut = useMutation({
    mutationFn: (id: string) => restoreArchive(id),
    onSuccess: () => {
      toast.success('Session restored from archive');
      invalidate();
    },
    onError: (e: Error) =>
      toast.error(
        e instanceof ApiRequestError && e.status === 422
          ? 'Archive artifact has an unsupported schema version; refusing to restore.'
          : e.message
      ),
  });

  const sessions = sessionsQ.data ?? [];

  return (
    <div className="space-y-6">
      <RetentionPolicyEditor />

      {incidentActive && (
        <div className="flex items-center gap-2 rounded-md border border-amber-300 bg-amber-50 px-4 py-2 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950 dark:text-amber-200">
          <AlertTriangle className="h-4 w-4 shrink-0" aria-hidden="true" />
          <span>
            Active incident — captain{' '}
            <span className="font-semibold">
              {formatPrincipal(regime?.declaredByPrincipal ?? undefined)}
            </span>
            . The captain is distinct from each session’s creator (shown per row).
          </span>
        </div>
      )}

      <div className="flex flex-wrap items-end gap-3">
        <div className="w-56">
          <label className="mb-1 block text-xs text-muted-foreground" htmlFor="principal-filter">
            Filter by principal
          </label>
          <Input
            id="principal-filter"
            value={principalFilter}
            onChange={(e) => setPrincipalFilter(e.target.value)}
            placeholder="user:alice@example.com"
            className="h-8"
          />
        </div>
        <div className="w-44">
          <label className="mb-1 block text-xs text-muted-foreground" htmlFor="type-filter">
            Filter by type
          </label>
          <Select
            value={typeFilter || 'all'}
            onValueChange={(v) => setTypeFilter(v === 'all' ? '' : v)}
          >
            <SelectTrigger id="type-filter" className="h-8" aria-label="Filter by type">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All types</SelectItem>
              <SelectItem value="default">Default</SelectItem>
              <SelectItem value="incident">Incident</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <Tabs value={state} onValueChange={(v) => setState(v as SessionState)}>
        <TabsList>
          <TabsTrigger value="active">Active</TabsTrigger>
          <TabsTrigger value="trashed">Trashed</TabsTrigger>
          <TabsTrigger value="archived">Archived</TabsTrigger>
        </TabsList>

        <TabsContent value={state} className="mt-4">
          {sessionsQ.isLoading ? (
            <LoadingPage />
          ) : sessionsQ.isError ? (
            <QueryError
              error={sessionsQ.error}
              onRetry={() => void sessionsQ.refetch()}
              resourceLabel="sessions"
            />
          ) : sessions.length === 0 ? (
            <EmptyState
              icon={Database}
              title="No sessions"
              description={`No ${state} sessions match the current filters.`}
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Session</TableHead>
                  <TableHead>Creator</TableHead>
                  <TableHead>State detail</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sessions.map((s) => (
                  <TableRow key={s.id}>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <span className="font-medium">{sessionLabel(s)}</span>
                        {s.linked_incident_id != null && (
                          <Badge
                            variant="outline"
                            className="border-amber-300 text-amber-900 dark:border-amber-700 dark:text-amber-200"
                          >
                            Incident
                          </Badge>
                        )}
                      </div>
                      <div className="text-xs text-muted-foreground">
                        {s.message_count} {s.message_count === 1 ? 'message' : 'messages'}
                      </div>
                    </TableCell>
                    <TableCell className="text-sm">
                      {formatPrincipal(s.creator_principal)}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {state === 'trashed' && s.purge_after
                        ? `purges ${formatDistanceToNow(new Date(s.purge_after), { addSuffix: true })}`
                        : state === 'archived' && s.archived_at
                          ? `archived ${formatDistanceToNow(new Date(s.archived_at), { addSuffix: true })}`
                          : '—'}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        {state === 'active' && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => archiveMut.mutate(s.id)}
                            disabled={archiveMut.isPending}
                          >
                            <Archive className="mr-1 h-3 w-3" />
                            Archive
                          </Button>
                        )}
                        {state === 'archived' && (
                          <Button
                            size="sm"
                            variant="ghost"
                            onClick={() => restoreArchiveMut.mutate(s.id)}
                            disabled={restoreArchiveMut.isPending}
                          >
                            <RotateCcw className="mr-1 h-3 w-3" />
                            Restore
                          </Button>
                        )}
                        {state !== 'archived' && (
                          <Button
                            size="sm"
                            variant="ghost"
                            className="text-destructive hover:text-destructive"
                            onClick={() => previewMut.mutate(s)}
                            disabled={previewMut.isPending}
                          >
                            <Trash2 className="mr-1 h-3 w-3" />
                            Purge
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </TabsContent>
      </Tabs>

      {/* Purge two-step: the manifest-with-hard-stop confirm (§12.5). The dialog
          is open only once the manifest dry-run has returned; the explicit confirm
          button is what fires the irreversible expunge. */}
      <Dialog open={purgeTarget !== null} onOpenChange={(open) => !open && setPurgeTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Purge session permanently</DialogTitle>
            <DialogDescription>
              This irreversibly destroys “{purgeTarget ? sessionLabel(purgeTarget.session) : ''}”.
            </DialogDescription>
          </DialogHeader>
          {purgeTarget && (
            <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
              <p className="font-medium text-destructive">This will permanently destroy:</p>
              <ul className="mt-1 list-inside list-disc text-muted-foreground">
                <li>
                  <span className="font-medium text-foreground">
                    {purgeTarget.manifest.messages_destroyed}
                  </span>{' '}
                  message{purgeTarget.manifest.messages_destroyed === 1 ? '' : 's'}
                </li>
                <li>
                  <span className="font-medium text-foreground">
                    {purgeTarget.manifest.linked_children_severed}
                  </span>{' '}
                  linked child session
                  {purgeTarget.manifest.linked_children_severed === 1 ? '' : 'ren'} severed
                </li>
              </ul>
              <p className="mt-2 text-xs text-muted-foreground">
                This cannot be undone. There is no recovery after purge.
              </p>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setPurgeTarget(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={confirmPurgeMut.isPending}
              onClick={() => purgeTarget && confirmPurgeMut.mutate(purgeTarget.session.id)}
            >
              Purge permanently
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
