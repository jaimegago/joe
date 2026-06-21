import { useQuery } from '@tanstack/react-query';
import {
  fetchAdminSessions,
  fetchAdminTrash,
  fetchRetentionPolicy,
  type AdminSessionFilters,
} from '@/api/adminSessions';

// useAdminSessions drives the cross-tenant admin list (§12.8). The filters
// (principal / type / state) are part of the query key so changing a filter
// refetches. state==='trashed' is served by the dedicated all-trash route, which
// carries purge_after for remaining-time rendering; every other state goes through
// the general list with its state filter.
export function useAdminSessions(filters: AdminSessionFilters) {
  return useQuery({
    queryKey: ['admin-sessions', filters.principal ?? '', filters.type ?? '', filters.state ?? ''],
    queryFn: () =>
      filters.state === 'trashed'
        ? fetchAdminTrash(200)
        : fetchAdminSessions({ ...filters, limit: 200 }),
  });
}

export function useRetentionPolicy() {
  return useQuery({ queryKey: ['retention-policy'], queryFn: fetchRetentionPolicy });
}
