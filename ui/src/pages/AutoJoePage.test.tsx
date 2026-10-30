import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { createWrapper } from '@/test/utils';
import { AutoJoePage } from './AutoJoePage';
import {
  fetchAutoRuns,
  fetchAutoSchedules,
  triggerAutoRun,
  installAutoSchedule,
  removeAutoSchedule,
} from '@/api/autoRuns';
import { fetchComponents } from '@/api/components';
import { ApiRequestError } from '@/api/client';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import type { AutoRun, AutoSchedule, Component } from '@/api/types';

vi.mock('@/api/autoRuns', () => ({
  fetchAutoRuns: vi.fn(),
  fetchAutoSchedules: vi.fn(),
  triggerAutoRun: vi.fn(),
  installAutoSchedule: vi.fn(),
  removeAutoSchedule: vi.fn(),
}));
vi.mock('@/api/components', () => ({ fetchComponents: vi.fn() }));
vi.mock('@/hooks/useCurrentUser', () => ({ useCurrentUser: vi.fn() }));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const mockRuns = vi.mocked(fetchAutoRuns);
const mockSchedules = vi.mocked(fetchAutoSchedules);
const mockTrigger = vi.mocked(triggerAutoRun);
const mockInstall = vi.mocked(installAutoSchedule);
const mockRemove = vi.mocked(removeAutoSchedule);
const mockComponents = vi.mocked(fetchComponents);
const mockUseCurrentUser = vi.mocked(useCurrentUser);

function setAdmin(isAdmin: boolean) {
  mockUseCurrentUser.mockReturnValue({ data: { is_admin: isAdmin } } as ReturnType<
    typeof useCurrentUser
  >);
}

const completed: AutoRun = {
  id: 'run-done',
  activator: 'svc:ops',
  principal: 'svc:agent:core',
  selection: { components: ['repo', 'idle'] },
  scope_key: 'c:idle,repo',
  resolved_components: ['idle', 'repo'],
  token_ceiling: 5000,
  wall_clock_seconds: 1200,
  report_reserve_tokens: 500,
  state: 'completed',
  covered: ['repo'],
  not_covered: ['idle'],
  tokens_used: 15,
  report_tokens: 0,
  session_id: 'sess-done',
  started_at: '2026-10-29T10:00:00Z',
  ended_at: '2026-10-29T10:00:05Z',
  finding_count: 2,
};

const interrupted: AutoRun = {
  id: 'run-interrupted',
  activator: 'schedule:sch-1',
  principal: 'svc:agent:core',
  selection: { components: ['repo'] },
  scope_key: 'c:repo',
  resolved_components: ['repo'],
  token_ceiling: 5000,
  wall_clock_seconds: 1200,
  report_reserve_tokens: 500,
  state: 'failed',
  stop_reason: 'interrupted',
  covered: [],
  not_covered: ['repo'],
  tokens_used: 0,
  report_tokens: 0,
  error: 'process exited while the run was live',
  started_at: '2026-10-29T09:00:00Z',
  ended_at: '2026-10-29T09:30:00Z',
};

const schedule: AutoSchedule = {
  id: 'sch-1',
  selection: { components: ['repo'] },
  scope_key: 'c:repo',
  interval_seconds: 60,
  token_ceiling: 5000,
  wall_clock_seconds: 1200,
  report_reserve_tokens: 500,
  installed_by: 'svc:ops',
  installed_at: '2026-10-29T09:00:00Z',
  next_run_at: '2026-10-29T10:02:00Z',
  last_run_at: '2026-10-29T10:01:00Z',
  last_outcome: 'refused: a run on this scope was still live',
};

const components: Component[] = [
  { id: 'repo', type: 'git', name: 'repo', status: 'connected', armed: true, zone: 'unassigned' },
  {
    id: 'idle',
    type: 'kubernetes',
    name: 'idle',
    status: 'unknown',
    armed: false,
    zone: 'unassigned',
  },
] as Component[];

