import { describe, it, expect } from 'vitest';
import { groupSessions, type GroupedSessions } from './sessionGrouping';
import { filterGrouped, sortGrouped, applyViewControls } from './sessionFilterSort';
import type { Session } from '@/api/types';

// session builds a projected list row with sane defaults; `incident_involved` is
// set per case as the server sends it (the controls consume the partition, never
// re-derive it).
function session(over: Partial<Session> & { id: string }): Session {
  return {
    started_at: '2026-06-22T10:00:00Z',
    last_activity_at: '2026-06-22T10:00:00Z',
    message_count: 1,
    type: 'default',
    incident_involved: false,
    ...over,
  };
}

// ---- Conversations fixtures (titles + distinct activity times) -------------
const apple = session({ id: 'apple', title: 'Apple deploy', last_activity_at: '2026-06-22T12:00:00Z' });
const banana = session({ id: 'banana', title: 'Banana rollout', last_activity_at: '2026-06-22T09:00:00Z' });
const cherry = session({ id: 'cherry', title: 'Cherry pipeline', last_activity_at: '2026-06-22T15:00:00Z' });

// ---- Incident-cluster fixtures ---------------------------------------------
// Master "Database outage" (newest), two ordered children.
const masterDb = session({
  id: 'mDb',
  type: 'incident',
  incident_involved: true,
  incident_state: 'being_worked',
  title: 'Database outage',
  last_activity_at: '2026-06-22T14:00:00Z',
});
const childDb1 = session({
  id: 'cDb1',
  incident_involved: true,
  linked_incident_id: 'mDb',
  linked_incident_title: 'Database outage',
  title: 'Replica lag probe',
  last_activity_at: '2026-06-22T13:00:00Z',
});
const childDb2 = session({
  id: 'cDb2',
  incident_involved: true,
  linked_incident_id: 'mDb',
  linked_incident_title: 'Database outage',
  title: 'Failover notes',
  last_activity_at: '2026-06-22T13:30:00Z',
});

// Master "Network blip" (older), one child whose OWN title is the only
// occurrence of the word "Gateway" — used for child-match-keeps-whole-cluster.
const masterNet = session({
  id: 'mNet',
  type: 'incident',
  incident_involved: true,
  incident_state: 'declared',
  title: 'Network blip',
  last_activity_at: '2026-06-22T08:00:00Z',
});
const childNet1 = session({
  id: 'cNet1',
  incident_involved: true,
  linked_incident_id: 'mNet',
  linked_incident_title: 'Network blip',
  title: 'Gateway capture',
  last_activity_at: '2026-06-22T07:00:00Z',
});

const conversationRows = [apple, banana, cherry];
const incidentRows = [masterDb, childDb1, childDb2, masterNet, childNet1];

// helpers to read ids out of a grouped result
const convoIds = (g: GroupedSessions) => g.conversations.map((s) => s.id);
const clusterIds = (g: GroupedSessions) => g.clusters.map((c) => c.masterId);
const childIds = (g: GroupedSessions, masterId: string) =>
  g.clusters.find((c) => c.masterId === masterId)?.children.map((s) => s.id) ?? [];

describe('filterGrouped — conversation view (degenerate per-row clusters)', () => {
  const grouped = groupSessions(conversationRows);

  it('empty query is a no-op — everything shows', () => {
    expect(convoIds(filterGrouped(grouped, ''))).toEqual(['apple', 'banana', 'cherry']);
    expect(convoIds(filterGrouped(grouped, '   '))).toEqual(['apple', 'banana', 'cherry']);
  });

  it('plain per-row title match, case-insensitive substring', () => {
    expect(convoIds(filterGrouped(grouped, 'BAN'))).toEqual(['banana']);
    expect(convoIds(filterGrouped(grouped, 'pipeline'))).toEqual(['cherry']);
  });

  it('no match hides the row', () => {
    expect(convoIds(filterGrouped(grouped, 'zzz'))).toEqual([]);
  });
});

describe('filterGrouped — incident view (cluster-level, atomic)', () => {
  const grouped = groupSessions(incidentRows);

  it('master-title match shows the WHOLE cluster (master + all children)', () => {
    const out = filterGrouped(grouped, 'database');
    expect(clusterIds(out)).toEqual(['mDb']);
    // Whole cluster: both children ride along, never partial.
    expect(childIds(out, 'mDb')).toEqual(['cDb1', 'cDb2']);
  });

  it('child-title match shows the WHOLE cluster, not just the matching child', () => {
    // "Gateway" appears only in childNet1's title, never the master's.
    const out = filterGrouped(grouped, 'gateway');
    expect(clusterIds(out)).toEqual(['mNet']);
    // The master and EVERY child of the matched cluster render — break-test for
    // partial clusters: the matching child does not appear alone.
    expect(out.clusters[0].master?.id).toBe('mNet');
    expect(childIds(out, 'mNet')).toEqual(['cNet1']);
  });

  it('a matched cluster is NEVER rendered partial — all children always present', () => {
    // Match the master of the two-child cluster; assert the matching child set
    // is the FULL child set, i.e. filtering does not prune children.
    const out = filterGrouped(grouped, 'outage');
    expect(out.clusters).toHaveLength(1);
    expect(childIds(out, 'mDb')).toEqual(['cDb1', 'cDb2']);
    // None of the children's own titles contain "outage"; they show purely
    // because the cluster matched as a unit.
    expect(childDb1.title?.toLowerCase().includes('outage')).toBe(false);
    expect(childDb2.title?.toLowerCase().includes('outage')).toBe(false);
  });

  it('no-match hides the entire cluster', () => {
    expect(clusterIds(filterGrouped(grouped, 'zzz'))).toEqual([]);
  });

  it('empty query keeps all clusters whole', () => {
    const out = filterGrouped(grouped, '');
    expect(clusterIds(out)).toEqual(['mDb', 'mNet']);
    expect(childIds(out, 'mDb')).toEqual(['cDb1', 'cDb2']);
  });
});

