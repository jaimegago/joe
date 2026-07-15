import { useEffect, useRef } from 'react';
import type { DisplayItem } from '@/hooks/useChat';
import { UserBubble } from './UserBubble';
import { AssistantTurnView } from './AssistantTurnView';
import { MessageSquare } from 'lucide-react';

interface MessageListProps {
  items: DisplayItem[];
  // onResend, when provided, lets a cancelled/failed assistant turn re-send its
  // paired user message as a new turn. Undefined in read-only views.
  onResend?: (text: string) => void;
}

// findScrollParent walks up from the list container to the nearest ancestor
// that actually scrolls (overflow-y auto/scroll with content taller than the
// box). The transcript itself does not scroll — an ancestor pane does — so the
// scroll listener has to attach there.
function findScrollParent(el: HTMLElement | null): HTMLElement | null {
  let node = el?.parentElement ?? null;
  while (node) {
    const overflowY = getComputedStyle(node).overflowY;
    if ((overflowY === 'auto' || overflowY === 'scroll') && node.scrollHeight > node.clientHeight) {
      return node;
    }
    node = node.parentElement;
  }
  return null;
}

// Distance from the bottom (px) within which the user is considered "pinned"
// to the tail and auto-scroll should follow new content. Beyond it, the user
// has scrolled up to read history and we must not yank them back down.
const NEAR_BOTTOM_THRESHOLD_PX = 120;

export function MessageList({ items, onResend }: MessageListProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  // Whether the user is currently pinned to the bottom. Starts true (a freshly
  // opened transcript scrolls to the latest turn) and flips as the user scrolls.
  const pinnedToBottom = useRef(true);

  // Track the scroll position on the nearest scrollable ancestor so we only
  // auto-scroll while the user is already near the bottom.
  useEffect(() => {
    const scroller = findScrollParent(containerRef.current);
    if (!scroller) return;
    const onScroll = () => {
      const distance = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;
      pinnedToBottom.current = distance <= NEAR_BOTTOM_THRESHOLD_PX;
    };
    onScroll();
    scroller.addEventListener('scroll', onScroll, { passive: true });
    return () => scroller.removeEventListener('scroll', onScroll);
  }, []);

  // Scroll to the bottom as the transcript grows or a live turn updates — but
  // only while the user is pinned to the tail, so scrolling up to read history
  // is never interrupted.
  useEffect(() => {
    if (pinnedToBottom.current) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
    }
  }, [items]);

  if (items.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 text-center text-muted-foreground">
        <MessageSquare className="h-10 w-10 opacity-30" />
        <p className="text-sm">Ask Joe anything about your infrastructure</p>
      </div>
    );
  }

  // Pair each assistant turn with the text of the user message that prompted it
  // (the nearest preceding user item) so a resend can re-send the original text.
  // Precomputed before render so the map callback stays pure.
  const pairedUserText = new Map<string, string>();
  {
    let lastUserText: string | null = null;
    for (const item of items) {
      if (item.kind === 'user') {
        lastUserText = item.content;
      } else if (lastUserText != null) {
        pairedUserText.set(item.id, lastUserText);
      }
    }
  }

  return (
    <div ref={containerRef} className="flex flex-col gap-4 py-4">
      {items.map((item) => {
        if (item.kind === 'user') {
          return <UserBubble key={item.id} content={item.content} createdAt={item.createdAt} />;
        }
        const userText = pairedUserText.get(item.id);
        return (
          <AssistantTurnView
            key={item.id}
            turn={item.turn}
            onResend={onResend && userText != null ? () => onResend(userText) : undefined}
          />
        );
      })}
      <div ref={bottomRef} />
    </div>
  );
}
