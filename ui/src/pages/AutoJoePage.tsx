import { useState } from 'react';
import { useSearchParams } from 'react-router';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { QueryError } from '@/components/common/QueryError';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { RunReading, ScheduleReading } from '@/components/autojoe/readings';
import { ScopeForm } from '@/components/autojoe/ScopeForm';
import { installAutoSchedule, removeAutoSchedule, triggerAutoRun } from '@/api/autoRuns';
import { useAutoRuns, useAutoSchedules } from '@/hooks/useAutoJoe';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { QUERY_KEYS } from '@/lib/queryKeys';
import type { AutoRunSelection, AutoSchedule } from '@/api/types';

// AutoJoePage is where a human operator reads back the schedules they have
// installed and the runs those schedules and their own triggers have caused
// (joe-pm threads/autojoe-ro-run-visibility § Settled). A page of its own,
// beside Findings: a run exists whether or not it produced findings — a failed
// run, one interrupted at boot, one stopped on budget with nothing to report,
// and a schedule fire refused because the scope was live all leave nothing in
// the findings view, and those are the cases an operator could not see.
//
// Readable by any authenticated principal, as the run surface's reads are.
// Two lists, schedules and runs, each a rendering of the record. The writes —
// install a schedule, remove one, trigger a run — are present for an admin
// and absent for anyone else, gated client-side on is_admin to match the
// server's requireAdmin, which remains the real enforcement.
export function AutoJoePage() {
  const qc = useQueryClient();
  const [params] = useSearchParams();
  const markedRun = params.get('run');
  const meQ = useCurrentUser();
  const isAdmin = meQ.data?.is_admin === true;
  const runsQ = useAutoRuns();
  const schedulesQ = useAutoSchedules();
  const [removeTarget, setRemoveTarget] = useState<AutoSchedule | null>(null);

  // After any act, refused or not, the lists are re-read so what the page
  // shows is the record's — never the page's own idea of what happened.
  const reread = () => {
    void qc.invalidateQueries({ queryKey: QUERY_KEYS.autoRuns });
    void qc.invalidateQueries({ queryKey: QUERY_KEYS.autoSchedules });
  };

  const triggerMut = useMutation({
    mutationFn: (scope: AutoRunSelection) => triggerAutoRun(scope),
    onSuccess: (run) => toast.success(`Run ${run.id} started`),
    onSettled: reread,
  });
  const installMut = useMutation({
    mutationFn: ({
      scope,
      intervalMinutes,
    }: {
      scope: AutoRunSelection;
      intervalMinutes: number;
    }) => installAutoSchedule(scope, intervalMinutes),
    onSuccess: (s) => toast.success(`Schedule ${s.id} installed`),
    onSettled: reread,
  });
  const removeMut = useMutation({
    mutationFn: (id: string) => removeAutoSchedule(id),
    onSuccess: () => toast.success('Schedule removed'),
    onError: (e: Error) => toast.error(e.message),
    onSettled: reread,
  });

  if (runsQ.isLoading || schedulesQ.isLoading) return <LoadingPage />;

  if (runsQ.isError || schedulesQ.isError) {
    const failed = runsQ.isError ? runsQ : schedulesQ;
    return (
      <>
        <Header title="Auto Joe" />
        <PageContainer>
          <QueryError
            error={failed.error}
            onRetry={() => {
              void runsQ.refetch();
              void schedulesQ.refetch();
            }}
            resourceLabel={runsQ.isError ? 'runs' : 'schedules'}
          />
        </PageContainer>
      </>
    );
  }

  const runs = runsQ.data ?? [];
  const schedules = schedulesQ.data ?? [];

  return (
    <>
      <Header title="Auto Joe" />
      <PageContainer className="space-y-8">
        <section aria-label="Schedules" data-testid="schedules">
          <h2 className="mb-2 text-sm font-semibold">
            Schedules{' '}
            <span className="font-normal text-muted-foreground">({schedules.length})</span>
          </h2>
          {isAdmin && (
            <ScopeForm
              kind="install"
              onSubmit={(scope, intervalMinutes) => installMut.mutate({ scope, intervalMinutes })}
              isPending={installMut.isPending}
              error={installMut.error?.message ?? null}
            />
          )}
          {schedules.length === 0 ? (
            <p className="text-sm text-muted-foreground" data-testid="schedules-empty">
              No schedules installed.
            </p>
          ) : (
            <ul className="space-y-2">
              {schedules.map((s) => (
                <ScheduleReading
                  key={s.id}
                  schedule={s}
                  onRemove={isAdmin ? setRemoveTarget : undefined}
                />
              ))}
            </ul>
          )}
        </section>

        <section aria-label="Runs" data-testid="runs">
          <h2 className="mb-2 text-sm font-semibold">
            Runs <span className="font-normal text-muted-foreground">({runs.length})</span>
          </h2>
          {isAdmin && (
            <ScopeForm
              kind="trigger"
              onSubmit={(scope) => triggerMut.mutate(scope)}
              isPending={triggerMut.isPending}
              error={triggerMut.error?.message ?? null}
            />
          )}
          {runs.length === 0 ? (
            <p className="text-sm text-muted-foreground" data-testid="runs-empty">
              No runs yet.
            </p>
          ) : (
            <ul className="space-y-2">
              {runs.map((r) => (
                <RunReading key={r.id} run={r} marked={r.id === markedRun} />
              ))}
            </ul>
          )}
        </section>
      </PageContainer>

      <ConfirmDialog
        open={removeTarget !== null}
        onOpenChange={(open) => {
          if (!open) setRemoveTarget(null);
        }}
        title="Remove schedule"
        description={
          removeTarget ? `Remove schedule ${removeTarget.id}? It will fire no further runs.` : ''
        }
        confirmLabel="Remove"
        variant="destructive"
        onConfirm={() => {
          if (removeTarget) removeMut.mutate(removeTarget.id);
        }}
      />
    </>
  );
}
