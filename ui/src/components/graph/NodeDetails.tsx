import { X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import type { GraphNode } from '@/api/types';
import { STATUS_CONFIG, NODE_KIND_CONFIG, DEFAULT_NODE_CONFIG } from '@/lib/constants';

interface NodeDetailsProps {
  node: GraphNode;
  onClose: () => void;
}

export function NodeDetails({ node, onClose }: NodeDetailsProps) {
  const cfg = NODE_KIND_CONFIG[node.kind] ?? DEFAULT_NODE_CONFIG;
  const status = STATUS_CONFIG[node.status ?? 'unknown'] ?? STATUS_CONFIG.unknown;

  const metaEntries = Object.entries(node.metadata ?? {}).slice(0, 12);

  return (
    <div className="flex h-full w-80 shrink-0 flex-col overflow-hidden border-l bg-background">
      <div className="flex items-center justify-between border-b px-4 py-3">
        <div className="flex items-center gap-2">
          <span className="text-lg">{cfg.icon}</span>
          <div>
            <p className="font-semibold leading-none">{node.name}</p>
            <p className="mt-0.5 text-xs text-muted-foreground">{node.kind}</p>
          </div>
        </div>
        <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onClose}>
          <X className="h-4 w-4" />
        </Button>
      </div>
      <div className="flex-1 overflow-y-auto p-4 text-sm">
        <div className="space-y-3">
          <div>
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Status</p>
            <p className="mt-1 flex items-center gap-1" style={{ color: status.color }}>
              {status.dot} {status.label}
            </p>
          </div>
          {node.namespace && (
            <div>
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Namespace</p>
              <p className="mt-1">{node.namespace}</p>
            </div>
          )}
          {node.cluster && (
            <div>
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Cluster</p>
              <p className="mt-1">{node.cluster}</p>
            </div>
          )}
          {node.labels && Object.keys(node.labels).length > 0 && (
            <div>
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-1">Labels</p>
              <div className="flex flex-wrap gap-1">
                {Object.entries(node.labels).slice(0, 8).map(([k, v]) => (
                  <Badge key={k} variant="secondary" className="text-xs">
                    {k}={String(v)}
                  </Badge>
                ))}
              </div>
            </div>
          )}
          {metaEntries.length > 0 && (
            <div>
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-1">Metadata</p>
              <dl className="space-y-1">
                {metaEntries.map(([k, v]) => (
                  <div key={k} className="flex gap-2">
                    <dt className="w-28 shrink-0 truncate text-xs text-muted-foreground">{k}</dt>
                    <dd className="truncate text-xs">{String(v)}</dd>
                  </div>
                ))}
              </dl>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
