import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fetchRunFindings, fetchProposedAdmissions } from './runFindings';

function stubFetchOnce(body: unknown, init: { ok?: boolean; status?: number } = {}) {
  const fetchMock = vi.fn().mockResolvedValue({
    ok: init.ok ?? true,
    status: init.status ?? 200,
    json: () => Promise.resolve(body),
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

const finding = {
  id: 'row-1',
  key: 'k1',
  run_id: 'run-1',
  scope_key: 'scope',
  session_id: 'sess-1',
  type: 'issue',
  reference: 'self_report',
  subject_node_id: 'default/svc-a',
  subject_component_id: 'k8s',
  claim: '0 of 1 replicas available',
  consequence: 'unavailable',
  band: 'P0',
  reach_count: 3,
  weight: 1,
  derivation: {
    consequence: 'unavailable',
    evidence_line: 'Available=False',
    reach: { dependents: null, components: null, count: 3 },
    weight: 1,
    base_band: 'P1',
    band: 'P0',
    movement: 'raised',
  },
  label: 'new',
  evidence_session_id: 'sess-1',
  established_at: '2026-10-28T12:00:00Z',
  created_at: '2026-10-28T12:00:00Z',
};

describe('run findings client fns', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('fetchRunFindings parses the latest view and hits the bare endpoint', async () => {
    const fetchMock = stubFetchOnce({
      runs: [
        {
          run_id: 'run-1',
          scope_key: 'scope',
          session_id: 'sess-1',
          created_at: '2026-10-28T12:00:00Z',
          finding_count: 1,
        },
      ],
      findings: [finding],
      count: 1,
    });
    const r = await fetchRunFindings();
    expect(r.count).toBe(1);
    expect(r.findings[0].band).toBe('P0');
    expect(r.findings[0].label).toBe('new');
    expect(r.findings[0].proposal).toBeUndefined();
    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toMatch(/\/api\/v1\/run-findings$/);
  });

  it('fetchRunFindings with a run id filters on the query string', async () => {
    const fetchMock = stubFetchOnce({ runs: [], findings: [], count: 0 });
    await fetchRunFindings('run x');
    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('/api/v1/run-findings?run_id=run%20x');
  });

  it('rejects a band outside the fixed scale at the boundary', async () => {
    stubFetchOnce({ runs: [], findings: [{ ...finding, band: 'P4' }], count: 1 });
    await expect(fetchRunFindings()).rejects.toThrow();
  });

  it('fetchProposedAdmissions hits the admissions endpoint and parses a proposal', async () => {
    const fetchMock = stubFetchOnce({
      runs: [],
      findings: [
        {
          ...finding,
          type: 'graph_update',
          consequence: undefined,
          band: 'P3',
          proposal: {
            shape: 'operator_action',
            target: { node_id: 'default/svc-a', component_id: 'k8s', system_id: 'default/svc-a' },
            base: 'no redis component registered',
            change: 'register the redis instance',
            expected_effect: 'the stores_in edge resolves',
            limits: 'type may be blocked',
            status: 'unverified',
            executable: false,
          },
        },
      ],
      count: 1,
    });
    const r = await fetchProposedAdmissions();
    expect(r.findings[0].proposal?.shape).toBe('operator_action');
    expect(r.findings[0].proposal?.executable).toBe(false);
    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('/api/v1/run-findings/admissions');
  });
});
