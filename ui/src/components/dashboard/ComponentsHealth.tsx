import type { Component } from '@/api/types';
import { STATUS_CONFIG } from '@/lib/constants';
import { cn } from '@/lib/utils';

interface ComponentsHealthProps {
  components: Component[];
}

export function ComponentsHealth({ components }: ComponentsHealthProps) {
  return (
    <div className="flex flex-wrap gap-3">
      {components.map((s) => {
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
