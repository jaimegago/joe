import { memo } from 'react';
import { Handle, Position, type NodeProps } from 'reactflow';
import { NODE_KIND_CONFIG, DEFAULT_NODE_CONFIG, STATUS_CONFIG } from '@/lib/constants';
import type { GraphNode } from '@/api/types';

export const GenericNode = memo(function GenericNode({ data, selected }: NodeProps<GraphNode>) {
  const cfg = NODE_KIND_CONFIG[data.kind] ?? DEFAULT_NODE_CONFIG;
  const status = STATUS_CONFIG[data.status ?? 'unknown'] ?? STATUS_CONFIG.unknown;

  return (
    <div
      style={{
        borderColor: selected ? cfg.color : '#e2e8f0',
        borderWidth: selected ? 2 : 1,
        backgroundColor: cfg.bgColor,
      }}
      className="min-w-[160px] max-w-[200px] rounded-lg border bg-white p-3 shadow-sm"
    >
      <Handle type="target" position={Position.Top} className="!border-0 !bg-gray-400" />
      <div className="flex items-center gap-1.5">
        <span className="text-base leading-none">{cfg.icon}</span>
        <span className="truncate text-sm font-semibold" title={data.name}>
          {data.name}
        </span>
      </div>
      <div className="mt-1 border-t border-gray-100 pt-1">
        <p className="truncate text-xs text-gray-500">
          {data.namespace ? `${data.namespace}` : ''}
          {data.cluster ? ` / ${data.cluster}` : ''}
        </p>
        <p className="mt-0.5 flex items-center gap-1 text-xs" style={{ color: status.color }}>
          <span>{status.dot}</span>
          <span>{data.kind}</span>
        </p>
      </div>
      <Handle type="source" position={Position.Bottom} className="!border-0 !bg-gray-400" />
    </div>
  );
});
