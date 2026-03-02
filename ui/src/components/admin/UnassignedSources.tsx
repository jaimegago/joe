import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { assignZone } from '@/api/security';
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import type { SecurityZone } from '@/api/types';

interface UnassignedSourcesProps {
  unassigned: { source_id: string }[];
  zones: SecurityZone[];
}

export function UnassignedSources({ unassigned, zones }: UnassignedSourcesProps) {
  const qc = useQueryClient();

  const assignMut = useMutation({
    mutationFn: ({ source_id, zoneId }: { source_id: string; zoneId: string }) =>
      assignZone(source_id, zoneId),
    onSuccess: () => {
      toast.success('Zone assigned');
      void qc.invalidateQueries({ queryKey: ['unassigned'] });
      void qc.invalidateQueries({ queryKey: ['source-zones'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  if (unassigned.length === 0) return null;

  return (
    <div className="mb-4 rounded-lg border border-yellow-200 bg-yellow-50 p-4">
      <p className="mb-3 text-sm font-medium text-yellow-800">
        ⚠ {unassigned.length} unassigned source{unassigned.length > 1 ? 's' : ''} (require admin action)
      </p>
      <div className="space-y-2">
        {unassigned.map(({ source_id }) => (
          <div key={source_id} className="flex items-center justify-between gap-2">
            <span className="font-mono text-sm">{source_id}</span>
            <Select
              onValueChange={(zoneId) => assignMut.mutate({ source_id, zoneId })}
            >
              <SelectTrigger className="h-7 w-40 text-xs">
                <SelectValue placeholder="Assign Zone" />
              </SelectTrigger>
              <SelectContent>
                {zones.map((z) => (
                  <SelectItem key={z.id} value={z.id}>{z.name || z.id}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ))}
      </div>
    </div>
  );
}
