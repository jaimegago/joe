import { describe, it, expect } from 'vitest';
import { incidentAffordance, type AffordanceInput } from './incidentAffordance';

// These tests pin every row of the INCIDENT-CHROME-AFFORDANCES matrix plus the
// cross-cutting rules. They are break-tests: each asserts the EXACT discriminated
// result for a fully-specified input, so a wrong branch (e.g. declare leaking
// onto an incident-type session) fails a row rather than passing by inspection.

describe('incidentAffordance — the six matrix rows', () => {
  it('row 1: default, not linked, regime normal → declare', () => {
    const input: AffordanceInput = {
      sessionType: 'default',
      linkedIncidentId: null,
      regimeMode: 'normal',
    };
    expect(incidentAffordance(input)).toEqual({ kind: 'declare' });
  });

  it('row 2: default, not linked, regime active → attach targeting the active master', () => {
    const input: AffordanceInput = {
      sessionType: 'default',
      linkedIncidentId: null,
      regimeMode: 'incident',
      activeMasterId: 'inc-1',
      activeMasterTitle: 'DB outage',
    };
    expect(incidentAffordance(input)).toEqual({
      kind: 'attach',
      masterId: 'inc-1',
      masterTitle: 'DB outage',
    });
  });

  it('row 3: default, linked to the active incident → linked (not resolved), names the master', () => {
    const input: AffordanceInput = {
      sessionType: 'default',
      linkedIncidentId: 'inc-1',
      linkedIncidentTitle: 'DB outage',
      regimeMode: 'incident',
      activeMasterId: 'inc-1',
      activeMasterTitle: 'DB outage',
    };
    expect(incidentAffordance(input)).toEqual({
      kind: 'linked',
      resolved: false,
      masterId: 'inc-1',
      masterTitle: 'DB outage',
    });
  });

  it('row 4: default, linked to a now-resolved incident (regime normal) → linked + resolved', () => {
    const input: AffordanceInput = {
      sessionType: 'default',
      linkedIncidentId: 'inc-old',
      linkedIncidentTitle: 'Old incident',
      regimeMode: 'normal',
    };
    expect(incidentAffordance(input)).toEqual({
      kind: 'linked',
      resolved: true,
      masterId: 'inc-old',
      masterTitle: 'Old incident',
    });
  });

  it('row 4 variant: linked to a resolved incident while a DIFFERENT incident is active → still resolved, no re-link', () => {
    const input: AffordanceInput = {
      sessionType: 'default',
      linkedIncidentId: 'inc-old',
      linkedIncidentTitle: 'Old incident',
      regimeMode: 'incident',
      activeMasterId: 'inc-new', // a different, newly-active incident
      activeMasterTitle: 'New incident',
    };
    expect(incidentAffordance(input)).toEqual({
      kind: 'linked',
      resolved: true,
      masterId: 'inc-old',
      masterTitle: 'Old incident',
    });
  });

  it('row 5: incident master, state active → manage (resolve/lifecycle)', () => {
    for (const state of ['declared', 'being_worked', 'believed_mitigated'] as const) {
      const input: AffordanceInput = {
        sessionType: 'incident',
        incidentState: state,
        regimeMode: 'incident',
        activeMasterId: 'inc-1',
      };
      expect(incidentAffordance(input)).toEqual({ kind: 'manage' });
    }
  });

  it('row 6: incident master, state resolved/reviewed → resolved badge only', () => {
    for (const state of ['resolved', 'reviewed'] as const) {
      const input: AffordanceInput = {
        sessionType: 'incident',
        incidentState: state,
        regimeMode: 'normal',
      };
      expect(incidentAffordance(input)).toEqual({ kind: 'resolved' });
    }
  });
});

describe('incidentAffordance — cross-cutting rules', () => {
  // DECLARE renders only on a default-type session AND only when regime is
  // normal; never on an incident-type session.
  it('declare never appears on an incident-type session (any lifecycle state)', () => {
    for (const state of [
      'declared',
      'being_worked',
      'believed_mitigated',
      'resolved',
      'reviewed',
    ] as const) {
      // Even with the regime momentarily normal (post-resolve), an incident-type
      // session must not offer declare.
      const result = incidentAffordance({
        sessionType: 'incident',
        incidentState: state,
        regimeMode: 'normal',
      });
      expect(result.kind).not.toBe('declare');
      expect(result.kind).not.toBe('attach');
    }
  });

  // ATTACH renders only on a default-type session, only when regime is active,
  // and only when not already linked.
  it('attach never appears on an incident-type session', () => {
    const result = incidentAffordance({
      sessionType: 'incident',
      incidentState: 'declared',
      regimeMode: 'incident',
      activeMasterId: 'inc-1',
    });
    expect(result.kind).toBe('manage');
    expect(result.kind).not.toBe('attach');
  });

  it('attach never appears on an already-linked default session', () => {
    const result = incidentAffordance({
      sessionType: 'default',
      linkedIncidentId: 'inc-1',
      regimeMode: 'incident',
      activeMasterId: 'inc-1',
    });
    expect(result.kind).toBe('linked');
  });

  it('declare requires a normal regime — an unlinked default in an active regime attaches, never declares', () => {
    const result = incidentAffordance({
      sessionType: 'default',
      linkedIncidentId: null,
      regimeMode: 'incident',
      activeMasterId: 'inc-1',
    });
    expect(result.kind).toBe('attach');
    expect(result.kind).not.toBe('declare');
  });

  it('a resolved incident master never offers resolve/manage (terminal record only)', () => {
    const result = incidentAffordance({
      sessionType: 'incident',
      incidentState: 'resolved',
      regimeMode: 'normal',
    });
    expect(result.kind).toBe('resolved');
    expect(result.kind).not.toBe('manage');
  });

  it('a missing/undefined incident state on an incident session is treated as active (fail-safe to manage, not resolved)', () => {
    const result = incidentAffordance({
      sessionType: 'incident',
      incidentState: undefined,
      regimeMode: 'incident',
    });
    expect(result.kind).toBe('manage');
  });
});
