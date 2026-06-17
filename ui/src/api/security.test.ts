import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fetchReadPromotions, setReadPromotion } from './security';

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

describe('read-promotions client fns', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('fetchReadPromotions unwraps the full enum and parses each row', async () => {
    const fetchMock = stubFetchOnce({
      read_promotions: [
        { component_type: 'github', enabled: true },
        { component_type: 'kubernetes', enabled: false },
      ],
      count: 2,
    });
    const rows = await fetchReadPromotions();
    expect(rows).toEqual([
      { component_type: 'github', enabled: true },
      { component_type: 'kubernetes', enabled: false },
    ]);

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('/api/v1/admin/read-promotions');
    expect(init.method ?? 'GET').toBe('GET');
  });

  it('setReadPromotion posts the type+enabled body and parses the echo', async () => {
    const fetchMock = stubFetchOnce({ component_type: 'github', enabled: true });
    const res = await setReadPromotion('github', true);
    expect(res).toEqual({ component_type: 'github', enabled: true });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('/api/v1/admin/read-promotions');
    expect(init.method).toBe('POST');
    const sent = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(sent).toEqual({ component_type: 'github', enabled: true });
  });

  it('setReadPromotion carries the off state through the body', async () => {
    const fetchMock = stubFetchOnce({ component_type: 'kubernetes', enabled: false });
    const res = await setReadPromotion('kubernetes', false);
    expect(res.enabled).toBe(false);
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const sent = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(sent).toEqual({ component_type: 'kubernetes', enabled: false });
  });
});
