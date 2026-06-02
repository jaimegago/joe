import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { createWrapper } from '@/test/utils';
import { useCurrentUser } from './useCurrentUser';
import { fetchCurrentUser } from '@/api/currentUser';

vi.mock('@/api/currentUser', () => ({ fetchCurrentUser: vi.fn() }));
const mockFetch = vi.mocked(fetchCurrentUser);

describe('useCurrentUser', () => {
  beforeEach(() => mockFetch.mockReset());

  it('surfaces admin status from the current-user endpoint', async () => {
    mockFetch.mockResolvedValue({ principal: 'alice', is_admin: true, rbac_enabled: true, oidc_enabled: false });
    const { Wrapper } = createWrapper();
    const { result } = renderHook(() => useCurrentUser(), { wrapper: Wrapper });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.is_admin).toBe(true);
    expect(result.current.data?.principal).toBe('alice');
    expect(result.current.data?.rbac_enabled).toBe(true);
  });

  it('reports non-admin status', async () => {
    mockFetch.mockResolvedValue({ principal: 'bob', is_admin: false, rbac_enabled: true, oidc_enabled: false });
    const { Wrapper } = createWrapper();
    const { result } = renderHook(() => useCurrentUser(), { wrapper: Wrapper });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.is_admin).toBe(false);
  });
});
