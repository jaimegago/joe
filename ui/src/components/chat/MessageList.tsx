import { useEffect, useRef } from 'react';
import type { DisplayItem } from '@/hooks/useChat';
import { UserBubble } from './UserBubble';
import { AssistantTurnView } from './AssistantTurnView';
import { MessageSquare } from 'lucide-react';

interface MessageListProps {
  items: DisplayItem[];
}

export function MessageList({ items }: MessageListProps) {
  const bottomRef = useRef<HTMLDivElement>(null);

  // Scroll to the bottom as the transcript grows or a live turn updates.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [items]);

  if (items.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 text-center text-muted-foreground">
        <MessageSquare className="h-10 w-10 opacity-30" />
        <p className="text-sm">Ask Joe anything about your infrastructure</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4 py-4">
      {items.map((item) =>
        item.kind === 'user' ? (
          <UserBubble key={item.id} content={item.content} createdAt={item.createdAt} />
        ) : (
          <AssistantTurnView key={item.id} turn={item.turn} />
        ),
      )}
      <div ref={bottomRef} />
    </div>
  );
}
