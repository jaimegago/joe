import type { Session } from '@/api/types';

// sessionGrouping is the SINGLE pure transform behind the two-view sessions
// split (docs/DESIGN-SESSIONS-VIEW.md §1.2). Its input is the flat array of
// projected session rows from GET /sessions; its output partitions that array
// into (a) the conversation list and (b) the incident clusters. The split key
// is the P0-computed `incident_involved` flag — this function NEVER re-derives
// `type === 'incident' || linked_incident_id != null` itself (acceptance:
// the predicate is consumed, not recomputed, in the frontend).
//
// Membership is a true partition (§1.3): every row lands in exactly one of
// `conversations` or some cluster — never both, never neither, never dropped.

// IncidentCluster is one group in the incident view: an incident master row and
// the linked children nested beneath it (§1.2). `master` is null only in the
// defensive orphan case — a linked child whose master row is absent from the
// input set (e.g. beyond the list cap); the children are still grouped (never
// promoted to a top-level row, never lost), and the header degrades to naming
// the master by id/title. `masterId` is the grouping key: the master's id for a
// real master, or the children's shared linked_incident_id for an orphan group.
export interface IncidentCluster {
  masterId: string;
  master: Session | null;
  children: Session[];
}

// GroupedSessions is the full output: the incident-free conversation rows and
// the incident clusters, each preserving the input's newest-first order (the
// server already sorts by last_activity_at DESC — this transform is order-
// preserving and adds no sort of its own, P1 being default-views-only).
export interface GroupedSessions {
  conversations: Session[];
  clusters: IncidentCluster[];
}

// groupSessions partitions the flat session list into the conversation view and
// the incident clusters in a single ordered pass.
//
// - A row with incident_involved=false is a conversation (the conversation view
//   is incident-free by construction, §1.3).
// - A row with type='incident' is a cluster master (its linked_incident_id is
//   cleared by promote-in-place, §12.3 — so type, not the pointer, marks it).
// - Any other incident_involved row is a linked child; it nests under the
//   cluster keyed by its linked_incident_id.
//
// Clusters appear in order of first encounter of their key (master or first
// child), which preserves the server's newest-first ordering. Children keep
// input order within their cluster.
export function groupSessions(sessions: Session[]): GroupedSessions {
  const conversations: Session[] = [];
  const byMasterId = new Map<string, IncidentCluster>();
  const order: string[] = [];

  const cluster = (masterId: string): IncidentCluster => {
    let c = byMasterId.get(masterId);
    if (!c) {
      c = { masterId, master: null, children: [] };
      byMasterId.set(masterId, c);
      order.push(masterId);
    }
    return c;
  };

  for (const s of sessions) {
    if (!s.incident_involved) {
      conversations.push(s);
      continue;
    }
    if (s.type === 'incident') {
      cluster(s.id).master = s;
      continue;
    }
    // Incident-involved and not a master ⇒ a linked child. Its
    // linked_incident_id is the master key; fall back to the row's own id only
    // in the impossible case the server set the flag without a pointer, which
    // still keeps the row out of the conversation view.
    cluster(s.linked_incident_id ?? s.id).children.push(s);
  }

  return { conversations, clusters: order.map((id) => byMasterId.get(id)!) };
}

// isResolvedIncidentState is the §1.4 terminal predicate, mirroring
// incidentAffordance.isActiveIncidentState: only resolved/reviewed are terminal
// (historical) — every other state is an active incident. Drives the
// active(amber)-vs-resolved(muted) cluster styling.
export function isResolvedIncidentState(state: string | null | undefined): boolean {
  return state === 'resolved' || state === 'reviewed';
}
