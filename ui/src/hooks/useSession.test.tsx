import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { createWrapper } from '@/test/utils';
import { useSession, deriveSessionStatus, nextTitlePollMs } from './useSession';
import { fetchSession } from '@/api/chat';
import type { Session } from '@/api/types';

vi.mock('@/api/chat', () => ({ fetchSession: vi.fn() }));
const mockFetch = vi.mocked(fetchSession);

const base: Session = {
  id: 's1',
  started_at: '2026-06-06T10:00:00Z',
  message_count: 2,
  visibility: 'private',
};
const owner = (over?: Partial<Session>): Session => ({ ...base, read_only: false, ...over });
const reader = (over?: Partial<Session>): Session => ({
  ...base,
  visibility: 'public',
  read_only: true,
  ...over,
});

// --- Pure status derivation: every branch, no React Query needed ------------
describe('deriveSessionStatus', () => {
  it('owner when read_only === false', () => {
    expect(deriveSessionStatus(owner(), false, false)).toBe('owner');
  });
  it('reader when read_only === true and no 404', () => {
    expect(deriveSessionStatus(reader(), false, false)).toBe('reader');
  });
  it('loading when no data and no error', () => {
    expect(deriveSessionStatus(undefined, false, false)).toBe('loading');
  });
  it('loading when read_only is absent (fails closed — not owner)', () => {
    expect(deriveSessionStatus({ ...base }, false, false)).toBe('loading');
  });
  it('gone on a 404 with no data (dead/deleted id)', () => {
    expect(deriveSessionStatus(undefined, true, false)).toBe('gone');
  });
  it('gone when a reader session then 404s (owner deleted it)', () => {
    expect(deriveSessionStatus(reader(), true, false)).toBe('gone');
  });
  it('gone when an owned session then 404s (deleted)', () => {
    expect(deriveSessionStatus(owner(), true, false)).toBe('gone');
  });
  it('does NOT go gone for a locallyCreated id that 404s (read-after-write)', () => {
    // The critical exemption: a freshly-minted session whose first metadata
    // fetch loses the read-after-write race must stay loading, never dead.
    expect(deriveSessionStatus(undefined, true, true)).toBe('loading');
    expect(deriveSessionStatus(reader(), true, true)).toBe('reader');
  });
});

// --- Pure bounded title-await decision --------------------------------------
describe('nextTitlePollMs', () => {
  it('polls (3s) while untitled, succeeding, within the cap', () => {
    expect(nextTitlePollMs({ hasTitle: false, isError: false, elapsedMs: 0 })).toBe(3000);
    expect(nextTitlePollMs({ hasTitle: false, isError: false, elapsedMs: 10_000 })).toBe(3000);
  });
  it('stops once the title has landed', () => {
    expect(nextTitlePollMs({ hasTitle: true, isError: false, elapsedMs: 0 })).toBe(false);
  });
  it('stops immediately on error (does not hammer a 404)', () => {
    expect(nextTitlePollMs({ hasTitle: false, isError: true, elapsedMs: 0 })).toBe(false);
  });
  it('gives up after the elapsed cap even if never titled', () => {
    expect(nextTitlePollMs({ hasTitle: false, isError: false, elapsedMs: 41_000 })).toBe(false);
  });
});

// --- Integration: the hook wiring over React Query --------------------------
describe('useSession (integration)', () => {
  beforeEach(() => mockFetch.mockReset());

  const render = (id: string | null, opts?: { locallyCreated?: boolean }) => {
    const { qc, Wrapper } = createWrapper();
    const view = renderHook(() => useSession(id, opts), { wrapper: Wrapper });
    return { qc, ...view };
  };

  it('resolves to owner and exposes derived fields', async () => {
    mockFetch.mockResolvedValue(owner({ linked_incident_id: 'inc-1' }));
    const { result } = render('s1');
    expect(result.current.status).toBe('loading');
    await waitFor(() => expect(result.current.status).toBe('owner'));
    expect(result.current.isOwner).toBe(true);
    expect(result.current.isLinkedToIncident).toBe(true);
  });

  it('resolves to reader for a non-owner session', async () => {
    mockFetch.mockResolvedValue(reader());
    const { result } = render('s1');
    await waitFor(() => expect(result.current.status).toBe('reader'));
    expect(result.current.isReader).toBe(true);
  });

  // Note: the 404-driven transitions (→gone and the locallyCreated exemption)
  // are covered exhaustively by the pure deriveSessionStatus tests above, so
  // they are not re-driven through React Query here (which only adds
  // rejection-handling flakiness).

  it('applyUpdate writes the response to the mutated session id', async () => {
    mockFetch.mockResolvedValue(owner());
    const { qc, result } = render('s1');
    await waitFor(() => expect(result.current.status).toBe('owner'));
    result.current.applyUpdate(owner({ id: 's2', visibility: 'public' }));
    // Lands on s2 (the authoritative id from the response), not the hook's s1.
    expect(qc.getQueryData(['session', 's2'])).toMatchObject({ visibility: 'public' });
  });
});
