import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import { formatDistanceToNow } from 'date-fns';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { fetchSessions, updateSessionTitle, deleteSession } from '@/api/chat';
import type { Session } from '@/api/types';
import { MessageSquare, Plus, Pencil, Trash2, Check, X } from 'lucide-react';

// Browse limit for the sessions list — generous enough to show a working
// history without paging, which is a later nicety.
const SESSIONS_LIMIT = 50;

function sessionLabel(s: Session): string {
  return s.title ?? s.summary ?? 'Untitled session';
}

export function SessionsPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [draftTitle, setDraftTitle] = useState('');
  const [pendingDelete, setPendingDelete] = useState<Session | null>(null);

  const sessionsQ = useQuery({
    queryKey: ['sessions', SESSIONS_LIMIT],
    queryFn: () => fetchSessions(SESSIONS_LIMIT),
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

  const deleteMut = useMutation({
    mutationFn: (id: string) => deleteSession(id),
    onSuccess: () => {
      toast.success('Session deleted');
      setPendingDelete(null);
      void qc.invalidateQueries({ queryKey: ['sessions'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

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

  if (sessionsQ.isLoading) return <LoadingPage />;

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
        {sessions.length === 0 ? (
          <EmptyState
            icon={MessageSquare}
            title="No sessions yet"
            description="Your chat sessions with Joe will appear here."
          />
        ) : (
          <ul className="divide-y rounded-md border">
            {sessions.map((s) => {
              const isEditing = editingId === s.id;
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
                          commitEdit(s.id);
                        }}
                      >
                        <Input
                          autoFocus
                          value={draftTitle}
                          onChange={(e) => setDraftTitle(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === 'Escape') setEditingId(null);
                          }}
                          className="h-8"
                          aria-label="Session title"
                        />
                        <Button
                          type="submit"
                          size="icon"
                          variant="ghost"
                          className="h-8 w-8 shrink-0"
                          disabled={renameMut.isPending}
                          aria-label="Save title"
                        >
                          <Check className="h-4 w-4" />
                        </Button>
                        <Button
                          type="button"
                          size="icon"
                          variant="ghost"
                          className="h-8 w-8 shrink-0"
                          onClick={() => setEditingId(null)}
                          aria-label="Cancel rename"
                        >
                          <X className="h-4 w-4" />
                        </Button>
                      </form>
                    ) : (
                      <Link to={`/chat/${s.id}`} className="block min-w-0">
                        <p className="flex items-center gap-2 truncate font-medium">
                          <span className="truncate hover:underline">{sessionLabel(s)}</span>
                          {s.visibility === 'public' && (
                            <Badge variant="secondary" className="shrink-0">
                              Public
                            </Badge>
                          )}
                        </p>
                        <p className="text-xs text-muted-foreground">
                          {formatDistanceToNow(new Date(activity), { addSuffix: true })}
                          {' · '}
                          {s.message_count} {s.message_count === 1 ? 'message' : 'messages'}
                        </p>
                      </Link>
                    )}
                  </div>
                  {!isEditing && (
                    <div className="flex shrink-0 items-center gap-1">
                      <Button
                        size="icon"
                        variant="ghost"
                        className="h-8 w-8"
                        onClick={() => startEdit(s)}
                        aria-label="Rename session"
                      >
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button
                        size="icon"
                        variant="ghost"
                        className="h-8 w-8 text-muted-foreground hover:text-destructive"
                        onClick={() => setPendingDelete(s)}
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
        )}

        <ConfirmDialog
          open={pendingDelete !== null}
          onOpenChange={(open) => {
            if (!open) setPendingDelete(null);
          }}
          title="Delete session"
          description={`This permanently deletes "${pendingDelete ? sessionLabel(pendingDelete) : ''}" and its messages. This cannot be undone.`}
          confirmLabel="Delete"
          variant="destructive"
          onConfirm={() => pendingDelete && deleteMut.mutate(pendingDelete.id)}
        />
      </PageContainer>
    </>
  );
}
