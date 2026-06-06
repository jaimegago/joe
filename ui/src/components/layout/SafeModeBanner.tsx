import { ShieldAlert } from 'lucide-react';
import { usePanicStatus } from '@/hooks/usePanicStatus';

// SafeModeBanner is the app-shell-wide active-safe-mode indicator, the panic-axis
// counterpart to IncidentBanner. It polls GET /api/v1/panic/status via
// usePanicStatus and renders a top-of-page alert only while safe mode is active.
//
// Safe mode and incident mode are independent, composable flags — both banners
// can show at once. When both are active this one renders ABOVE the incident
// banner (see AppShell) because safe mode is the more restrictive state: it
// blocks every write, not just non-captain writes. It is an app-shell concern,
// not chat content — it never enters the chat-history snapshot.
export function SafeModeBanner() {
  const { data } = usePanicStatus();
  if (!data?.safeMode) return null;

  const source = data.triggerSource ?? 'an operator';
  const reason = data.triggerReason;
  const when = formatTriggeredAt(data.triggeredAt);

  return (
    <div
      role="alert"
      className="flex items-center gap-2 border-b border-red-300 bg-red-50 px-4 py-2 text-sm text-red-900 dark:border-red-700 dark:bg-red-950 dark:text-red-200"
    >
      <ShieldAlert className="h-4 w-4 shrink-0" aria-hidden="true" />
      <span>
        <span className="font-semibold">System is in safe mode</span> — triggered by {source}
        {when ? ` at ${when}` : ''}
        {reason ? ` (${reason})` : ''}. Only read-only operations are permitted; run{' '}
        <code className="rounded bg-red-100 px-1 font-mono text-xs dark:bg-red-900">
          joe unlock
        </code>{' '}
        to resume writes.
      </span>
    </div>
  );
}

// formatTriggeredAt renders the triggered-at timestamp in the viewer's locale,
// falling back to empty (the banner omits the "at ..." clause) when absent or
// unparseable — the banner must never throw on a malformed timestamp.
function formatTriggeredAt(triggeredAt: string | null): string {
  if (!triggeredAt) return '';
  const d = new Date(triggeredAt);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString();
}
