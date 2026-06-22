import { describe, it, expect } from 'vitest';
import { groupSessions, isResolvedIncidentState } from './sessionGrouping';
import type { Session } from '@/api/types';

// session builds a projected list row with sane defaults. `incident_involved`
// is set EXPLICITLY per case (as the server sends it) so the tests exercise the
// flag the transform actually reads — not a value re-derived from type/link.
function session(over: Partial<Session> & { id: string }): Session {
  return {
    started_at: '2026-06-22T10:00:00Z',
    message_count: 1,
    type: 'default',
    incident_involved: false,
    ...over,
  };
}

// A plain conversation: incident-free.
const plain = session({ id: 'plain' });

// An incident master with two linked children.
const masterA = session({
  id: 'mA',
  type: 'incident',
  incident_involved: true,
  incident_state: 'being_worked',
});
const childA1 = session({
  id: 'cA1',
  incident_involved: true,
  linked_incident_id: 'mA',
  linked_incident_title: 'Master A',
});
const childA2 = session({
  id: 'cA2',
  incident_involved: true,
  linked_incident_id: 'mA',
  linked_incident_title: 'Master A',
});

// A resolved master with one child.
const masterR = session({
  id: 'mR',
  type: 'incident',
  incident_involved: true,
  incident_state: 'resolved',
});
const childR1 = session({
  id: 'cR1',
  incident_involved: true,
  linked_incident_id: 'mR',
  linked_incident_title: 'Master R',
});

// A standalone master with zero children.
const masterLone = session({
  id: 'mLone',
  type: 'incident',
  incident_involved: true,
  incident_state: 'declared',
});

// An orphan child: incident-involved, points at a master NOT present in the input.
const orphan = session({
  id: 'orphan',
  incident_involved: true,
  linked_incident_id: 'absent',
  linked_incident_title: 'Absent master',
});

describe('groupSessions — row-type matrix', () => {
  it('the empty list yields empty buckets', () => {
    const { conversations, clusters } = groupSessions([]);
    expect(conversations).toEqual([]);
    expect(clusters).toEqual([]);
  });

  it('a plain default lands in conversations, never in a cluster', () => {
    const { conversations, clusters } = groupSessions([plain]);
    expect(conversations.map((s) => s.id)).toEqual(['plain']);
    expect(clusters).toEqual([]);
  });

  it('a master with N children forms one cluster with the master and its children', () => {
    const { conversations, clusters } = groupSessions([masterA, childA1, childA2]);
    expect(conversations).toEqual([]);
    expect(clusters).toHaveLength(1);
    expect(clusters[0].master?.id).toBe('mA');
    expect(clusters[0].children.map((s) => s.id)).toEqual(['cA1', 'cA2']);
  });

  it('a lone child whose master is absent is grouped (orphan cluster), never a top-level row', () => {
    const { conversations, clusters } = groupSessions([orphan]);
    // Never leaks into the incident-free conversation view.
    expect(conversations).toEqual([]);
    expect(clusters).toHaveLength(1);
    // No master row present, but the child is grouped (not orphaned/dropped).
    expect(clusters[0].master).toBeNull();
    expect(clusters[0].masterId).toBe('absent');
    expect(clusters[0].children.map((s) => s.id)).toEqual(['orphan']);
  });

  it('a resolved master with children clusters the same way; state is carried for styling', () => {
    const { clusters } = groupSessions([masterR, childR1]);
    expect(clusters).toHaveLength(1);
    expect(clusters[0].master?.id).toBe('mR');
    expect(clusters[0].master?.incident_state).toBe('resolved');
    expect(clusters[0].children.map((s) => s.id)).toEqual(['cR1']);
  });

  it('a master with zero children renders as a header with an empty children list', () => {
    const { clusters } = groupSessions([masterLone]);
    expect(clusters).toHaveLength(1);
    expect(clusters[0].master?.id).toBe('mLone');
    expect(clusters[0].children).toEqual([]);
  });

  it('a child whose master is also present nests under that master (out of conversations)', () => {
    const { conversations, clusters } = groupSessions([childA1, masterA]);
    expect(conversations).toEqual([]);
    expect(clusters).toHaveLength(1);
    expect(clusters[0].master?.id).toBe('mA');
    expect(clusters[0].children.map((s) => s.id)).toEqual(['cA1']);
  });
});

describe('groupSessions — partition invariants over a mixed set', () => {
  const all = [plain, masterA, childA1, childA2, masterR, childR1, masterLone, orphan];

  it('partitions every row into exactly one bucket — none dropped, none duplicated', () => {
    const { conversations, clusters } = groupSessions(all);
    const seen = [
      ...conversations.map((s) => s.id),
      ...clusters.flatMap((c) => [
        ...(c.master ? [c.master.id] : []),
        ...c.children.map((s) => s.id),
      ]),
    ];
    // Every input id appears exactly once across both buckets.
    expect(seen.sort()).toEqual([...all.map((s) => s.id)].sort());
    expect(new Set(seen).size).toBe(seen.length);
  });

  it('the conversation view contains zero incident-involved rows', () => {
    const { conversations } = groupSessions(all);
    expect(conversations.every((s) => s.incident_involved === false)).toBe(true);
    expect(conversations.map((s) => s.id)).toEqual(['plain']);
  });

  it('no master appears in conversations and every cluster is keyed correctly', () => {
    const { conversations, clusters } = groupSessions(all);
    expect(conversations.some((s) => s.type === 'incident')).toBe(false);
    // Four clusters: mA, mR, mLone, and the orphan group keyed by 'absent'.
    expect(clusters.map((c) => c.masterId).sort()).toEqual(['absent', 'mA', 'mLone', 'mR']);
  });

  it('preserves the input (newest-first) order of clusters by first encounter', () => {
    const { clusters } = groupSessions(all);
    expect(clusters.map((c) => c.masterId)).toEqual(['mA', 'mR', 'mLone', 'absent']);
  });
});

describe('isResolvedIncidentState', () => {
  it('treats only resolved/reviewed as terminal', () => {
    expect(isResolvedIncidentState('resolved')).toBe(true);
    expect(isResolvedIncidentState('reviewed')).toBe(true);
    expect(isResolvedIncidentState('declared')).toBe(false);
    expect(isResolvedIncidentState('being_worked')).toBe(false);
    expect(isResolvedIncidentState('believed_mitigated')).toBe(false);
    expect(isResolvedIncidentState(undefined)).toBe(false);
    expect(isResolvedIncidentState(null)).toBe(false);
  });
});
