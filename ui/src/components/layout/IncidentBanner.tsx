import { AlertTriangle, ArrowRight } from 'lucide-react';
import { Link } from 'react-router-dom';
import { useRegime } from '@/hooks/useRegime';

// IncidentBanner is the app-shell-wide active-incident indicator
// (DECISIONS.md D-0014, incident banner). It polls GET /api/v1/regime via
// useRegime and renders a top-of-page alert only while the mode is "incident".
// It is an app-shell concern, not chat content — it never enters the
// chat-history snapshot.
export function IncidentBanner() {
  const { data } = useRegime();
  if (data?.mode !== 'incident') return null;

  const captain = data.declaredByPrincipal ?? 'an operator';
  const when = formatDeclaredAt(data.declaredAt);

  return (
    <div
      role="alert"
      className="flex items-center gap-2 border-b border-amber-300 bg-amber-50 px-4 py-2 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950 dark:text-amber-200"
    >
      <AlertTriangle className="h-4 w-4 shrink-0" aria-hidden="true" />
      <span>
        {/* The declarer is the incident CAPTAIN, who may differ from a given
            session's creator (§12.3) — label it as such so the two roles are
            never conflated. */}
        <span className="font-semibold">System is in incident mode</span> — incident captain{' '}
        {captain}
        {when ? ` (declared at ${when})` : ''}. Writes may be blocked.
      </span>
      {/* Deep-link to the incident master session, where the captain/admin finds
          the lifecycle and resolve controls. The id is only present while an
          incident is active (the regime read resolves the active master). */}
      {data.incidentSessionId && (
        <Link
          to={`/chat/${data.incidentSessionId}`}
          className="ml-auto inline-flex shrink-0 items-center gap-1 rounded border border-amber-400 px-2 py-0.5 font-medium hover:bg-amber-100 dark:border-amber-600 dark:hover:bg-amber-900"
        >
          Go to incident
          <ArrowRight className="h-3 w-3" aria-hidden="true" />
        </Link>
      )}
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
