import { MessageList } from './MessageList';
import { ChatInput } from './ChatInput';
import type { DisplayItem } from '@/hooks/useChat';

interface ChatWindowProps {
  items: DisplayItem[];
  isSending: boolean;
  onSend: (message: string) => void;
}

export function ChatWindow({ items, isSending, onSend }: ChatWindowProps) {
  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 overflow-y-auto overflow-x-hidden px-4">
        {/* Progress (steps, tool activity, token counter) and failures now
            render inline within the streaming assistant turn — no separate
            "thinking" spinner or error banner. */}
        <MessageList items={items} />
      </div>
      <ChatInput onSend={onSend} disabled={isSending} />
    </div>
  );
}
