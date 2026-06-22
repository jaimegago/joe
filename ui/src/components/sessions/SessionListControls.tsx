import { Search } from 'lucide-react';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { SORT_OPTIONS, type SortKey } from '@/lib/sessionFilterSort';

export interface SessionListControlsProps {
  query: string;
  onQueryChange: (q: string) => void;
  sort: SortKey;
  onSortChange: (s: SortKey) => void;
}

// SessionListControls is the SHARED keyword-filter + sort control for both the
// Conversations and Incidents views (docs/DESIGN-SESSIONS-VIEW.md §2). One
// implementation, applied to each view's rows/clusters via applyViewControls.
// The filter matches session titles only (not message content); the sort axes
// are newest/oldest activity and title A–Z.
export function SessionListControls({
  query,
  onQueryChange,
  sort,
  onSortChange,
}: SessionListControlsProps) {
  return (
    <div className="flex items-center gap-2">
      <div className="relative flex-1">
        <Search
          className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
          aria-hidden="true"
        />
        <Input
          type="search"
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          placeholder="Filter by title…"
          aria-label="Filter sessions by title"
          className="pl-9"
        />
      </div>
      <Select value={sort} onValueChange={(v) => onSortChange(v as SortKey)}>
        <SelectTrigger className="w-[180px] shrink-0" aria-label="Sort sessions">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {SORT_OPTIONS.map((opt) => (
            <SelectItem key={opt.value} value={opt.value}>
              {opt.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
