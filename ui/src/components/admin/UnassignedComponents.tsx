import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { assignZone } from '@/api/security';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { QUERY_KEYS } from '@/lib/queryKeys';
import type { SecurityZone } from '@/api/types';

interface UnassignedComponentsProps {
  unassigned: { component_id: string }[];
  zones: SecurityZone[];
}

export function UnassignedComponents({ unassigned, zones }: UnassignedComponentsProps) {
  const qc = useQueryClient();

  const assignMut = useMutation({
    mutationFn: ({ component_id, zoneId }: { component_id: string; zoneId: string }) =>
      assignZone(component_id, zoneId),
    onSuccess: () => {
      toast.success('Zone assigned');
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.unassigned });
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.componentZones });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  if (unassigned.length === 0) return null;

  return (
    <div className="mb-4 rounded-lg border border-yellow-200 bg-yellow-50 p-4">
      <p className="mb-3 text-sm font-medium text-yellow-800">
        ⚠ {unassigned.length} unassigned source{unassigned.length > 1 ? 's' : ''} (require admin
        action)
      </p>
      <div className="space-y-2">
        {unassigned.map(({ component_id }) => (
          <div key={component_id} className="flex items-center justify-between gap-2">
            <span className="font-mono text-sm">{component_id}</span>
            <Select onValueChange={(zoneId) => assignMut.mutate({ component_id, zoneId })}>
              <SelectTrigger className="h-7 w-40 text-xs">
                <SelectValue placeholder="Assign Zone" />
              </SelectTrigger>
              <SelectContent>
                {zones.map((z) => (
                  <SelectItem key={z.id} value={z.id}>
                    {z.name || z.id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        ))}
      </div>
    </div>
  );
}
