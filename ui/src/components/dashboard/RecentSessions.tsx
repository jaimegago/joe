import type { Session } from '@/api/types';
import { formatDistanceToNow } from 'date-fns';
import { MessageSquare } from 'lucide-react';

interface RecentSessionsProps {
  sessions: Session[];
}

export function RecentSessions({ sessions }: RecentSessionsProps) {
  if (sessions.length === 0) {
    return <p className="text-sm text-muted-foreground">No recent sessions</p>;
  }
  return (
    <ul className="space-y-2">
      {sessions.map((s) => (
        <li key={s.id} className="flex items-start gap-2 text-sm">
          <MessageSquare className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0">
            <p className="truncate">{s.summary ?? 'Session'}</p>
            <p className="text-xs text-muted-foreground">
              {formatDistanceToNow(new Date(s.started_at), { addSuffix: true })}
              {' · '}
              {s.messageCount ?? 0} messages
            </p>
          </div>
        </li>
      ))}
    </ul>
  );
}
