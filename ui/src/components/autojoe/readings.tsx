import { useEffect, useRef } from 'react';
import type { ReactElement, ReactNode } from 'react';
import { Link } from 'react-router';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { evidenceHref } from '@/lib/findingLinks';
import type { AutoRun, AutoRunSelection, AutoSchedule } from '@/api/types';

// A reading is a rendering of the record and the page derives nothing
// (autojoe-ro-run-visibility § Settled 2): every line below is a field the API
// serves, shown as served. No health summary, no last-run badge inferred from
// the findings set, no next-due computed from cadence and last-fired — the
// schedule record carries next_run_at and the reading shows that.

// when renders a timestamp the record carries, as the browser's locale shows
// it, with the raw value on hover. An unparseable value is shown raw.
function when(iso: string): ReactElement {
  const d = new Date(iso);
  const text = Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
  return (
    <time dateTime={iso} title={iso}>
      {text}
    </time>
  );
}

function list(items: string[] | null | undefined): string {
  return items && items.length > 0 ? items.join(', ') : 'none';
}

// selection renders the scope as recorded at install — the selection as typed
// — which is what a schedule carries; a run shows its resolved set instead.
function selection(sel: AutoRunSelection): string {
  const parts: string[] = [];
  if (sel.components && sel.components.length > 0)
    parts.push(`components ${sel.components.join(', ')}`);
  if (sel.zones && sel.zones.length > 0) parts.push(`zones ${sel.zones.join(', ')}`);
  return parts.length > 0 ? parts.join('; ') : 'none';
}

const STATE_VARIANT: Record<string, 'default' | 'success' | 'destructive' | 'secondary'> = {
  running: 'default',
  completed: 'success',
  failed: 'destructive',
};

function Field({
  label,
  testId,
  children,
}: {
  label: string;
  testId: string;
  children: ReactNode;
}) {
  return (
    <p className="text-xs text-muted-foreground">
      <span className="font-medium text-foreground">{label}:</span>{' '}
      <span data-testid={testId}>{children}</span>
    </p>
  );
}

interface RunReadingProps {
  run: AutoRun;
  // marked is the run the Findings page's run pointer landed on (?run=); the
  // reading is outlined and scrolled into view once.
  marked?: boolean;
}

export function RunReading({ run, marked = false }: RunReadingProps) {
  const ref = useRef<HTMLLIElement>(null);
  useEffect(() => {
    if (marked) ref.current?.scrollIntoView?.({ block: 'center' });
  }, [marked]);
  return (
    <li
      ref={ref}
      className={`rounded-md border p-3 text-sm ${marked ? 'ring-2 ring-primary' : ''}`}
      data-testid="run-reading"
      data-run-id={run.id}
      data-marked={marked ? 'true' : undefined}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-xs" data-testid="run-id">
          {run.id}
        </span>
        <Badge variant={STATE_VARIANT[run.state] ?? 'secondary'} data-testid="run-state">
          {run.state}
        </Badge>
        <span className="text-xs text-muted-foreground">
          stop reason: <span data-testid="run-stop-reason">{run.stop_reason ?? 'none'}</span>
        </span>
      </div>
      <Field label="Activator" testId="run-activator">
        {run.activator}
      </Field>
      <Field label="Principal" testId="run-principal">
        {run.principal}
      </Field>
      <Field label="Scope (resolved)" testId="run-scope">
        {list(run.resolved_components)}
      </Field>
      <Field label="Covered" testId="run-covered">
        {list(run.covered)}
      </Field>
      <Field label="Not covered" testId="run-not-covered">
        {list(run.not_covered)}
      </Field>
      <Field label="Budget" testId="run-budget">
        {run.tokens_used} of {run.token_ceiling} tokens; wall clock {run.wall_clock_seconds}s;
        report {run.report_tokens} of {run.report_reserve_tokens} reserved tokens
      </Field>
      <Field label="Started" testId="run-started">
        {when(run.started_at)}
      </Field>
      <Field label="Ended" testId="run-ended">
        {run.ended_at ? when(run.ended_at) : 'not yet'}
      </Field>
      {run.error && (
        <Field label="Error" testId="run-error">
          {run.error}
        </Field>
      )}
      <p className="mt-1 text-xs">
        {run.session_id ? (
          <Link
            to={evidenceHref(run.session_id)}
            className="text-primary underline-offset-2 hover:underline"
            data-testid="run-session-link"
          >
            session
          </Link>
        ) : (
          <span className="text-muted-foreground" data-testid="run-no-session">
            no session
          </span>
        )}
        {run.finding_count !== undefined && (
          <>
            {' · '}
            <Link
              to="/findings"
              className="text-primary underline-offset-2 hover:underline"
              data-testid="run-findings-link"
            >
              findings ({run.finding_count})
            </Link>
          </>
        )}
      </p>
    </li>
  );
}

interface ScheduleReadingProps {
  schedule: AutoSchedule;
  // onRemove is present for an admin and absent — not disabled — for anyone
  // else, so a reader cannot mistake a missing grant for a broken control.
  onRemove?: (schedule: AutoSchedule) => void;
}

export function ScheduleReading({ schedule: s, onRemove }: ScheduleReadingProps) {
  return (
    <li
      className="rounded-md border p-3 text-sm"
      data-testid="schedule-reading"
      data-schedule-id={s.id}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-xs" data-testid="schedule-id">
          {s.id}
        </span>
        {onRemove && (
          <Button
            variant="outline"
            size="sm"
            className="ml-auto"
            onClick={() => onRemove(s)}
            data-testid="schedule-remove"
          >
            Remove
          </Button>
        )}
      </div>
      <Field label="Scope" testId="schedule-scope">
        {selection(s.selection)}
      </Field>
      <Field label="Cadence" testId="schedule-cadence">
        every {s.interval_seconds}s
      </Field>
      <Field label="Budget" testId="schedule-budget">
        {s.token_ceiling} tokens; wall clock {s.wall_clock_seconds}s; report reserve{' '}
        {s.report_reserve_tokens} tokens
      </Field>
      <Field label="Installed by" testId="schedule-installer">
        {s.installed_by}
      </Field>
      <Field label="Installed" testId="schedule-installed-at">
        {when(s.installed_at)}
      </Field>
      <Field label="Last fired" testId="schedule-last-fired">
        {s.last_run_at ? when(s.last_run_at) : 'never'}
      </Field>
      <Field label="Last outcome" testId="schedule-last-outcome">
        {s.last_outcome ?? 'none'}
      </Field>
      <Field label="Next due" testId="schedule-next-due">
        {when(s.next_run_at)}
      </Field>
    </li>
  );
}
