import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { formatDistanceToNow } from 'date-fns';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { sessionLabel, formatOwner } from '@/lib/sessionLabel';
import type { Session } from '@/api/types';
import { MessageSquare, Pencil, Trash2, Check, X, AlertTriangle, Eye } from 'lucide-react';

// SessionRowActions is the shared mutator/edit-state bundle threaded from the
// page into every row, whether it renders in the conversation list or as a
// master/child inside an incident cluster.
export interface SessionRowActions {
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

export interface SessionRowProps extends SessionRowActions {
  session: Session;
  // declareMode shows the per-row "Promote to incident" affordance. Passed only
  // by the conversation list — incident-cluster rows (already masters/children)
  // never offer it.
  declareMode?: boolean;
  // badge is the incident-context chip rendered beside the title: the master's
  // lifecycle-state badge, or a child's "Linked to «master»" badge. Supplied by
  // the cluster renderer; absent for a plain conversation row.
  badge?: ReactNode;
  // icon overrides the default leading icon (a child uses the linked-incident
  // glyph; a conversation uses the message glyph).
  icon?: ReactNode;
  className?: string;
}

// SessionRow renders one session as a row: a title that links into the chat,
// owner controls (rename inline / move-to-trash / declare-mode promote) when the
// caller owns it, a read-only badge + owner attribution when it does not, and an
// optional incident-context badge slot. It is the single row primitive shared by
// the conversation view and the incident clusters — the incident vs linked
// badges that used to be baked in here now arrive via the `badge` slot so each
// view composes them coherently.
export function SessionRow({
  session: s,
  declareMode = false,
  badge,
  icon,
  className,
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
}: SessionRowProps) {
  const isEditing = editingId === s.id;
  // Owner-only controls gate on the positive signal (read_only === false) and
  // fail closed if the flag is ever absent — see SessionSchema.
  const isOwner = s.read_only === false;
  const isIncidentMaster = s.type === 'incident';
  const activity = s.last_activity_at ?? s.started_at;

  return (
    <div className={cn('flex items-center gap-3 px-4 py-3', className)}>
      {icon ?? <MessageSquare className="h-4 w-4 shrink-0 text-muted-foreground" />}
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
          // The badges are SIBLINGS of the title link, not nested inside it: a
          // child's "Linked to «master»" badge is itself a navigable link, and
          // an anchor nested in an anchor is invalid HTML.
          <>
            <div className="flex items-center gap-2 font-medium">
              <Link to={`/chat/${s.id}`} className="truncate hover:underline">
                {sessionLabel(s)}
              </Link>
              {!isOwner && (
                <Badge variant="outline" className="shrink-0">
                  <Eye className="mr-1 h-3 w-3" aria-hidden="true" />
                  Read-only
                </Badge>
              )}
              {badge}
            </div>
            <Link to={`/chat/${s.id}`} className="block">
              <p className="text-xs text-muted-foreground">
                {!isOwner && `owned by ${formatOwner(s.shared_by)} · `}
                {formatDistanceToNow(new Date(activity), { addSuffix: true })}
                {' · '}
                {s.message_count} {s.message_count === 1 ? 'message' : 'messages'}
              </p>
            </Link>
          </>
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
    </div>
  );
}
