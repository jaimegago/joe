import { useQuery } from '@tanstack/react-query';
import { fetchComponents } from '@/api/components';

export function useComponents() {
  return useQuery({
    queryKey: ['components'],
    queryFn: fetchComponents,
    refetchInterval: 30_000,
  });
}
