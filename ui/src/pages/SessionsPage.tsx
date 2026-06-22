import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';
import { formatDistanceToNow } from 'date-fns';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
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
import {
  MessageSquare,
  Plus,
  Pencil,
  Trash2,
  Check,
  X,
  AlertTriangle,
  Eye,
  RotateCcw,
} from 'lucide-react';

// Browse limit for the sessions list — generous enough to show a working
// history without paging, which is a later nicety.
const SESSIONS_LIMIT = 50;

type View = 'all' | 'mine' | 'trash';

function sessionLabel(s: Session): string {
  return s.title ?? s.summary ?? 'Untitled session';
}

// formatOwner renders a principal ("user:alice@example.com") as the bare identity
// for the "owned by" label. Falls back when the owner is absent.
function formatOwner(principal?: string): string {
  if (!principal) return 'another user';
  return principal.replace(/^user:/, '');
}

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

  const [view, setView] = useState<View>('all');
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draftTitle, setDraftTitle] = useState('');
  const [pendingDelete, setPendingDelete] = useState<Session | null>(null);

  const sessionsQ = useQuery({
    queryKey: ['sessions', view === 'mine' ? 'mine' : 'all', SESSIONS_LIMIT],
    queryFn: () => fetchSessions({ mine: view === 'mine', limit: SESSIONS_LIMIT }),
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

  const sessions = sessionsQ.data ?? [];
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
                  Promote one of your sessions below to the incident master, or start a brand-new
                  incident on a fresh session.
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
            <TabsTrigger value="all">All sessions</TabsTrigger>
            <TabsTrigger value="mine">Mine</TabsTrigger>
            <TabsTrigger value="trash">Trash</TabsTrigger>
          </TabsList>

          {loading ? (
            <LoadingPage />
          ) : (
            <>
              <TabsContent value="all">
                <SessionList
                  sessions={sessions}
                  emptyTitle="No sessions yet"
                  emptyDescription="Chat sessions across your team appear here."
                  declareMode={declareMode}
                  editingId={editingId}
                  draftTitle={draftTitle}
                  setDraftTitle={setDraftTitle}
                  onStartEdit={startEdit}
                  onCommitEdit={commitEdit}
                  onCancelEdit={() => setEditingId(null)}
                  onDelete={setPendingDelete}
                  onPromote={(id) => promoteMut.mutate(id)}
                  renamePending={renameMut.isPending}
                  promotePending={promoteMut.isPending}
                />
              </TabsContent>

              <TabsContent value="mine">
                <SessionList
                  sessions={sessions}
                  emptyTitle="You have no sessions"
                  emptyDescription="Sessions you create appear here."
                  declareMode={declareMode}
                  editingId={editingId}
                  draftTitle={draftTitle}
                  setDraftTitle={setDraftTitle}
                  onStartEdit={startEdit}
                  onCommitEdit={commitEdit}
                  onCancelEdit={() => setEditingId(null)}
                  onDelete={setPendingDelete}
                  onPromote={(id) => promoteMut.mutate(id)}
                  renamePending={renameMut.isPending}
                  promotePending={promoteMut.isPending}
                />
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
                          <p className="truncate font-medium">{sessionLabel(s)}</p>
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

interface SessionListProps {
  sessions: Session[];
  emptyTitle: string;
  emptyDescription: string;
  declareMode: boolean;
  editingId: string | null;
  draftTitle: string;
  setDraftTitle: (t: string) => void;
  onStartEdit: (s: Session) => void;
  onCommitEdit: (id: string) => void;
  onCancelEdit: () => void;
  onDelete: (s: Session) => void;
  onPromote: (id: string) => void;
  renamePending: boolean;
  promotePending: boolean;
}

// SessionList renders the team-wide list. A row the caller owns (read_only !==
// true) gets the owner controls (rename, move-to-trash, and — in declare mode —
// promote-to-incident); a row owned by another principal is a read-only viewer,
// badged and owner-attributed via shared_by.
function SessionList({
  sessions,
  emptyTitle,
  emptyDescription,
  declareMode,
  editingId,
  draftTitle,
  setDraftTitle,
  onStartEdit,
  onCommitEdit,
  onCancelEdit,
  onDelete,
  onPromote,
  renamePending,
  promotePending,
}: SessionListProps) {
  if (sessions.length === 0) {
    return <EmptyState icon={MessageSquare} title={emptyTitle} description={emptyDescription} />;
  }
  return (
    <ul className="divide-y rounded-md border">
      {sessions.map((s) => {
        const isEditing = editingId === s.id;
        const isOwner = s.read_only !== true;
        // The incident MASTER (the session the incident was declared on) is marked
        // by type, not by linked_incident_id — promote-in-place clears its pointer
        // (§12.3). A LINKED participant keeps its pointer. They are distinct badges.
        const isIncidentMaster = s.type === 'incident';
        const isLinked = s.linked_incident_id != null;
        const activity = s.last_activity_at ?? s.started_at;
        return (
          <li key={s.id} className="flex items-center gap-3 px-4 py-3">
            <MessageSquare className="h-4 w-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              {isEditing ? (
                <form
                  className="flex items-center gap-2"
                  onSubmit={(e) => {
                    e.preventDefault();
                    onCommitEdit(s.id);
                  }}
                >
                  <Input
                    autoFocus
                    value={draftTitle}
                    onChange={(e) => setDraftTitle(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Escape') onCancelEdit();
                    }}
                    className="h-8"
                    aria-label="Session title"
                  />
                  <Button
                    type="submit"
                    size="icon"
                    variant="ghost"
                    className="h-8 w-8 shrink-0"
                    disabled={renamePending}
                    aria-label="Save title"
                  >
                    <Check className="h-4 w-4" />
                  </Button>
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    className="h-8 w-8 shrink-0"
                    onClick={onCancelEdit}
                    aria-label="Cancel rename"
                  >
                    <X className="h-4 w-4" />
                  </Button>
                </form>
              ) : (
                <Link to={`/chat/${s.id}`} className="block min-w-0">
                  {/* A div, not a <p>: the Badge renders a <div>, which is
                      invalid (and a hydration error) nested inside a <p>. */}
                  <div className="flex items-center gap-2 truncate font-medium">
                    <span className="truncate hover:underline">{sessionLabel(s)}</span>
                    {!isOwner && (
                      <Badge variant="outline" className="shrink-0">
                        <Eye className="mr-1 h-3 w-3" aria-hidden="true" />
                        Read-only
                      </Badge>
                    )}
                    {isIncidentMaster && (
                      <Badge
                        variant="outline"
                        className="shrink-0 border-amber-400 bg-amber-100 font-semibold text-amber-900 dark:border-amber-600 dark:bg-amber-950 dark:text-amber-200"
                      >
                        <AlertTriangle className="mr-1 h-3 w-3" aria-hidden="true" />
                        Incident Session
                      </Badge>
                    )}
                    {isLinked && (
                      <Badge
                        variant="outline"
                        className="shrink-0 border-amber-300 text-amber-900 dark:border-amber-700 dark:text-amber-200"
                      >
                        <AlertTriangle className="mr-1 h-3 w-3" aria-hidden="true" />
                        Linked to incident
                      </Badge>
                    )}
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {!isOwner && `owned by ${formatOwner(s.shared_by)} · `}
                    {formatDistanceToNow(new Date(activity), { addSuffix: true })}
                    {' · '}
                    {s.message_count} {s.message_count === 1 ? 'message' : 'messages'}
                  </p>
                </Link>
              )}
            </div>
            {!isEditing && isOwner && (
              <div className="flex shrink-0 items-center gap-1">
                {declareMode && !isIncidentMaster && (
                  <Button
                    size="sm"
                    variant="outline"
                    className="border-amber-300 text-amber-900 dark:border-amber-700 dark:text-amber-200"
                    onClick={() => onPromote(s.id)}
                    disabled={promotePending}
                  >
                    <AlertTriangle className="mr-1 h-3 w-3" />
                    Promote to incident
                  </Button>
                )}
                <Button
                  size="icon"
                  variant="ghost"
                  className="h-8 w-8"
                  onClick={() => onStartEdit(s)}
                  aria-label="Rename session"
                >
                  <Pencil className="h-4 w-4" />
                </Button>
                <Button
                  size="icon"
                  variant="ghost"
                  className="h-8 w-8 text-muted-foreground hover:text-destructive"
                  onClick={() => onDelete(s)}
                  aria-label="Delete session"
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            )}
          </li>
        );
      })}
    </ul>
  );
}
