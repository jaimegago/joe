import { useQuery } from '@tanstack/react-query';
import { fetchSourceZones } from '@/api/security';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';

export function SourceZoneAssign() {
  const { data = [] } = useQuery({ queryKey: ['source-zones'], queryFn: fetchSourceZones });

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Source</TableHead>
          <TableHead>Zone</TableHead>
          <TableHead>Assigned By</TableHead>
          <TableHead>Assigned At</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {data.map((a) => (
          <TableRow key={a.source_id}>
            <TableCell className="font-mono text-sm">{a.source_id}</TableCell>
            <TableCell>{a.zone_id}</TableCell>
            <TableCell className="text-muted-foreground text-sm">{a.assigned_by}</TableCell>
            <TableCell className="text-muted-foreground text-sm">
              {new Date(a.assigned_at).toLocaleString()}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
