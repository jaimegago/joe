import { useQuery } from '@tanstack/react-query';
import { fetchReadPromotions } from '@/api/security';

// useReadPromotions shares the per-component-type autonomous-read states app-wide.
// The query key is the invalidation target the toggle mutation refreshes after each
// independent change.
export function useReadPromotions() {
  return useQuery({ queryKey: ['read-promotions'], queryFn: fetchReadPromotions });
}
