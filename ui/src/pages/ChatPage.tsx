import { useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useMutation } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Header } from '@/components/layout/Header';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { ChatWindow } from '@/components/chat/ChatWindow';
import { ZeroZoneEmptyState } from '@/components/chat/ZeroZoneEmptyState';
import { EmptyState } from '@/components/common/EmptyState';
import { useQueryClient } from '@tanstack/react-query';
import { useChat } from '@/hooks/useChat';
import { useSession } from '@/hooks/useSession';
import { useAuth } from '@/auth/AuthContext';
import { useRegime } from '@/hooks/useRegime';
import { loadLastSession, saveLastSession, clearLastSession } from '@/lib/lastSession';
import { updateSessionTitle, linkSessionToIncident, promoteSessionToIncident } from '@/api/chat';
import { ApiRequestError } from '@/api/client';
import {
  Plus,
  Link as LinkIcon,
  AlertTriangle,
  Pencil,
  Check,
  X,
  FileQuestion,
} from 'lucide-react';

export function ChatPage() {
  const { sessionId: routeSessionId } = useParams<{ sessionId?: string }>();
  const chat = useChat(routeSessionId);
  const { principal, rbacEnabled, isAdmin, zones } = useAuth();
  const navigate = useNavigate();
  const qc = useQueryClient();

  // The id of the session actually in view — either the one from the URL or
  // the one useChat lazily created on the first message of a fresh chat. The
  // sharing controls key off this, not the route param.
  const activeSessionId = chat.sessionId;

  // Keep the address bar in sync with the session in view. A fresh /chat mints a
  // session on the first message; reflect its id in the URL (replace, so the
  // blank /chat does not linger in history) so a refresh or shared link reopens
  // the same session. Clearing the session (New Session) returns to /chat.
  useEffect(() => {
    const inUrl = routeSessionId ?? null;
    if (activeSessionId === inUrl) return;
    navigate(activeSessionId ? `/chat/${activeSessionId}` : '/chat', { replace: true });
  }, [activeSessionId, routeSessionId, navigate]);

  // Reopen the last session viewed this tab whenever we land on a bare /chat —
  // the sidebar "Chat" link, a refresh, or any return without a session in the
  // URL. This is reactive, NOT mount-only: navigating /chat/{id} → /chat reuses
  // this component (no remount), and the user expects "Chat" to bring them back
  // to their session, not a blank one. "New Session" is the one intentional
  // blank: it sets suppressRestore so this effect doesn't bounce it back.
  // restoredId remembers what we reopened so the recovery effect can tell a dead
  // *restored* id apart from a session the user actively opened.
  const restoredId = useRef<string | null>(null);
  const suppressRestore = useRef(false);
  useEffect(() => {
    if (routeSessionId != null) return; // already on a session URL
    if (activeSessionId != null) return; // a session is in view (or being created)
    if (suppressRestore.current) {
      suppressRestore.current = false; // honor one explicit "New Session", then resume
      return;
    }
    const last = loadLastSession();
    if (last) {
      restoredId.current = last;
      navigate(`/chat/${last}`, { replace: true });
    }
  }, [routeSessionId, activeSessionId, navigate]);

  // Start a fresh chat: suppress the next restore so we stay on a blank /chat
  // instead of immediately reopening the last session.
  const handleNewSession = () => {
    suppressRestore.current = true;
    chat.startNewSession();
  };

  // Remember the session in view so the next return to /chat reopens it.
  useEffect(() => {
    if (activeSessionId) saveLastSession(activeSessionId);
  }, [activeSessionId]);

  // Session metadata + explicit lifecycle status, owned by useSession (header,
  // sharing controls, read-only viewer, and the title auto-update all derive
  // from this one seam). locallyCreated marks an id this tab just minted so a
  // transient read-after-write 404 on it is treated as benign, never a dead
  // state. Ownership is a POSITIVE signal (status === 'owner', i.e. the server
  // returned read_only=false) so anything unconfirmed fails CLOSED.
  const { status, session, isOwner, isReader, isLinkedToIncident, applyUpdate } = useSession(
    activeSessionId,
    { locallyCreated: activeSessionId != null && activeSessionId === chat.locallyCreatedId }
  );
  const readOnly = isReader;
  const sessionGone = status === 'gone';

  // If a session we *restored* turns out to be gone (deleted since), forget it
  // and fall back to a blank chat rather than stranding the user on a dead URL.
  // Driven by the explicit status, and the locallyCreated exemption keeps it
  // from tripping on a freshly-created session.
  useEffect(() => {
    if (activeSessionId != null && activeSessionId === restoredId.current && status === 'gone') {
      restoredId.current = null;
      clearLastSession();
      navigate('/chat', { replace: true });
    }
  }, [status, activeSessionId, navigate]);

  // The app-wide regime drives the "attach to incident" affordance: a chat can
  // only be linked while an incident is active (the server 409s otherwise).
  const { data: regime } = useRegime();
  const incidentActive = regime?.mode === 'incident';

  // Each mutation takes the id to act on as a variable (captured at click time)
  // and, on success, hands the full response to applyUpdate, which writes it to
  // the cache for the session it ACTUALLY changed (updated.id), not the session
  // currently in view — so a rename that resolves after the user navigated away
  // never stamps its result onto a different session.
  const linkIncidentMut = useMutation({
    mutationFn: (id: string) => linkSessionToIncident(id),
    onSuccess: (updated) => {
      applyUpdate(updated);
      toast.success('Session linked to the active incident');
    },
    onError: (e: Error) => toast.error(e.message),
  });

  // Secondary promote-this-session-to-incident affordance (§12.10): the chat-view
  // entry point to the one promote-in-place transition (§12.3). It promotes THIS
  // session into the incident master. Authorized by the regime-control zone
  // server-side; a 409 means an incident is already active.
  const promoteIncidentMut = useMutation({
    mutationFn: (id: string) => promoteSessionToIncident(id),
    onSuccess: () => {
      toast.success('Incident declared on this session');
      void qc.invalidateQueries({ queryKey: ['regime'] });
      if (activeSessionId) void qc.invalidateQueries({ queryKey: ['session', activeSessionId] });
      void qc.invalidateQueries({ queryKey: ['sessions'] });
    },
    onError: (e: Error) =>
      toast.error(
        e instanceof ApiRequestError && e.status === 409
          ? 'An incident is already active — resolve it before declaring another.'
          : e.message
      ),
  });

  // Inline title editing in the header (the same rename available on the
  // Sessions list, brought to the chat itself). Owner-only; the draft is local
  // so the background auto-title poll never clobbers what the user is typing.
  const [isEditingTitle, setIsEditingTitle] = useState(false);
  const [draftTitle, setDraftTitle] = useState('');

  const renameMut = useMutation({
    mutationFn: ({ id, title }: { id: string; title: string }) => updateSessionTitle(id, title),
    onSuccess: (updated) => {
      applyUpdate(updated);
      setIsEditingTitle(false);
      toast.success('Session renamed');
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const startEditTitle = () => {
    setDraftTitle(session?.title ?? '');
    setIsEditingTitle(true);
  };

  const commitTitle = () => {
    const title = draftTitle.trim();
    if (!title) {
      toast.error('Title must not be empty');
      return;
    }
    if (title === (session?.title ?? '')) {
      setIsEditingTitle(false);
      return;
    }
    if (!activeSessionId) return;
    renameMut.mutate({ id: activeSessionId, title });
  };

  const copyLink = () => {
    if (!activeSessionId) return;
    const url = `${window.location.origin}/chat/${activeSessionId}`;
    void navigator.clipboard.writeText(url).then(
      () => toast.success('Link copied'),
      () => toast.error('Could not copy link')
    );
  };

  // A zero-zone, non-admin user with RBAC enabled would 403 on every message.
  // Replace the chat surface with an explanation rather than a doomed input.
  // (Auth-disabled local dev has rbacEnabled=false and is unaffected; an admin
  // always reaches every zone so never hits this even with no grants.) A
  // read-only public viewer is exempt — reading shared content needs no zone.
  const accessPending = rbacEnabled && !isAdmin && zones.length === 0 && !readOnly;

  // Owner sharing controls appear only for a session we own (status === 'owner',
  // i.e. the server returned read_only=false). A non-owner public viewer gets a
  // read-only badge instead. The same gate guards inline title editing — it fails
  // closed for a non-owner or any session whose ownership is not yet confirmed.
  const showOwnerControls = isOwner;

  // The header title. claude.ai-style: it shows the session's own title once it
  // has one, else a "New chat" placeholder (swapped live by useSession's bounded
  // title-await — no raw-first-words flash). The owner can rename it inline. h1
  // only accepts phrasing content, so the editor is spans/input/buttons.
  const titleNode = isEditingTitle ? (
    <span className="flex items-center gap-2">
      <Input
        autoFocus
        value={draftTitle}
        onChange={(e) => setDraftTitle(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            commitTitle();
          } else if (e.key === 'Escape') {
            setIsEditingTitle(false);
          }
        }}
        className="h-8 w-72 text-base font-normal"
        aria-label="Session title"
      />
      <Button
        size="icon"
        variant="ghost"
        className="h-8 w-8 shrink-0"
        onClick={commitTitle}
        disabled={renameMut.isPending}
        aria-label="Save title"
      >
        <Check className="h-4 w-4" />
      </Button>
      <Button
        size="icon"
        variant="ghost"
        className="h-8 w-8 shrink-0"
        onClick={() => setIsEditingTitle(false)}
        aria-label="Cancel rename"
      >
        <X className="h-4 w-4" />
      </Button>
    </span>
  ) : (
    <span className="flex items-center gap-2">
      <span className="truncate">{session?.title ?? 'New chat'}</span>
      {showOwnerControls && (
        <Button
          size="icon"
          variant="ghost"
          className="h-7 w-7 shrink-0 text-muted-foreground"
          onClick={startEditTitle}
          aria-label="Rename session"
        >
          <Pencil className="h-3.5 w-3.5" />
        </Button>
      )}
    </span>
  );

  return (
    <div className="flex h-screen flex-col">
      <Header
        title={titleNode}
        actions={
          <div className="flex items-center gap-2">
            {readOnly && <Badge variant="secondary">Read-only</Badge>}
            {isLinkedToIncident && (
              <Badge
                variant="outline"
                className="border-amber-300 text-amber-900 dark:border-amber-700 dark:text-amber-200"
              >
                <AlertTriangle className="mr-1 h-3 w-3" aria-hidden="true" />
                Linked to incident
              </Badge>
            )}
            {showOwnerControls && (
              <>
                {incidentActive && !isLinkedToIncident && (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => activeSessionId && linkIncidentMut.mutate(activeSessionId)}
                    disabled={linkIncidentMut.isPending}
                  >
                    <AlertTriangle className="mr-1 h-3 w-3" />
                    Attach to incident
                  </Button>
                )}
                {!incidentActive && activeSessionId && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="border-amber-300 text-amber-900 dark:border-amber-700 dark:text-amber-200"
                    onClick={() => promoteIncidentMut.mutate(activeSessionId)}
                    disabled={promoteIncidentMut.isPending}
                  >
                    <AlertTriangle className="mr-1 h-3 w-3" />
                    Declare incident
                  </Button>
                )}
                {/* Every session is readable by anyone in the org, so the owner
                    can always share its link. */}
                <Button variant="outline" size="sm" onClick={copyLink}>
                  <LinkIcon className="mr-1 h-3 w-3" />
                  Copy link
                </Button>
              </>
            )}
            <Button variant="outline" size="sm" onClick={handleNewSession}>
              <Plus className="mr-1 h-3 w-3" />
              New Session
            </Button>
          </div>
        }
      />
      {isLinkedToIncident && incidentActive && (
        <IncidentParticipants
          creator={isOwner ? (principal ?? 'you') : session?.shared_by}
          captain={regime?.declaredByPrincipal ?? null}
        />
      )}
      <div className="flex-1 overflow-hidden">
        {accessPending ? (
          <ZeroZoneEmptyState principal={principal} />
        ) : sessionGone ? (
          <EmptyState
            icon={FileQuestion}
            title="Session not found"
            description="This chat doesn’t exist anymore — it may have been deleted by its owner."
            action={{ label: 'Start a new chat', onClick: handleNewSession }}
          />
        ) : (
          <ChatWindow
            items={chat.messages}
            isSending={chat.isSending}
            onSend={(msg) => {
              void chat.send(msg);
            }}
            readOnly={readOnly}
          />
        )}
      </div>
    </div>
  );
}

// formatPrincipal renders a principal ("user:alice@example.com") as the bare
// identity, with a stable fallback so a missing value never renders blank.
function formatPrincipal(principal?: string | null): string {
  if (!principal) return 'unknown';
  return principal.replace(/^user:/, '');
}

// IncidentParticipants renders the §12.3 creator-vs-captain distinction for a
// session participating in an incident. The two are SEPARATE principals by
// design: the creator is the session's original owner (creator_principal), the
// captain is the declarer/incident lead — they may differ and must never be
// conflated. Creator comes from the session (the caller when they own it, else
// shared_by); captain comes from the active regime's declarer.
function IncidentParticipants({
  creator,
  captain,
}: {
  creator?: string | null;
  captain: string | null;
}) {
  return (
    <div className="flex items-center gap-4 border-b bg-muted/40 px-6 py-1.5 text-xs text-muted-foreground">
      <span>
        Created by <span className="font-medium text-foreground">{formatPrincipal(creator)}</span>
      </span>
      <span aria-hidden="true">·</span>
      <span>
        Incident captain{' '}
        <span className="font-medium text-foreground">{formatPrincipal(captain)}</span>
      </span>
    </div>
  );
}
