import type { Session } from '@/api/types';
import { sessionLabel } from '@/lib/sessionLabel';
import type { GroupedSessions, IncidentCluster } from '@/lib/sessionGrouping';

// sessionFilterSort holds the two-view sessions list CONTROLS (keyword filter +
// sort), implemented as pure functions over the OUTPUT of groupSessions
// (docs/reference/DESIGN-SESSIONS-VIEW.md §2). The pipeline is:
//
//     fetched rows → groupSessions → filterGrouped → sortGrouped
//
// These functions compose with groupSessions — they never re-run the partition
// and never re-derive the incident predicate (`incident_involved`); they only
// reorder/drop the already-grouped conversations and clusters. Filter and sort
// are independent (filter drops, sort reorders a disjoint concern), so they
// COMMUTE — applying filter-then-sort yields the identical result set and order
// as sort-then-filter. We run filter first only so the cheaper sort touches a
// smaller set.
//
// !! INTERIM CLIENT-SIDE — load-bearing for P3 !!
// These controls are CLIENT-SIDE by deliberate decision, and are correct ONLY
// while the list is UNPAGED. The list query returns a single capped top-N
// (`LIMIT 50`, no OFFSET/cursor — internal/sessionmodel/repository.go:532) in
// `ORDER BY last_activity_at DESC` order, so the full list already lives on the
// client and filtering/sorting it locally sees every row. When paging lands
// (P3), this MUST move SERVER-SIDE: a client-side filter/sort over a single page
// would filter/sort only that page, not the whole history. This is the
// known-revisit recorded in docs/reference/DESIGN-SESSIONS-VIEW.md §2.1, not a surprise.

// SortKey is the control set: newest-first by last activity (the default, which
// matches the server's existing ORDER BY last_activity_at DESC), oldest-first by
// last activity, and A–Z by title. `last_activity_at` and `title` are both on
// the list projection (internal/api/webui.go `webUISession`), so every axis is
// available; no axis had to be dropped.
export type SortKey = 'newest' | 'oldest' | 'az';

export const DEFAULT_SORT: SortKey = 'newest';

// SORT_OPTIONS drives the sort control; order is the menu order.
export const SORT_OPTIONS: readonly { value: SortKey; label: string }[] = [
  { value: 'newest', label: 'Newest activity' },
  { value: 'oldest', label: 'Oldest activity' },
  { value: 'az', label: 'Title A–Z' },
];

// titleText is the searchable / A–Z-sortable text for one row: its VISIBLE label
// (sessionLabel — title, else summary, else the untitled fallback), lower-cased.
// We match the label the user actually sees rather than the raw `title` column
// alone, because `title` is optional on the projection — keying on the bare
// column would make every untitled session unmatchable the moment a query is
// typed. This is still session METADATA, never message content; conversational
// content-search is a separate, backlogged capability
// (docs/backlog/session-content-search.md).
function titleText(s: Session): string {
  return sessionLabel(s).toLowerCase();
}

// activityTime is the millisecond timestamp a date sort orders by: the row's
// last_activity_at, falling back to started_at (always present) and then 0 for
// an unparseable value, so an undated row sorts deterministically to the oldest
// end rather than throwing.
function activityTime(s: Session): number {
  const raw = s.last_activity_at ?? s.started_at;
  const ms = Date.parse(raw);
  return Number.isNaN(ms) ? 0 : ms;
}

// compareSessions is the total order for a single SortKey. Array.prototype.sort
// is stable (ES2019+), so equal keys preserve input order — which for 'newest'
// keeps the server's secondary ordering intact.
function compareSessions(a: Session, b: Session, sort: SortKey): number {
  switch (sort) {
    case 'newest':
      return activityTime(b) - activityTime(a);
    case 'oldest':
      return activityTime(a) - activityTime(b);
    case 'az':
      return titleText(a).localeCompare(titleText(b));
  }
}

// clusterRow is the single session a cluster sorts BY — its master if present,
// else its first child (the orphan case, where the master row is beyond the list
// cap). The cluster reorders as one unit keyed on this row; children never sort
// independently (§1.5: list operations target masters, children are fixed
// detail that rides with the master).
function clusterRow(c: IncidentCluster): Session | null {
  return c.master ?? c.children[0] ?? null;
}

function compareClusters(a: IncidentCluster, b: IncidentCluster, sort: SortKey): number {
  const ra = clusterRow(a);
  const rb = clusterRow(b);
  // Defensive: a cluster always has a master or ≥1 child, so both rows are
  // non-null in practice. Fall back to the masterId for total ordering if not.
  if (!ra || !rb) {
    if (sort === 'az') return a.masterId.localeCompare(b.masterId);
    return 0;
  }
  return compareSessions(ra, rb, sort);
}

// matchesQuery is the case-insensitive substring test, already lower-cased query.
function matchesQuery(s: Session, q: string): boolean {
  return titleText(s).includes(q);
}

// clusterMatches is the CLUSTER-LEVEL filter predicate (option-2 semantics): a
// cluster matches iff its MASTER's title OR ANY child's title matches. When it
// matches, the WHOLE cluster is kept (master + all children) — never a partial
// cluster. This preserves the cluster-as-atomic-unit invariant (§1.5).
function clusterMatches(c: IncidentCluster, q: string): boolean {
  if (c.master && matchesQuery(c.master, q)) return true;
  return c.children.some((child) => matchesQuery(child, q));
}

// normalizeQuery trims and lower-cases; an empty/whitespace query is the no-op
// signal (returns '').
function normalizeQuery(query: string): string {
  return query.trim().toLowerCase();
}

// filterGrouped drops non-matching rows/clusters by title (case-insensitive
// substring). An empty query is a no-op (everything shows). Conversations filter
// per-row; incident clusters filter atomically (a child-title match keeps the
// whole cluster, and a kept cluster is NEVER rendered partial). In the
// conversation view every row is its own degenerate cluster, so this reduces to
// plain per-row title match via the same predicate.
export function filterGrouped(grouped: GroupedSessions, query: string): GroupedSessions {
  const q = normalizeQuery(query);
  if (!q) return grouped;
  return {
    conversations: grouped.conversations.filter((s) => matchesQuery(s, q)),
    // Keep matching clusters WHOLE — children are not individually filtered, so
    // a cluster never renders partial.
    clusters: grouped.clusters.filter((c) => clusterMatches(c, q)),
  };
}

// sortGrouped reorders conversations (per row) and incident clusters (per
// MASTER, clusters moving as units) by the chosen SortKey. Children keep their
// existing fixed sub-order and ride with their master — sort never reorders a
// child relative to its master or relative to its siblings.
export function sortGrouped(grouped: GroupedSessions, sort: SortKey): GroupedSessions {
  return {
    conversations: [...grouped.conversations].sort((a, b) => compareSessions(a, b, sort)),
    clusters: [...grouped.clusters].sort((a, b) => compareClusters(a, b, sort)),
  };
}

// applyViewControls is the composed pipeline: filter then sort. Both views call
// this one function, so the Conversations and Incidents tabs share a single
// filter/sort implementation.
export function applyViewControls(
  grouped: GroupedSessions,
  query: string,
  sort: SortKey
): GroupedSessions {
  return sortGrouped(filterGrouped(grouped, query), sort);
}
