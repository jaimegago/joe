import { MessageList } from './MessageList';
import { ChatInput } from './ChatInput';
import type { DisplayItem } from '@/hooks/useChat';

interface ChatWindowProps {
  items: DisplayItem[];
  isSending: boolean;
  onSend: (message: string) => void;
  // readOnly renders the transcript without a composer — used for a non-owner
  // reading another principal's session (the team-public read model: any
  // authenticated principal may read any session, only the owner may write —
  // DESIGN-CHAT-SESSIONS.md §12.7).
  readOnly?: boolean;
}

export function ChatWindow({ items, isSending, onSend, readOnly = false }: ChatWindowProps) {
  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 overflow-y-auto overflow-x-hidden px-4">
        {/* Progress (steps, tool activity, token counter) and failures now
            render inline within the streaming assistant turn — no separate
            "thinking" spinner or error banner. */}
        <MessageList items={items} />
      </div>
      {readOnly ? (
        <div className="border-t px-4 py-3 text-center text-sm text-muted-foreground">
          You are viewing a shared session in read-only mode.
        </div>
      ) : (
        <ChatInput onSend={onSend} disabled={isSending} />
      )}
    </div>
  );
}
