import { useParams } from 'react-router-dom';
import { Header } from '@/components/layout/Header';
import { Button } from '@/components/ui/button';
import { ChatWindow } from '@/components/chat/ChatWindow';
import { useChat } from '@/hooks/useChat';
import { Plus } from 'lucide-react';

export function ChatPage() {
  const { sessionId } = useParams<{ sessionId?: string }>();
  const chat = useChat(sessionId);

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
        <ChatWindow
          items={chat.messages}
          isSending={chat.isSending}
          onSend={(msg) => { void chat.send(msg); }}
        />
      </div>
    </div>
  );
}
