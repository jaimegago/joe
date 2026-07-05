import type { Node, Edge } from 'reactflow';

// Simple hierarchical layout using level-based positioning
export function applyHierarchicalLayout(nodes: Node[], edges: Edge[]): Node[] {
  if (nodes.length === 0) return nodes;

  // Build adjacency
  const children: Record<string, string[]> = {};
  const parents: Record<string, string[]> = {};
  nodes.forEach((n) => {
    children[n.id] = [];
    parents[n.id] = [];
  });
  edges.forEach((e) => {
    if (children[e.source]) children[e.source].push(e.target);
    if (parents[e.target]) parents[e.target].push(e.source);
  });

  // Find roots (no parents)
  const roots = nodes.filter((n) => (parents[n.id]?.length ?? 0) === 0).map((n) => n.id);
  if (roots.length === 0) {
    // fallback: grid layout
    return nodes.map((n, i) => ({
      ...n,
      position: { x: (i % 5) * 220, y: Math.floor(i / 5) * 150 },
    }));
  }

  // BFS to assign levels
  const levels: Record<string, number> = {};
  const queue = [...roots];
  roots.forEach((r) => (levels[r] = 0));
  while (queue.length > 0) {
    const id = queue.shift()!;
    for (const child of children[id] ?? []) {
      if (levels[child] === undefined) {
        levels[child] = (levels[id] ?? 0) + 1;
        queue.push(child);
      }
    }
  }

  // Group by level
  const byLevel: Record<number, string[]> = {};
  nodes.forEach((n) => {
    const lvl = levels[n.id] ?? 0;
    if (!byLevel[lvl]) byLevel[lvl] = [];
    byLevel[lvl].push(n.id);
  });

  const nodeWidth = 200;
  const nodeHeight = 120;
  const hGap = 40;
  const vGap = 80;

  const positioned: Record<string, { x: number; y: number }> = {};
  Object.entries(byLevel).forEach(([lvlStr, ids]) => {
    const lvl = Number(lvlStr);
    const totalWidth = ids.length * nodeWidth + (ids.length - 1) * hGap;
    ids.forEach((id, i) => {
      positioned[id] = {
        x: -totalWidth / 2 + i * (nodeWidth + hGap),
        y: lvl * (nodeHeight + vGap),
      };
    });
  });

  return nodes.map((n) => ({
    ...n,
    position: positioned[n.id] ?? { x: 0, y: 0 },
  }));
}

export function applyGridLayout(nodes: Node[]): Node[] {
  return nodes.map((n, i) => ({
    ...n,
    position: { x: (i % 5) * 220, y: Math.floor(i / 5) * 150 },
  }));
}
