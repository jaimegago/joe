import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Badge } from '@/components/ui/badge';
import { EmptyState } from '@/components/common/EmptyState';
import { Cpu, Check, X } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { LLMProvider } from '@/api/types';

interface ProvidersTabProps {
  providers: LLMProvider[];
  current: string;
}

// ProvidersTab is a read-only presence list. The response carries only
// booleans and never key material, so this renders the key-present status
// as a boolean indicator — there is no key entry and no mutation here.
export function ProvidersTab({ providers, current }: ProvidersTabProps) {
  if (providers.length === 0) {
    return (
      <EmptyState icon={Cpu} title="No providers" description="No LLM models are configured." />
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Model</TableHead>
          <TableHead>Provider</TableHead>
          <TableHead>Backend model</TableHead>
          <TableHead>Key present</TableHead>
          <TableHead>Selected</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {providers.map((p) => (
          <TableRow key={p.name} className={cn(p.name === current && 'bg-accent/50')}>
            <TableCell className="font-medium">{p.name}</TableCell>
            <TableCell>{p.provider}</TableCell>
            <TableCell className="text-muted-foreground">{p.model}</TableCell>
            <TableCell>
              {p.key_present ? (
                <Badge variant="success" className="gap-1">
                  <Check className="h-3 w-3" /> Present
                </Badge>
              ) : (
                <Badge variant="secondary" className="gap-1">
                  <X className="h-3 w-3" /> Absent
                </Badge>
              )}
            </TableCell>
            <TableCell>{p.name === current && <Badge variant="default">Current</Badge>}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
