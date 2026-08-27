import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fetchPolicies } from './security';

// The grant list is NULLABLE on the wire: the handler serializes a nil slice, so a
// zero-grant install answers {"count":0,"policies":null}. Measured against a booted
// binary on a fresh store, not assumed.
//
// This is pinned because the read-posture control's zero-grant lockout warning turns
// on telling "no grants" apart from "the grants could not be read", and a bare
// z.array() parse collapses the first into the second by rejecting. It also restores
// the Policies page's own "No policies" empty state, which was unreachable.
//
// Stubbing global fetch (the pattern client.test.ts uses) rather than mocking the
// apiClient keeps the real transport and the real zod parse in the path — the parse
// is the thing under test.
function stubResponse(body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve(body),
    })
  );
}

describe('fetchPolicies', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('reads a null policies field as an empty list, not an error', async () => {
    stubResponse({ count: 0, policies: null });
    await expect(fetchPolicies()).resolves.toEqual([]);
  });

  it('still parses a populated list', async () => {
    stubResponse({
      count: 1,
      policies: [{ id: 1, principal: 'user:a@b.c', zone_id: 'dev-full', created_at: '2026-01-01' }],
    });
    await expect(fetchPolicies()).resolves.toHaveLength(1);
  });

  it('still rejects a response whose policies field is the wrong shape', async () => {
    stubResponse({ count: 1, policies: 'not-a-list' });
    await expect(fetchPolicies()).rejects.toThrow();
  });
});
