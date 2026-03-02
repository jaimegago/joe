import { MessageList } from './MessageList';
import { ChatInput } from './ChatInput';
import { LoadingSpinner } from '@/components/common/LoadingSpinner';
import type { ChatMessage } from '@/api/types';

interface ChatWindowProps {
  messages: ChatMessage[];
  isSending: boolean;
  sendError?: string | null;
  onSend: (message: string) => void;
}

export function ChatWindow({ messages, isSending, sendError, onSend }: ChatWindowProps) {
  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 overflow-y-auto overflow-x-hidden px-4">
        <MessageList messages={messages} />
        {isSending && (
          <div className="flex items-center gap-2 py-3 px-1 text-sm text-muted-foreground">
            <LoadingSpinner size="sm" />
            <span>Joe is thinking…</span>
          </div>
        )}
      </div>
      {sendError && (
        <div className="border-t bg-destructive/10 px-4 py-2 text-sm text-destructive">
          {sendError}
        </div>
      )}
      <ChatInput onSend={onSend} disabled={isSending} />
    </div>
  );
}
