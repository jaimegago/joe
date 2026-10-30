import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  fetchAutoRuns,
  fetchAutoRun,
  fetchAutoSchedules,
  triggerAutoRun,
  installAutoSchedule,
  removeAutoSchedule,
} from './autoRuns';

type FetchCall = [string, RequestInit];

function stubFetchOnce(body: unknown, init: { ok?: boolean; status?: number } = {}) {
  const fetchMock = vi
    .fn<(url: string, init: RequestInit) => Promise<unknown>>()
    .mockResolvedValue({
      ok: init.ok ?? true,
      status: init.status ?? 200,
      json: () => Promise.resolve(body),
    });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

const run = {
  id: 'run-1',
  activator: 'svc:ops',
  principal: 'svc:agent:core',
  selection: { components: ['repo'] },
  scope_key: 'c:repo',
  resolved_components: ['repo'],
  token_ceiling: 5000,
  wall_clock_seconds: 1200,
  report_reserve_tokens: 500,
  state: 'completed',
  covered: ['repo'],
  not_covered: [],
  tokens_used: 15,
  report_tokens: 0,
  session_id: 'sess-1',
  started_at: '2026-10-29T10:00:00Z',
  ended_at: '2026-10-29T10:00:05Z',
  finding_count: 2,
};

const schedule = {
  id: 'sch-1',
  selection: { components: ['repo'] },
  scope_key: 'c:repo',
  interval_seconds: 60,
  token_ceiling: 5000,
  wall_clock_seconds: 1200,
  report_reserve_tokens: 500,
  installed_by: 'svc:ops',
  installed_at: '2026-10-29T10:00:00Z',
  next_run_at: '2026-10-29T10:01:00Z',
};

describe('auto run client fns', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('fetchAutoRuns parses the list and tolerates a null list', async () => {
    const fetchMock = stubFetchOnce({ runs: [run], count: 1 });
    expect(await fetchAutoRuns()).toEqual([run]);
    expect(String(fetchMock.mock.calls[0][0])).toMatch(/\/api\/v1\/auto\/runs$/);
    stubFetchOnce({ runs: null, count: 0 });
    expect(await fetchAutoRuns()).toEqual([]);
  });

  it('fetchAutoRun reads one run by id, with omitted fields absent', async () => {
    const running: Record<string, unknown> = { ...run, state: 'running' };
    delete running.finding_count;
    delete running.ended_at;
    delete running.session_id;
    const fetchMock = stubFetchOnce(running);
    const r = await fetchAutoRun('run 1');
    expect(r.state).toBe('running');
    expect(r.finding_count).toBeUndefined();
    expect(r.session_id).toBeUndefined();
    expect(String(fetchMock.mock.calls[0][0])).toContain('/api/v1/auto/runs/run%201');
  });

  it('fetchAutoSchedules parses the list', async () => {
    stubFetchOnce({ schedules: [schedule], count: 1 });
    expect(await fetchAutoSchedules()).toEqual([schedule]);
  });

  it('triggerAutoRun posts a scope and nothing else', async () => {
    const fetchMock = stubFetchOnce({ ...run, state: 'running' }, { status: 202 });
    await triggerAutoRun({ components: ['repo'] });
    const [url, init] = fetchMock.mock.calls[0] as FetchCall;
    expect(String(url)).toMatch(/\/api\/v1\/auto\/runs$/);
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ scope: { components: ['repo'] } });
  });

  it('installAutoSchedule posts a scope and a cadence in minutes, no budget', async () => {
    const fetchMock = stubFetchOnce(schedule, { status: 201 });
    await installAutoSchedule({ components: ['repo'] }, 1);
    const [, init] = fetchMock.mock.calls[0] as FetchCall;
    expect(JSON.parse(init.body as string)).toEqual({
      scope: { components: ['repo'] },
      interval_minutes: 1,
    });
  });

  it('removeAutoSchedule deletes by id', async () => {
    const fetchMock = stubFetchOnce(undefined, { status: 204 });
    await removeAutoSchedule('sch 1');
    const [url, init] = fetchMock.mock.calls[0] as FetchCall;
    expect(String(url)).toContain('/api/v1/auto/schedules/sch%201');
    expect(init.method).toBe('DELETE');
  });

  it('surfaces the API message of a refusal', async () => {
    stubFetchOnce(
      {
        error: 'conflict',
        message: 'a run on this scope is live; the trigger is refused, not queued',
      },
      { ok: false, status: 409 }
    );
    await expect(triggerAutoRun({ components: ['repo'] })).rejects.toThrow(
      'a run on this scope is live; the trigger is refused, not queued'
    );
  });
});
