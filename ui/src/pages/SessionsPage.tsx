import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import { formatDistanceToNow } from 'date-fns';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { SessionRow, type SessionRowActions } from '@/components/sessions/SessionRow';
import { IncidentClusterList } from '@/components/sessions/IncidentClusterList';
import { groupSessions } from '@/lib/sessionGrouping';
import { sessionLabel } from '@/lib/sessionLabel';
import {
  fetchSessions,
  fetchTrash,
  updateSessionTitle,
  deleteSession,
  restoreSession,
  promoteSessionToIncident,
  createSession,
} from '@/api/chat';
import { ApiRequestError } from '@/api/client';
import type { Session } from '@/api/types';
import { MessageSquare, Plus, Trash2, AlertTriangle, RotateCcw, ShieldAlert } from 'lucide-react';

// Browse limit for the sessions list — generous enough to show a working
// history without paging, which is a later phase (P3).
const SESSIONS_LIMIT = 50;

// The primary axis is now the two-view split (docs/DESIGN-SESSIONS-VIEW.md §1.2):
// Conversations (incident-free) and Incidents (clustered), with Trash retained
// as-is. The old flat "All sessions" listing is replaced by these two views; the
// former "Mine" tab becomes a toggle on the Conversations view.
type View = 'conversations' | 'incidents' | 'trash';

// remainingBeforePurge renders the §12.5 trash-grace countdown from purge_after.
// No purge_after means the trash-then-purge policy has no grace window configured
// (or the inactivity sweep is off), so the session sits in trash indefinitely.
function remainingBeforePurge(purgeAfter?: string): string {
  if (!purgeAfter) return 'no automatic purge scheduled';
  const d = new Date(purgeAfter);
  if (Number.isNaN(d.getTime())) return 'no automatic purge scheduled';
  return `purges ${formatDistanceToNow(d, { addSuffix: true })}`;
}

