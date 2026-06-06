import { AlertTriangle } from 'lucide-react';
import { useRegime } from '@/hooks/useRegime';

// IncidentBanner is the app-shell-wide active-incident indicator
// (DECISIONS.md D-0014, incident banner). It polls GET /api/v1/regime via
// useRegime and renders a top-of-page alert only while the mode is "incident".
// It is an app-shell concern, not chat content — it never enters the
// chat-history snapshot.
export function IncidentBanner() {
  const { data } = useRegime();
  if (data?.mode !== 'incident') return null;

  const who = data.declaredByPrincipal ?? 'an operator';
  const when = formatDeclaredAt(data.declaredAt);

  return (
    <div
      role="alert"
      className="flex items-center gap-2 border-b border-amber-300 bg-amber-50 px-4 py-2 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950 dark:text-amber-200"
    >
      <AlertTriangle className="h-4 w-4 shrink-0" aria-hidden="true" />
      <span>
        <span className="font-semibold">System is in incident mode</span> — declared by {who}
        {when ? ` at ${when}` : ''}. Writes may be blocked.
      </span>
    </div>
  );
}

// formatDeclaredAt renders the declared-at timestamp in the viewer's locale,
// falling back to empty (the banner omits the "at ..." clause) when absent or
// unparseable — the banner must never throw on a malformed timestamp.
function formatDeclaredAt(declaredAt: string | null): string {
  if (!declaredAt) return '';
  const d = new Date(declaredAt);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString();
}
