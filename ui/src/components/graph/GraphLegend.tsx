import { NODE_KIND_CONFIG } from '@/lib/constants';

export function GraphLegend() {
  const entries = Object.entries(NODE_KIND_CONFIG).slice(0, 8);
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1 rounded-lg border bg-white/90 px-3 py-2 text-xs shadow-sm backdrop-blur">
      {entries.map(([kind, cfg]) => (
        <div key={kind} className="flex items-center gap-1">
          <span>{cfg.icon}</span>
          <span style={{ color: cfg.color }}>{kind}</span>
        </div>
      ))}
    </div>
  );
}