export function SessionsPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const declareMode = searchParams.get('declare') != null;

  const [view, setView] = useState<View>('conversations');
  // mineOnly is the retained "Mine" filter, scoped to the Conversations view
  // (docs/DESIGN-SESSIONS-VIEW.md §2 / §3): the incident view shows all owners,
  // and "filter to mine" on incidents is the explicitly deferred item.
  const [mineOnly, setMineOnly] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draftTitle, setDraftTitle] = useState('');
  const [pendingDelete, setPendingDelete] = useState<Session | null>(null);

  // Conversations honor the Mine toggle (server-narrowed, exactly as the old
  // Mine tab). Incidents always fetch the full team-wide list (all owners) so
  // clusters are complete regardless of who owns each member.
  const mineActive = view === 'conversations' && mineOnly;
  const sessionsQ = useQuery({
    queryKey: [
      'sessions',
      view === 'incidents' ? 'all' : 'conversations',
      mineActive,
      SESSIONS_LIMIT,
    ],
    queryFn: () => fetchSessions({ mine: mineActive, limit: SESSIONS_LIMIT }),
    enabled: view !== 'trash',
  });

  const trashQ = useQuery({
    queryKey: ['sessions', 'trash', SESSIONS_LIMIT],
    queryFn: () => fetchTrash(SESSIONS_LIMIT),
    enabled: view === 'trash',
  });

  const renameMut = useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) => updateSessionTitle(id, title),
    onSuccess: () => {
      toast.success('Session renamed');
      setEditingId(null);
      void qc.invalidateQueries({ queryKey: ['sessions'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  // Delete is a SOFT delete — it moves the session to trash (recoverable), not a
  // hard delete (§12.5 macOS-trash). Purge is admin-only.
  const deleteMut = useMutation({
    mutationFn: (id: string) => deleteSession(id),
    onSuccess: () => {
      toast.success('Session moved to trash');
      setPendingDelete(null);
      void qc.invalidateQueries({ queryKey: ['sessions'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const restoreMut = useMutation({
    mutationFn: (id: string) => restoreSession(id),
    onSuccess: () => {
      toast.success('Session restored');
      void qc.invalidateQueries({ queryKey: ['sessions'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  // Promote an existing session in place to the incident master (§12.3). Both the
  // global declare flow's "promote existing" branch and this list's per-row action
  // target the one promote-incident route.
  const promoteMut = useMutation({
    mutationFn: (id: string) => promoteSessionToIncident(id),
    onSuccess: (res) => {
      toast.success('Incident declared');
      clearDeclareMode();
      void qc.invalidateQueries({ queryKey: ['sessions'] });
      void qc.invalidateQueries({ queryKey: ['regime'] });
      navigate(`/chat/${res.session_id}`);
    },
    onError: (e: Error) => toast.error(promoteErrorMessage(e)),
  });

  // Start-new is create-empty-then-promote (§12.3 / §12.10): two existing
  // primitives sequenced in the UI — create a fresh default session, then promote
  // it in place. Not a backend orchestration.
  const startNewIncidentMut = useMutation({
    mutationFn: async () => {
      const created = await createSession();
      return promoteSessionToIncident(created.id);
    },
    onSuccess: (res) => {
      toast.success('Incident declared on a new session');
      clearDeclareMode();
      void qc.invalidateQueries({ queryKey: ['sessions'] });
      void qc.invalidateQueries({ queryKey: ['regime'] });
      navigate(`/chat/${res.session_id}`);
    },
    onError: (e: Error) => toast.error(promoteErrorMessage(e)),
  });

  const clearDeclareMode = () => {
    if (declareMode) {
      searchParams.delete('declare');
      setSearchParams(searchParams, { replace: true });
    }
  };

  const startEdit = (s: Session) => {
    setEditingId(s.id);
    setDraftTitle(sessionLabel(s));
  };

  const commitEdit = (id: string) => {
    const title = draftTitle.trim();
    if (!title) {
      toast.error('Title must not be empty');
      return;
    }
    renameMut.mutate({ id, title });
  };

  // The shared row action/edit-state bundle threaded into both views' rows.
  const rowActions: SessionRowActions = {
    editingId,
    draftTitle,
    setDraftTitle,
    onStartEdit: startEdit,
    onCommitEdit: commitEdit,
    onCancelEdit: () => setEditingId(null),
    onDelete: setPendingDelete,
    onPromote: (id) => promoteMut.mutate(id),
    renamePending: renameMut.isPending,
    promotePending: promoteMut.isPending,
  };

  // The single P0-driven split: conversations (incident-free) vs incident
  // clusters. The membership predicate is read from each row's incident_involved
  // flag inside groupSessions — never re-derived here.
  const grouped = groupSessions(sessionsQ.data ?? []);
  const trashed = trashQ.data ?? [];
  const loading = view === 'trash' ? trashQ.isLoading : sessionsQ.isLoading;

  return (
    <>
      <Header
        title="Sessions"
        actions={
          <Button variant="outline" size="sm" onClick={() => navigate('/chat')}>
            <Plus className="mr-1 h-3 w-3" />
            New chat
          </Button>
        }
      />
      <PageContainer>
        {declareMode && (
          <div className="mb-6 rounded-md border border-amber-300 bg-amber-50 p-4 dark:border-amber-700 dark:bg-amber-950">
            <div className="flex items-start gap-3">
              <AlertTriangle
                className="mt-0.5 h-5 w-5 shrink-0 text-amber-700 dark:text-amber-300"
                aria-hidden="true"
              />
              <div className="flex-1">
                <p className="font-semibold text-amber-900 dark:text-amber-100">
                  Declare an incident
                </p>
                <p className="mt-1 text-sm text-amber-800 dark:text-amber-200">
                  Promote one of your conversations below to the incident master, or start a
                  brand-new incident on a fresh session.
                </p>
                <div className="mt-3 flex gap-2">
                  <Button
                    size="sm"
                    onClick={() => startNewIncidentMut.mutate()}
                    disabled={startNewIncidentMut.isPending}
                  >
                    <AlertTriangle className="mr-1 h-3 w-3" />
                    Start a new incident
                  </Button>
                  <Button size="sm" variant="ghost" onClick={clearDeclareMode}>
                    Cancel
                  </Button>
                </div>
              </div>
            </div>
          </div>
        )}

        <Tabs value={view} onValueChange={(v) => setView(v as View)}>
          <TabsList className="mb-4">
            <TabsTrigger value="conversations">Conversations</TabsTrigger>
            <TabsTrigger value="incidents">Incidents</TabsTrigger>
            <TabsTrigger value="trash">Trash</TabsTrigger>
          </TabsList>

          {loading ? (
            <LoadingPage />
          ) : (
            <>
              <TabsContent value="conversations">
                <div className="mb-4 flex items-center justify-between">
                  <Button
                    size="sm"
                    variant={mineOnly ? 'default' : 'outline'}
                    aria-pressed={mineOnly}
                    onClick={() => setMineOnly((v) => !v)}
                  >
                    Mine only
                  </Button>
                </div>
                {grouped.conversations.length === 0 ? (
                  <EmptyState
                    icon={MessageSquare}
                    title={mineOnly ? 'You have no conversations' : 'No conversations yet'}
                    description={
                      mineOnly
                        ? 'Conversations you create appear here.'
                        : 'Chat sessions across your team that are not part of an incident appear here.'
                    }
                  />
                ) : (
                  <ul className="divide-y rounded-md border">
                    {grouped.conversations.map((s) => (
                      <li key={s.id}>
                        <SessionRow {...rowActions} session={s} declareMode={declareMode} />
                      </li>
                    ))}
                  </ul>
                )}
              </TabsContent>

              <TabsContent value="incidents">
                <IncidentClusterList {...rowActions} clusters={grouped.clusters} />
              </TabsContent>

              <TabsContent value="trash">
                {trashed.length === 0 ? (
                  <EmptyState
                    icon={Trash2}
                    title="Trash is empty"
                    description="Sessions you delete land here, recoverable until they are purged."
                  />
                ) : (
                  <ul className="divide-y rounded-md border">
                    {trashed.map((s) => (
                      <li key={s.id} className="flex items-center gap-3 px-4 py-3">
                        <Trash2 className="h-4 w-4 shrink-0 text-muted-foreground" />
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-2 truncate font-medium">
                            <span className="truncate">{sessionLabel(s)}</span>
                            {/* Trashed incident-involved sessions keep their
                                marker so trash is not split by view (P1 leaves
                                trash behavior unchanged). */}
                            {s.incident_involved && (
                              <Badge variant="outline" className="shrink-0 text-muted-foreground">
                                <ShieldAlert className="mr-1 h-3 w-3" aria-hidden="true" />
                                Incident
                              </Badge>
                            )}
                          </div>
                          <p className="text-xs text-muted-foreground">
                            {remainingBeforePurge(s.purge_after)}
                            {' · '}
                            {s.message_count} {s.message_count === 1 ? 'message' : 'messages'}
                          </p>
                        </div>
                        <Button
                          size="sm"
                          variant="outline"
                          className="shrink-0"
                          onClick={() => restoreMut.mutate(s.id)}
                          disabled={restoreMut.isPending}
                        >
                          <RotateCcw className="mr-1 h-3 w-3" />
                          Restore
                        </Button>
                      </li>
                    ))}
                  </ul>
                )}
              </TabsContent>
            </>
          )}
        </Tabs>

        <ConfirmDialog
          open={pendingDelete !== null}
          onOpenChange={(open) => {
            if (!open) setPendingDelete(null);
          }}
          title="Move session to trash"
          description={`"${pendingDelete ? sessionLabel(pendingDelete) : ''}" will be moved to trash. You can restore it from the Trash tab until it is purged.`}
          confirmLabel="Move to trash"
          variant="destructive"
          onConfirm={() => pendingDelete && deleteMut.mutate(pendingDelete.id)}
        />
      </PageContainer>
    </>
  );
}

// promoteErrorMessage maps the promote-incident route's failure codes to an
// actionable message: 409 = an incident is already active (single global regime).
function promoteErrorMessage(e: Error): string {
  if (e instanceof ApiRequestError && e.status === 409) {
    return 'An incident is already active — resolve it before declaring another.';
  }
  return e.message;
}
