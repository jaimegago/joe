import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Badge } from '@/components/ui/badge';
import type { SecurityZone } from '@/api/types';

interface ZonesTableProps {
  zones: SecurityZone[];
}

export function ZonesTable({ zones }: ZonesTableProps) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Zone</TableHead>
          <TableHead>Description</TableHead>
          <TableHead>Actions</TableHead>
          <TableHead>Sources</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {zones.map((z) => (
          <TableRow key={z.id}>
            <TableCell className="font-medium">{z.name || z.id}</TableCell>
            <TableCell className="text-muted-foreground text-sm">{z.description}</TableCell>
            <TableCell>
              <div className="flex flex-wrap gap-1">
                {z.allowed_actions.map((a) => (
                  <Badge key={a} variant="secondary" className="text-xs">{a}</Badge>
                ))}
              </div>
            </TableCell>
            <TableCell>{z.sourceCount ?? 0}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
