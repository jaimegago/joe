import { useParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Header } from '@/components/layout/Header';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { ChatWindow } from '@/components/chat/ChatWindow';
import { ZeroZoneEmptyState } from '@/components/chat/ZeroZoneEmptyState';
import { useChat } from '@/hooks/useChat';
import { useAuth } from '@/auth/AuthContext';
import { useRegime } from '@/hooks/useRegime';
import { fetchSession, updateSessionVisibility, linkSessionToIncident } from '@/api/chat';
import { Plus, Globe, Lock, Link as LinkIcon, AlertTriangle } from 'lucide-react';

export function ChatPage() {
  const { sessionId: routeSessionId } = useParams<{ sessionId?: string }>();
  const chat = useChat(routeSessionId);
  const { principal, rbacEnabled, isAdmin, zones } = useAuth();
  const qc = useQueryClient();

  // The id of the session actually in view — either the one from the URL or
  // the one useChat lazily created on the first message of a fresh chat. The
  // sharing controls key off this, not the route param.
  const activeSessionId = chat.sessionId;

  // Session metadata (visibility + read_only) for the sharing controls and the
  // read-only viewer. Loaded only once a session exists.
  const sessionQ = useQuery({
    queryKey: ['session', activeSessionId],
    queryFn: () => fetchSession(activeSessionId!),
    enabled: activeSessionId != null,
  });
  const session = sessionQ.data;
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
  // read-only badge instead.
  const showOwnerControls = activeSessionId != null && session != null && !readOnly;

  return (
    <div className="flex h-screen flex-col">
      <Header
        title="Chat with Joe"
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
