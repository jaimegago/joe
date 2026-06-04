import { useParams } from 'react-router-dom';
import { Header } from '@/components/layout/Header';
import { Button } from '@/components/ui/button';
import { ChatWindow } from '@/components/chat/ChatWindow';
import { ZeroZoneEmptyState } from '@/components/chat/ZeroZoneEmptyState';
import { useChat } from '@/hooks/useChat';
import { useAuth } from '@/auth/AuthContext';
import { Plus } from 'lucide-react';

export function ChatPage() {
  const { sessionId } = useParams<{ sessionId?: string }>();
  const chat = useChat(sessionId);
  const { principal, rbacEnabled, isAdmin, zones } = useAuth();

  // A zero-zone, non-admin user with RBAC enabled would 403 on every message.
  // Replace the chat surface with an explanation rather than a doomed input.
  // (Auth-disabled local dev has rbacEnabled=false and is unaffected; an admin
  // always reaches every zone so never hits this even with no grants.)
  const accessPending = rbacEnabled && !isAdmin && zones.length === 0;

  return (
    <div className="flex h-screen flex-col">
      <Header
        title="Chat with Joe"
        actions={
          <Button variant="outline" size="sm" onClick={chat.startNewSession}>
            <Plus className="mr-1 h-3 w-3" />
            New Session
          </Button>
        }
      />
      <div className="flex-1 overflow-hidden">
        {accessPending ? (
          <ZeroZoneEmptyState principal={principal} />
        ) : (
          <ChatWindow
            items={chat.messages}
            isSending={chat.isSending}
            onSend={(msg) => { void chat.send(msg); }}
          />
        )}
      </div>
    </div>
  );
}
