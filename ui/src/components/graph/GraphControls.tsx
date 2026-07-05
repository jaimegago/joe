import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { LayoutDashboard, Grid3X3 } from 'lucide-react';
import type { GraphNode } from '@/api/types';

interface GraphControlsProps {
  nodes: GraphNode[];
  filterNamespace: string;
  filterKind: string;
  filterStatus: string;
  layout: 'hierarchical' | 'grid';
  onFilterNamespace: (v: string) => void;
  onFilterKind: (v: string) => void;
  onFilterStatus: (v: string) => void;
  onLayoutChange: (v: 'hierarchical' | 'grid') => void;
}

export function GraphControls({
  nodes,
  filterNamespace,
  filterKind,
  filterStatus,
  layout,
  onFilterNamespace,
  onFilterKind,
  onFilterStatus,
  onLayoutChange,
}: GraphControlsProps) {
  const namespaces = [...new Set(nodes.map((n) => n.namespace).filter(Boolean))];
  const kinds = [...new Set(nodes.map((n) => n.kind))];
  const statuses = ['healthy', 'degraded', 'unhealthy', 'unknown'];

  return (
    <div className="flex items-center gap-2">
      <Select value={filterNamespace} onValueChange={onFilterNamespace}>
        <SelectTrigger className="h-8 w-36 text-xs">
          <SelectValue placeholder="Namespace" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All namespaces</SelectItem>
          {namespaces.map((ns) => (
            <SelectItem key={ns} value={ns!}>
              {ns}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Select value={filterKind} onValueChange={onFilterKind}>
        <SelectTrigger className="h-8 w-32 text-xs">
          <SelectValue placeholder="Kind" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All kinds</SelectItem>
          {kinds.map((k) => (
            <SelectItem key={k} value={k}>
              {k}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Select value={filterStatus} onValueChange={onFilterStatus}>
        <SelectTrigger className="h-8 w-32 text-xs">
          <SelectValue placeholder="Status" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All statuses</SelectItem>
          {statuses.map((s) => (
            <SelectItem key={s} value={s}>
              {s}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <div className="ml-2 flex gap-1">
        <Button
          variant={layout === 'hierarchical' ? 'default' : 'outline'}
          size="icon"
          className="h-8 w-8"
          title="Hierarchical layout"
          onClick={() => onLayoutChange('hierarchical')}
        >
          <LayoutDashboard className="h-3 w-3" />
        </Button>
        <Button
          variant={layout === 'grid' ? 'default' : 'outline'}
          size="icon"
          className="h-8 w-8"
          title="Grid layout"
          onClick={() => onLayoutChange('grid')}
        >
          <Grid3X3 className="h-3 w-3" />
        </Button>
      </div>
    </div>
  );
}