describe('sortGrouped — conversation view', () => {
  const grouped = groupSessions(conversationRows);

  it('newest activity first (matches the server default)', () => {
    // cherry 15:00 > apple 12:00 > banana 09:00
    expect(convoIds(sortGrouped(grouped, 'newest'))).toEqual(['cherry', 'apple', 'banana']);
  });

  it('oldest activity first', () => {
    expect(convoIds(sortGrouped(grouped, 'oldest'))).toEqual(['banana', 'apple', 'cherry']);
  });

  it('A–Z by title', () => {
    expect(convoIds(sortGrouped(grouped, 'az'))).toEqual(['apple', 'banana', 'cherry']);
  });

  it('does not mutate the input', () => {
    const before = convoIds(grouped);
    sortGrouped(grouped, 'az');
    expect(convoIds(grouped)).toEqual(before);
  });
});

describe('sortGrouped — incident view (masters reorder, children ride along)', () => {
  const grouped = groupSessions(incidentRows);

  it('newest: masters reorder by master activity; mDb(14:00) before mNet(08:00)', () => {
    const out = sortGrouped(grouped, 'newest');
    expect(clusterIds(out)).toEqual(['mDb', 'mNet']);
  });

  it('oldest: masters reverse to mNet before mDb', () => {
    const out = sortGrouped(grouped, 'oldest');
    expect(clusterIds(out)).toEqual(['mNet', 'mDb']);
  });

  it('A–Z by master title: "Database outage" before "Network blip"', () => {
    const out = sortGrouped(grouped, 'az');
    expect(clusterIds(out)).toEqual(['mDb', 'mNet']);
  });

  it('children KEEP their fixed sub-order and ride with their master under every sort', () => {
    // childDb1 then childDb2 is the input/grouped sub-order. A child sort by
    // activity would put cDb2(13:30) before cDb1(13:00) — assert that NEVER
    // happens: the sub-order is fixed regardless of the master sort axis.
    for (const key of ['newest', 'oldest', 'az'] as const) {
      const out = sortGrouped(grouped, key);
      expect(childIds(out, 'mDb')).toEqual(['cDb1', 'cDb2']);
      expect(childIds(out, 'mNet')).toEqual(['cNet1']);
    }
  });
});

describe('sortGrouped — orphan cluster sorts by its representative child', () => {
  const orphanChild = session({
    id: 'orphanC',
    incident_involved: true,
    linked_incident_id: 'absentMaster',
    linked_incident_title: 'Absent master',
    title: 'Orphan work',
    last_activity_at: '2026-06-22T20:00:00Z', // newest of all
  });
  const grouped = groupSessions([masterDb, childDb1, orphanChild]);

  it('a master-less cluster orders by its first child (no throw, deterministic)', () => {
    // orphan child 20:00 is newest → its cluster sorts ahead of mDb under newest.
    const out = sortGrouped(grouped, 'newest');
    expect(clusterIds(out)).toEqual(['absentMaster', 'mDb']);
  });
});

describe('applyViewControls — composed filter then sort', () => {
  const grouped = groupSessions(conversationRows);

  it('filters then sorts (filter and sort commute — same result either order)', () => {
    // Filter to titles containing "a" → apple, banana (cherry has no 'a'),
    // then A–Z.
    const composed = applyViewControls(grouped, 'a', 'az');
    expect(convoIds(composed)).toEqual(['apple', 'banana']);

    // Manually sort-then-filter must match.
    const sortThenFilter = filterGrouped(sortGrouped(grouped, 'az'), 'a');
    expect(convoIds(composed)).toEqual(convoIds(sortThenFilter));
  });

  it('incident view: filter keeps whole clusters, then masters reorder', () => {
    const ig = groupSessions(incidentRows);
    const out = applyViewControls(ig, '', 'oldest');
    expect(clusterIds(out)).toEqual(['mNet', 'mDb']);
    expect(childIds(out, 'mDb')).toEqual(['cDb1', 'cDb2']);
  });
});
