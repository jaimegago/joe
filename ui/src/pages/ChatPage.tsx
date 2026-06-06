import { useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Header } from '@/components/layout/Header';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { ChatWindow } from '@/components/chat/ChatWindow';
import { ZeroZoneEmptyState } from '@/components/chat/ZeroZoneEmptyState';
import { useChat } from '@/hooks/useChat';
import { useAuth } from '@/auth/AuthContext';
import { useRegime } from '@/hooks/useRegime';
import { ApiRequestError } from '@/api/client';
import { loadLastSession, saveLastSession, clearLastSession } from '@/lib/lastSession';
import {
  fetchSession,
  updateSessionTitle,
  updateSessionVisibility,
  linkSessionToIncident,
} from '@/api/chat';
import { Plus, Globe, Lock, Link as LinkIcon, AlertTriangle, Pencil, Check, X } from 'lucide-react';

export function ChatPage() {
  const { sessionId: routeSessionId } = useParams<{ sessionId?: string }>();
  const chat = useChat(routeSessionId);
  const { principal, rbacEnabled, isAdmin, zones } = useAuth();
  const qc = useQueryClient();
  const navigate = useNavigate();

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

  // Reopen the last session viewed this tab when landing on a bare /chat (the
  // sidebar "Chat" link, or any return to the page without a session in the
  // URL). Mount-only via the ref: "New Session" deliberately clears to /chat
  // while the page stays mounted, so it must not be bounced back to the old one.
  // restoredId remembers what we reopened so the recovery effect below can tell
  // a dead *restored* id apart from the session the user is actively in.
  const didRestore = useRef(false);
  const restoredId = useRef<string | null>(null);
  useEffect(() => {
    if (didRestore.current) return;
    didRestore.current = true;
    if (routeSessionId) return;
    const last = loadLastSession();
    if (last) {
      restoredId.current = last;
      navigate(`/chat/${last}`, { replace: true });
    }
  }, [routeSessionId, navigate]);

  // Remember the session in view so the next return to /chat reopens it.
  useEffect(() => {
    if (activeSessionId) saveLastSession(activeSessionId);
  }, [activeSessionId]);

  // Session metadata (visibility + read_only + title) for the header, sharing
  // controls and read-only viewer. Loaded only once a session exists. The
  // server titles a fresh session asynchronously from its first message
  // (claude.ai-style), so while the session is still untitled we poll briefly to
  // swap the "New chat" placeholder for the real title without a refresh —
  // stopping as soon as a title lands, and giving up after the backend's title
  // window (~30s, ~10 polls) so an LLM-less/failed title never polls forever.
  const sessionQ = useQuery({
    queryKey: ['session', activeSessionId],
    queryFn: () => fetchSession(activeSessionId!),
    enabled: activeSessionId != null,
    refetchInterval: (q) => (q.state.data?.title || q.state.dataUpdateCount > 10 ? false : 3000),
  });
  const session = sessionQ.data;

  // If the session we *restored* turns out to be gone (deleted since, or owned
  // by a different user now in this tab), forget it and fall back to a blank
  // chat rather than stranding the user in a dead session. Deliberately narrow:
  // only a 404 on the restored id triggers it — never a freshly-created session,
  // a directly-opened URL, or a transient/5xx error, any of which would
  // otherwise wrongly reset a live chat (and its pending auto-title) to "New
  // chat".
  const sessionErr = sessionQ.error;
  useEffect(() => {
    if (
      activeSessionId != null &&
      activeSessionId === restoredId.current &&
      sessionErr instanceof ApiRequestError &&
      sessionErr.status === 404
    ) {
      restoredId.current = null;
      clearLastSession();
      navigate('/chat', { replace: true });
    }
  }, [sessionErr, activeSessionId, navigate]);

  // When the async title finally lands, refresh the browse list / dashboard so
  // their "New chat" entries pick it up live too (the header reads it directly).
  useEffect(() => {
    if (session?.title) void qc.invalidateQueries({ queryKey: ['sessions'] });
  }, [session?.title, qc]);
  const readOnly = session?.read_only === true;
  const isPublic = session?.visibility === 'public';
  const isLinkedToIncident = session?.linked_incident_id != null;

  // The app-wide regime drives the "attach to incident" affordance: a chat can
  // only be linked while an incident is active (the server 409s otherwise).
  const { data: regime } = useRegime();
  const incidentActive = regime?.mode === 'incident';

  const visibilityMut = useMutation({
    mutationFn: (visibility: 'private' | 'public') =>
      updateSessionVisibility(activeSessionId!, visibility),
    onSuccess: (updated) => {
      qc.setQueryData(['session', activeSessionId], updated);
      void qc.invalidateQueries({ queryKey: ['sessions'] });
      toast.success(
        updated.visibility === 'public' ? 'Session is now public' : 'Session is now private'
      );
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const linkIncidentMut = useMutation({
    mutationFn: () => linkSessionToIncident(activeSessionId!),
    onSuccess: (updated) => {
      qc.setQueryData(['session', activeSessionId], updated);
      void qc.invalidateQueries({ queryKey: ['sessions'] });
      toast.success('Session linked to the active incident');
    },
    onError: (e: Error) => toast.error(e.message),
  });

  // Inline title editing in the header (the same rename available on the
  // Sessions list, brought to the chat itself). Owner-only; the draft is local
  // so the background auto-title poll never clobbers what the user is typing.
  const [isEditingTitle, setIsEditingTitle] = useState(false);
  const [draftTitle, setDraftTitle] = useState('');

  const renameMut = useMutation({
    mutationFn: (title: string) => updateSessionTitle(activeSessionId!, title),
    onSuccess: (updated) => {
      qc.setQueryData(['session', activeSessionId], updated);
      void qc.invalidateQueries({ queryKey: ['sessions'] });
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
    renameMut.mutate(title);
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

  // Owner sharing controls appear only for an existing session we own (the
  // server returns read_only=false). A non-owner public viewer gets a
  // read-only badge instead. The same gate guards inline title editing.
  const showOwnerControls = activeSessionId != null && session != null && !readOnly;

  // The header title. claude.ai-style: it shows the session's own title once it
  // has one, else a "New chat" placeholder (swapped live by the poll above — no
  // raw-first-words flash). The owner can rename it inline. h1 only accepts
  // phrasing content, so the editor is spans/input/buttons — no block/form.
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
                    onClick={() => linkIncidentMut.mutate()}
                    disabled={linkIncidentMut.isPending}
                  >
                    <AlertTriangle className="mr-1 h-3 w-3" />
                    Attach to incident
                  </Button>
                )}
                {isPublic && (
                  <Button variant="outline" size="sm" onClick={copyLink}>
                    <LinkIcon className="mr-1 h-3 w-3" />
                    Copy link
                  </Button>
                )}
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => visibilityMut.mutate(isPublic ? 'private' : 'public')}
                  disabled={visibilityMut.isPending}
                >
                  {isPublic ? (
                    <>
                      <Lock className="mr-1 h-3 w-3" />
                      Make private
                    </>
                  ) : (
                    <>
                      <Globe className="mr-1 h-3 w-3" />
                      Make public
                    </>
                  )}
                </Button>
              </>
            )}
            {!readOnly && (
              <Button variant="outline" size="sm" onClick={chat.startNewSession}>
                <Plus className="mr-1 h-3 w-3" />
                New Session
              </Button>
            )}
          </div>
        }
      />
      <div className="flex-1 overflow-hidden">
        {accessPending ? (
          <ZeroZoneEmptyState principal={principal} />
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
