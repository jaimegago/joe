import { useQuery } from '@tanstack/react-query';
import { fetchPrincipals, fetchAdmins } from '@/api/security';

// usePrincipals lists the identity registry (GET /admin/principals) — the data
// source for the Users page and the policy-grant principal picker. Admin-gated
// server-side; every caller already renders behind RequireAdmin.
export function usePrincipals() {
  return useQuery({ queryKey: ['principals'], queryFn: fetchPrincipals });
}

// useAdmins lists the admin roster (GET /admin/admins). Used by the Admins tab
// and composed client-side into the Users page's admin-status column.
export function useAdmins() {
  return useQuery({ queryKey: ['admins'], queryFn: fetchAdmins });
}
