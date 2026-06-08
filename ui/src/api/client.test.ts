import { describe, it, expect, vi, beforeEach } from 'vitest';

// Re-import to get a fresh instance per test via module isolation
async function getClient() {
  vi.resetModules();
  const mod = await import('./client');
  return mod.apiClient;
}

describe('ApiClient', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('throws an Error when response is not ok', async () => {
    const client = await getClient();
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 404,
        json: () => Promise.resolve({ message: 'not found' }),
      })
    );

    await expect(client.get('/missing')).rejects.toThrow('not found');
  });

  it('falls back to generic message when error body is not JSON', async () => {
    const client = await getClient();
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: () => Promise.reject(new Error('bad json')),
      })
    );

    await expect(client.get('/broken')).rejects.toThrow('API request failed');
  });

  it('returns parsed JSON on success', async () => {
    const client = await getClient();
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ hello: 'world' }),
      })
    );

    const result = await client.get<{ hello: string }>('/ok');
    expect(result).toEqual({ hello: 'world' });
  });

  it('returns undefined on 204 No Content without parsing JSON', async () => {
    const client = await getClient();
    const json = vi.fn(() => Promise.reject(new Error('should not be called')));
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 204,
        json,
      })
    );

    const result = await client.delete<void>('/api/v1/components/some-id');
    expect(result).toBeUndefined();
    expect(json).not.toHaveBeenCalled();
  });

  it('sends Authorization header when token is set', async () => {
    const client = await getClient();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({}),
    });
    vi.stubGlobal('fetch', fetchMock);

    client.setToken('my-token');
    await client.get('/secure');

    const [, opts] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect((opts.headers as Record<string, string>).Authorization).toBe('Bearer my-token');
  });

  it('throws a typed ApiRequestError carrying the HTTP status on 401', async () => {
    const mod = await import('./client');
    const client = mod.apiClient;
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        json: () => Promise.resolve({ message: 'unauthorized' }),
      })
    );

    await expect(client.get('/secure')).rejects.toBeInstanceOf(mod.ApiRequestError);
    await expect(client.get('/secure')).rejects.toMatchObject({ status: 401, message: 'unauthorized' });
  });

  it('invokes the unauthorized handler exactly on 401, not on other non-2xx', async () => {
    const client = await getClient();
    const onUnauthorized = vi.fn();
    client.setUnauthorizedHandler(onUnauthorized);

    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        json: () => Promise.resolve({ message: 'nope' }),
      })
    );
    await expect(client.get('/secure')).rejects.toBeTruthy();
    expect(onUnauthorized).toHaveBeenCalledTimes(1);

    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 500,
        json: () => Promise.resolve({ message: 'boom' }),
      })
    );
    await expect(client.get('/secure')).rejects.toBeTruthy();
    // Still once — a 500 must not trip the logged-out transition.
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });
});
