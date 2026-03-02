import type { Alert } from '@/api/types';
import { Badge } from '@/components/ui/badge';
import { SEVERITY_CONFIG } from '@/lib/constants';
import { formatDistanceToNow } from 'date-fns';

interface AlertsListProps {
  alerts: Alert[];
}

export function AlertsList({ alerts }: AlertsListProps) {
  if (alerts.length === 0) {
    return <p className="text-sm text-muted-foreground">No active alerts</p>;
  }
  return (
    <ul className="space-y-2">
      {alerts.slice(0, 10).map((alert) => {
        const sev = SEVERITY_CONFIG[alert.severity] ?? SEVERITY_CONFIG['info'];
        return (
          <li key={alert.id} className="flex items-start gap-2 text-sm">
            <Badge variant={sev.variant} className="mt-0.5 shrink-0">
              {alert.severity}
            </Badge>
            <div className="min-w-0">
              <p className="truncate">{alert.message}</p>
              <p className="text-xs text-muted-foreground">
                {alert.source} · {formatDistanceToNow(new Date(alert.timestamp), { addSuffix: true })}
              </p>
            </div>
          </li>
        );
      })}
    </ul>
  );
}
