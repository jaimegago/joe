import { Link } from 'react-router';
import { formatDistanceToNow } from 'date-fns';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { QueryError } from '@/components/common/QueryError';
import { FindingCard } from '@/components/findings/FindingCard';
import { evidenceHref } from '@/lib/findingLinks';
import { useRunFindings } from '@/hooks/useRunFindings';
import type { RunFinding, RunFindingBand } from '@/api/types';
import { ClipboardList } from 'lucide-react';

// The fixed scale, most severe first. The server computes the band; this page
// only groups by it.
const BANDS: RunFindingBand[] = ['P0', 'P1', 'P2', 'P3'];

// FindingsPage is the findings view by band (docs/backlog/autojoe-ro.md
// § Delivery): the latest read-only Auto Joe run's findings on every scope,
// each with its new/standing/resolved/unknown label, a link to its subject
// node and to its evidence. It reads real records through an endpoint of its
// own — the purpose-built replacement for the alerts widget D-0038 retired.
// Graph updates are delivered apart, to admins, on the Components page where
// registration happens.
export function FindingsPage() {
  const q = useRunFindings();

  if (q.isLoading) return <LoadingPage />;

  if (q.isError || !q.data) {
    return (
      <>
        <Header title="Findings" />
        <PageContainer>
          <QueryError error={q.error} onRetry={() => void q.refetch()} resourceLabel="findings" />
        </PageContainer>
      </>
    );
  }

  const { runs, findings } = q.data;
  const ranked = findings.filter((f) => f.type !== 'graph_update');
  const admissions = findings.length - ranked.length;

  if (runs.length === 0) {
    return (
      <>
        <Header title="Findings" />
        <PageContainer>
          <EmptyState
            icon={ClipboardList}
            title="No runs yet"
            description="Findings appear here once a read-only Auto Joe run has reported on a scope."
          />
        </PageContainer>
      </>
    );
  }

  const byBand = new Map<RunFindingBand, RunFinding[]>(BANDS.map((b) => [b, []]));
  for (const f of ranked) byBand.get(f.band)?.push(f);

  return (
    <>
      <Header title="Findings" />
      <PageContainer className="space-y-6">
        <section aria-label="Runs" className="text-sm text-muted-foreground">
          <ul className="space-y-1">
            {runs.map((r) => {
              const at = new Date(r.created_at);
              return (
                <li key={r.run_id} data-testid="run-row">
                  Run <span className="font-mono text-xs">{r.run_id}</span>
                  {!Number.isNaN(at.getTime()) && (
                    <> · {formatDistanceToNow(at, { addSuffix: true })}</>
                  )}{' '}
                  · {r.finding_count} finding{r.finding_count === 1 ? '' : 's'} ·{' '}
                  <Link
                    to={evidenceHref(r.session_id)}
                    className="text-primary underline-offset-2 hover:underline"
                  >
                    session
                  </Link>
                </li>
              );
            })}
          </ul>
          {admissions > 0 && (
            <p className="mt-2" data-testid="admissions-note">
              {admissions} proposed admission{admissions === 1 ? '' : 's'} listed for admins on the{' '}
              <Link to="/components" className="text-primary underline-offset-2 hover:underline">
                Components
              </Link>{' '}
              page.
            </p>
          )}
        </section>

        {ranked.length === 0 ? (
          <EmptyState
            icon={ClipboardList}
            title="No findings"
            description="The latest run on every scope established no issue and no risk."
          />
        ) : (
          BANDS.map((band) => {
            const inBand = byBand.get(band) ?? [];
            return (
              <section key={band} aria-label={`${band} findings`} data-testid={`band-${band}`}>
                <h2 className="mb-2 text-sm font-semibold">
                  {band}{' '}
                  <span className="font-normal text-muted-foreground">({inBand.length})</span>
                </h2>
                {inBand.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No {band} findings.</p>
                ) : (
                  <ul className="space-y-2">
                    {inBand.map((f) => (
                      <FindingCard key={f.id} finding={f} />
                    ))}
                  </ul>
                )}
              </section>
            );
          })
        )}
      </PageContainer>
    </>
  );
}
