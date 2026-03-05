import type { Source } from '@/api/types';
import { STATUS_CONFIG } from '@/lib/constants';
import { cn } from '@/lib/utils';

interface SourcesHealthProps {
  sources: Source[];
}

export function SourcesHealth({ sources }: SourcesHealthProps) {
  return (
    <div className="flex flex-wrap gap-3">
      {sources.map((s) => {
        const status = STATUS_CONFIG[s.status] ?? STATUS_CONFIG.unknown;
        return (
          <div key={s.id} className="flex items-center gap-1.5">
            <span
              className={cn('text-sm')}
              style={{ color: status.color }}
              title={status.label}
            >
              {status.dot}
            </span>
            <span className="text-sm text-muted-foreground">{s.id}</span>
          </div>
        );
      })}
    </div>
  );
}
