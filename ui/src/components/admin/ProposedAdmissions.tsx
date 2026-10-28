import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { FindingCard } from '@/components/findings/FindingCard';
import { useProposedAdmissions } from '@/hooks/useRunFindings';

// ProposedAdmissions is the separate proposed-admissions list a read-only Auto
// Joe run delivers apart from its ranked findings: graph updates — evidence of
// a system outside joe's remit, or a relationship no refresher derives — each
// carrying its evidence and an operator-action proposal to register a
// component or accept a relationship. It is placed here, where registration
// happens, and addressed to admins because registration is an admission
// (D-0029); the run applies nothing. A proposal naming a type the allow-list
// blocks says so in its limits, so it reads as information and not as an
// action.
export function ProposedAdmissions({ enabled }: { enabled: boolean }) {
  const q = useProposedAdmissions({ enabled });
  if (!enabled) return null;
  return (
    <Card className="mt-6" data-testid="proposed-admissions">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium">Proposed admissions</CardTitle>
      </CardHeader>
      <CardContent className="text-sm">
        {q.isLoading && <p className="text-muted-foreground">Loading…</p>}
        {q.isError && (
          <p className="text-muted-foreground">Couldn&apos;t load proposed admissions.</p>
        )}
        {q.data?.findings.length === 0 && (
          <p className="text-muted-foreground">
            None. The latest read-only Auto Joe run on every scope proposed no admission.
          </p>
        )}
        {q.data && q.data.findings.length > 0 && (
          <>
            <p className="mb-2 text-xs text-muted-foreground">
              From the latest run on each scope. Each is unverified; registering a component is your
              act, never the run&apos;s.
            </p>
            <ul className="space-y-2">
              {q.data.findings.map((f) => (
                <FindingCard key={f.id} finding={f} />
              ))}
            </ul>
          </>
        )}
      </CardContent>
    </Card>
  );
}
