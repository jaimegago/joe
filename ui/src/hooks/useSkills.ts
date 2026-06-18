import { useQuery } from '@tanstack/react-query';
import { fetchSkills } from '@/api/skills';

// useSkills lists the installed skills (active + quarantined) with their git
// source — GET /skills. Admin-gated by its host page. Refetches periodically so
// the view tracks installs/reloads happening out of band (CLI, watcher).
export function useSkills() {
  return useQuery({
    queryKey: ['skills'],
    queryFn: fetchSkills,
    refetchInterval: 30_000,
  });
}
