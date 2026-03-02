import { memo } from 'react';
import { BaseEdge, EdgeLabelRenderer, getBezierPath, type EdgeProps } from 'reactflow';
import { EDGE_TYPE_CONFIG } from '@/lib/constants';

export const DependencyEdge = memo(function DependencyEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  markerEnd,
}: EdgeProps) {
  const edgeType = (data as { type?: string } | undefined)?.type ?? 'depends_on';
  const cfg = EDGE_TYPE_CONFIG[edgeType] ?? EDGE_TYPE_CONFIG['depends_on'];

  const [edgePath, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        markerEnd={markerEnd}
        style={{
          stroke: cfg.color,
          strokeWidth: 1.5,
          strokeDasharray: cfg.style === 'dashed' ? '5,5' : cfg.style === 'dotted' ? '2,3' : undefined,
        }}
      />
      <EdgeLabelRenderer>
        <div
          style={{
            position: 'absolute',
            transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)`,
            pointerEvents: 'all',
          }}
          className="rounded bg-white px-1 py-0.5 text-xs text-gray-400 shadow-sm"
        >
          {cfg.label}
        </div>
      </EdgeLabelRenderer>
    </>
  );
});