function renderPage(path = '/auto-joe') {
  const { Wrapper, qc } = createWrapper();
  render(
    <Wrapper>
      <MemoryRouter initialEntries={[path]}>
        <AutoJoePage />
      </MemoryRouter>
    </Wrapper>
  );
  return qc;
}

describe('AutoJoePage', () => {
  beforeEach(() => {
    mockRuns.mockReset();
    mockSchedules.mockReset();
    mockTrigger.mockReset();
    mockInstall.mockReset();
    mockRemove.mockReset();
    mockComponents.mockReset();
    mockComponents.mockResolvedValue(components);
    mockRuns.mockResolvedValue([completed, interrupted]);
    mockSchedules.mockResolvedValue([schedule]);
  });

  it('renders each run and schedule record as served, and marks the run named by ?run=', async () => {
    setAdmin(false);
    renderPage('/auto-joe?run=run-interrupted');

    const done = (await screen.findAllByTestId('run-reading')).find(
      (el) => el.getAttribute('data-run-id') === 'run-done'
    )!;
    expect(within(done).getByTestId('run-activator')).toHaveTextContent('svc:ops');
    expect(within(done).getByTestId('run-principal')).toHaveTextContent('svc:agent:core');
    // The resolved set, not the selection as typed.
    expect(within(done).getByTestId('run-scope')).toHaveTextContent('idle, repo');
    expect(within(done).getByTestId('run-state')).toHaveTextContent('completed');
    expect(within(done).getByTestId('run-stop-reason')).toHaveTextContent('none');
    expect(within(done).getByTestId('run-covered')).toHaveTextContent('repo');
    expect(within(done).getByTestId('run-not-covered')).toHaveTextContent('idle');
    expect(within(done).getByTestId('run-budget')).toHaveTextContent(
      '15 of 5000 tokens; wall clock 1200s; report 0 of 500 reserved tokens'
    );
    expect(within(done).getByTestId('run-session-link')).toHaveAttribute('href', '/chat/sess-done');
    expect(within(done).getByTestId('run-findings-link')).toHaveAttribute('href', '/findings');
    expect(within(done).getByTestId('run-findings-link')).toHaveTextContent('findings (2)');
    expect(done).not.toHaveAttribute('data-marked');

    // A run with no session and no finding set: no links, the error shown, and
    // the interrupted stop reason — the case the findings view cannot show.
    const failed = screen
      .getAllByTestId('run-reading')
      .find((el) => el.getAttribute('data-run-id') === 'run-interrupted')!;
    expect(within(failed).getByTestId('run-state')).toHaveTextContent('failed');
    expect(within(failed).getByTestId('run-stop-reason')).toHaveTextContent('interrupted');
    expect(within(failed).getByTestId('run-activator')).toHaveTextContent('schedule:sch-1');
    expect(within(failed).getByTestId('run-error')).toHaveTextContent(
      'process exited while the run was live'
    );
    expect(within(failed).queryByTestId('run-session-link')).not.toBeInTheDocument();
    expect(within(failed).queryByTestId('run-findings-link')).not.toBeInTheDocument();
    expect(failed).toHaveAttribute('data-marked', 'true');

    // The schedule record, including a refused fire and the served next-due.
    const sch = screen.getByTestId('schedule-reading');
    expect(within(sch).getByTestId('schedule-scope')).toHaveTextContent('components repo');
    expect(within(sch).getByTestId('schedule-cadence')).toHaveTextContent('every 60s');
    expect(within(sch).getByTestId('schedule-installer')).toHaveTextContent('svc:ops');
    expect(within(sch).getByTestId('schedule-last-outcome')).toHaveTextContent(
      'refused: a run on this scope was still live'
    );
    expect(within(sch).getByTestId('schedule-next-due').querySelector('time')).toHaveAttribute(
      'datetime',
      '2026-10-29T10:02:00Z'
    );
    expect(within(sch).getByTestId('schedule-last-fired').querySelector('time')).toHaveAttribute(
      'datetime',
      '2026-10-29T10:01:00Z'
    );
  });

  it('shows no write affordance to a reader', async () => {
    setAdmin(false);
    renderPage();
    await screen.findByTestId('schedule-reading');
    expect(screen.queryByTestId('install-form')).not.toBeInTheDocument();
    expect(screen.queryByTestId('trigger-form')).not.toBeInTheDocument();
    expect(screen.queryByTestId('schedule-remove')).not.toBeInTheDocument();
    expect(mockComponents).not.toHaveBeenCalled();
  });

  it('lets an admin trigger a run over a picked scope, and re-reads the lists', async () => {
    setAdmin(true);
    mockTrigger.mockResolvedValue({ ...completed, id: 'run-new', state: 'running' });
    renderPage();
    const form = await screen.findByTestId('trigger-form');
    const user = userEvent.setup();
    await user.click(await within(form).findByTestId('trigger-scope-repo'));
    await user.click(within(form).getByTestId('trigger-submit'));
    await waitFor(() => expect(mockTrigger).toHaveBeenCalledWith({ components: ['repo'] }));
    // Re-read after the act: one fetch on mount, one after.
    await waitFor(() => expect(mockRuns).toHaveBeenCalledTimes(2));
    expect(within(form).queryByTestId('trigger-error')).not.toBeInTheDocument();
  });

  it('shows a refused trigger with the reason the API gave, at the form', async () => {
    setAdmin(true);
    mockTrigger.mockRejectedValue(
      new ApiRequestError(409, 'a run on this scope is live; the trigger is refused, not queued')
    );
    renderPage();
    const form = await screen.findByTestId('trigger-form');
    const user = userEvent.setup();
    await user.click(await within(form).findByTestId('trigger-scope-repo'));
    await user.click(within(form).getByTestId('trigger-submit'));
    expect(await within(form).findByTestId('trigger-error')).toHaveTextContent(
      'a run on this scope is live; the trigger is refused, not queued'
    );
    // The install form's error slot is its own.
    expect(screen.queryByTestId('install-error')).not.toBeInTheDocument();
    await waitFor(() => expect(mockRuns).toHaveBeenCalledTimes(2));
  });

  it('lets an admin install a schedule with a scope and a cadence, and remove one', async () => {
    setAdmin(true);
    mockInstall.mockResolvedValue({ ...schedule, id: 'sch-2' });
    mockRemove.mockResolvedValue(undefined);
    renderPage();
    const form = await screen.findByTestId('install-form');
    const user = userEvent.setup();
    await user.click(await within(form).findByTestId('install-scope-idle'));
    const interval = within(form).getByTestId('install-interval');
    await user.clear(interval);
    await user.type(interval, '5');
    await user.click(within(form).getByTestId('install-submit'));
    await waitFor(() => expect(mockInstall).toHaveBeenCalledWith({ components: ['idle'] }, 5));

    await user.click(screen.getByTestId('schedule-remove'));
    await user.click(await screen.findByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(mockRemove).toHaveBeenCalledWith('sch-1'));
    await waitFor(() => expect(mockSchedules).toHaveBeenCalledTimes(3));
  });

  it('shows the empty lists before any schedule or run exists', async () => {
    setAdmin(false);
    mockRuns.mockResolvedValue([]);
    mockSchedules.mockResolvedValue([]);
    renderPage();
    expect(await screen.findByTestId('runs-empty')).toBeInTheDocument();
    expect(screen.getByTestId('schedules-empty')).toBeInTheDocument();
  });

  it('renders the shared error panel when a list fails', async () => {
    setAdmin(false);
    mockRuns.mockRejectedValue(new Error('boom'));
    renderPage();
    expect(await screen.findByText("Couldn't load runs")).toBeInTheDocument();
  });
});
