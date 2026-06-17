import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
  fetchPromotionRequirements,
  fetchPromotionCandidates,
  promoteComponent,
} from './components';

// Stub global fetch with a single JSON response. The client fns run their Zod
// parse over whatever the stub returns, so these tests cover both the request
// wiring and the schema parse at the boundary.
function stubFetchOnce(body: unknown, init: { ok?: boolean; status?: number } = {}) {
  const fetchMock = vi.fn().mockResolvedValue({
    ok: init.ok ?? true,
    status: init.status ?? 200,
    json: () => Promise.resolve(body),
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

describe('promotion client fns', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('fetchPromotionRequirements parses a wired requirements shape', async () => {
    stubFetchOnce({
      type: 'github',
      wired: true,
      kind: 'static',
      locator_fields: [{ name: 'env_var', required: true }],
      constraints: [],
    });
    const r = await fetchPromotionRequirements('prod-github');
    expect(r.wired).toBe(true);
    if (r.wired) expect(r.kind).toBe('static');
  });

  it('fetchPromotionRequirements parses an unwired shape', async () => {
    stubFetchOnce({ type: 'webhook', wired: false, armable_types: ['github'] });
    const r = await fetchPromotionRequirements('a-webhook');
    expect(r.wired).toBe(false);
    if (!r.wired) expect(r.armable_types).toEqual(['github']);
  });

  it('fetchPromotionCandidates parses a static candidate set', async () => {
    const fetchMock = stubFetchOnce({
      type: 'github',
      wired: true,
      kind: 'static',
      prefix: 'JOE_GITHUB_',
      applicable: true,
      candidates: [{ label: 'PROD', env_var_name: 'JOE_GITHUB_PROD' }],
    });
    const r = await fetchPromotionCandidates('prod-github');
    expect(r.wired).toBe(true);
    if (r.wired) expect(r.candidates[0].env_var_name).toBe('JOE_GITHUB_PROD');
    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('/api/v1/components/prod-github/promotion-candidates');
  });

  it('promoteComponent posts a static reference and parses the outcome', async () => {
    const fetchMock = stubFetchOnce({
      component_id: 'prod-github',
      type: 'github',
      provider: 'static',
      armed: true,
      rearm: false,
    });
    const res = await promoteComponent('prod-github', {
      credential_provider: 'static',
      env_var: 'JOE_GITHUB_PROD',
    });
    expect(res.armed).toBe(true);

    // The body carries ONLY the reference — never an inline `value` secret.
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const sent = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(sent).toEqual({ credential_provider: 'static', env_var: 'JOE_GITHUB_PROD' });
    expect(sent).not.toHaveProperty('value');
    expect(init.method).toBe('POST');
  });

  it('promoteComponent posts a kubeconfig-exec reference', async () => {
    const fetchMock = stubFetchOnce({
      component_id: 'prod-k8s',
      type: 'kubernetes',
      provider: 'kubeconfig-exec',
      armed: true,
      rearm: true,
    });
    const res = await promoteComponent('prod-k8s', {
      credential_provider: 'kubeconfig-exec',
      kubeconfig: '/etc/joe/kubeconfig',
      in_cluster: true,
    });
    expect(res.rearm).toBe(true);
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const sent = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(sent).toEqual({
      credential_provider: 'kubeconfig-exec',
      kubeconfig: '/etc/joe/kubeconfig',
      in_cluster: true,
    });
    expect(sent).not.toHaveProperty('value');
  });
});
