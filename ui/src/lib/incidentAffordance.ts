// incidentAffordance is the SINGLE source of truth for the per-session incident
// chrome affordance (declare / attach / linked / resolve / resolved badge). It
// exists because the chat header previously computed each control from the
// GLOBAL regime alone — so an incident-type session (active OR resolved master)
// wrongly offered Attach/Declare, and a resolved master kept offering Resolve
// (INCIDENT-CHROME-AFFORDANCES defects 1, 3b, 5). The fix is to decide the
// affordance from (the viewed session's incident ROLE) × (the global regime),
// not from the regime by itself.
//
// This function is PURE and exhaustive over the affordance matrix; it is
// unit-tested row-by-row (incidentAffordance.test.ts). Ownership/captaincy are
// deliberately NOT inputs: they are an orthogonal gate the caller layers on the
// ACTIONABLE results (declare/attach require ownership; resolve requires
// captain/admin). This function only decides WHICH affordance the session×regime
// warrants — never WHO may invoke it.

// IncidentLifecycleState mirrors the backend §5b-1 lifecycle (declared →
// being_worked → believed_mitigated → resolved → reviewed). Only the terminal
// pair (resolved, reviewed) means the incident master is historical; the rest
// are "active".
export type IncidentLifecycleState =
  | 'declared'
  | 'being_worked'
  | 'believed_mitigated'
  | 'resolved'
  | 'reviewed';

// AffordanceInput is the exact, minimal signal set the prompt fixes: the viewed
// session's type; for an incident session its lifecycle state; for a default
// session its link pointer + (when resolvable) the linked master's title; and
// the global regime (normal, or incident together with the active master's
// identity). No field here is ownership.
export interface AffordanceInput {
  sessionType: 'default' | 'incident';
  // incidentState is the lifecycle position of an INCIDENT-type session; null /
  // undefined on a default session.
  incidentState?: IncidentLifecycleState | null;
  // linkedIncidentId is the participation pointer of a DEFAULT session, or null
  // when unlinked.
  linkedIncidentId?: string | null;
  // linkedIncidentTitle is the linked master's human title when the read model
  // resolved it (defect 2); null/undefined when unavailable.
  linkedIncidentTitle?: string | null;
  // regimeMode is the single global regime.
  regimeMode: 'normal' | 'incident';
  // activeMasterId / activeMasterTitle identify the active incident master when
  // regimeMode === 'incident'. They are how a linked default session decides
  // whether its link points at the CURRENTLY-active incident (row 3) or a
  // now-resolved one (row 4), and the attach target for row 2.
  activeMasterId?: string | null;
  activeMasterTitle?: string | null;
}

// IncidentAffordance is the single primary affordance for the session-in-view.
// The caller switches on `kind`:
//   declare  — row 1: offer Declare Incident (owner-gated by caller).
//   attach   — row 2: offer Attach To Incident, targeting the active master.
//   linked   — rows 3 & 4: a navigable "Linked to «master»" badge; `resolved`
//              picks the muted style and is the marker that NO attach/declare is
//              offered (re-linking a resolved-linked session is a deferred node).
//   manage   — row 5: this IS the active incident master — offer the
//              lifecycle/resolve controls (captain/admin-gated by caller).
//   resolved — row 6: a terminal historical master — an "Incident · Resolved"
//              badge ONLY (no resolve, no reopen).
export type IncidentAffordance =
  | { kind: 'declare' }
  | { kind: 'attach'; masterId: string | null; masterTitle: string | null }
  | { kind: 'linked'; resolved: boolean; masterId: string; masterTitle: string | null }
  | { kind: 'manage' }
  | { kind: 'resolved' };

// isActiveIncidentState is the §C "active" predicate: active === NOT terminal.
function isActiveIncidentState(state: IncidentLifecycleState | null | undefined): boolean {
  return state !== 'resolved' && state !== 'reviewed';
}

// incidentAffordance maps one (session role × regime) to its single primary
// affordance. The branch order encodes the cross-cutting rules structurally:
// an incident-type session can ONLY reach 'manage' or 'resolved' (so it never
// offers declare/attach — defects 1 & 3b), a linked default never reaches attach
// or declare, attach requires an active regime, and declare is the sole
// remaining default+normal case.
export function incidentAffordance(input: AffordanceInput): IncidentAffordance {
  // Incident-type session: the only two outcomes are the active-master
  // management surface or the terminal resolved badge. Crucially, NEITHER
  // declare nor attach is reachable here — that is the whole point (defects 1,
  // 3b) and the removal of the path that triggered defect 5.
  if (input.sessionType === 'incident') {
    return isActiveIncidentState(input.incidentState) ? { kind: 'manage' } : { kind: 'resolved' };
  }

  // Default session that participates in some incident: render the navigable
  // "Linked to «master»" badge. It is "linked to the currently-active incident"
  // (row 3) ONLY when the regime is active AND the active master IS this link's
  // target; otherwise the linked incident has resolved (or a different incident
  // is now active) → row 4, muted, no re-link. Either way: no attach, no declare.
  if (input.linkedIncidentId) {
    const linkedToActive =
      input.regimeMode === 'incident' && input.activeMasterId === input.linkedIncidentId;
    return {
      kind: 'linked',
      resolved: !linkedToActive,
      masterId: input.linkedIncidentId,
      masterTitle: input.linkedIncidentTitle ?? null,
    };
  }

  // Unlinked default session. With an incident active it can ATTACH to the
  // active master (row 2); with the regime normal it can DECLARE a fresh
  // incident (row 1). Declare is the sole default+unlinked+normal outcome — and
  // it is reachable ONLY here, never on an incident-type session.
  if (input.regimeMode === 'incident') {
    return {
      kind: 'attach',
      masterId: input.activeMasterId ?? null,
      masterTitle: input.activeMasterTitle ?? null,
    };
  }
  return { kind: 'declare' };
}
