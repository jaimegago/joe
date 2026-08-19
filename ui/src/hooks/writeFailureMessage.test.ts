import { describe, it, expect } from 'vitest';
import { writeFailureMessage } from './useChat';

// writeFailureMessage is the frontend dispatch for the backend's typed
// write-failure codes (Item 8). One branch per code, plus the unknown/absent
// fallback that lets callers keep the generic error text.
describe('writeFailureMessage', () => {
  it('maps zone_denial to the access-not-granted message', () => {
    expect(writeFailureMessage('zone_denial')).toBe(
      'Access to this zone has not been granted to you. Ask your administrator.'
    );
  });

  it('maps scope_denial to a session-scope message distinct from zone_denial', () => {
    const msg = writeFailureMessage('scope_denial');
    expect(msg).toBe(
      'That target is outside the scope this session was given — a limit of this session, not a missing permission. Ask your administrator to widen its scope.'
    );
    // scope_denial is the session's configured scope, zone_denial is a missing
    // RBAC grant. Two different remedies, so two different sentences.
    expect(msg).not.toBe(writeFailureMessage('zone_denial'));
  });

  // A code the backend can emit but this dispatch does not map renders NOTHING
  // and, because the backend summary is first-non-empty across the turn, takes
  // the slot from a mapped code later in the same turn. scope_denial can fire on
  // a refused READ — typically the earliest thing a turn does — where every code
  // above it in the switch fires only on a write, so an unmapped branch here
  // silently removed the observation-mode notice from a read-only session's
  // turn. Pin every code the backend can send.
  it('maps every tool-failure code the backend can send', () => {
    for (const code of [
      'zone_denial',
      'scope_denial',
      'incident_mode',
      'safe_mode',
      'observation',
      'internal_error',
    ]) {
      expect(writeFailureMessage(code), `no message for ${code}`).toBeDefined();
    }
  });

  it('maps incident_mode to the writes-blocked message', () => {
    expect(writeFailureMessage('incident_mode')).toBe(
      'System is in incident mode. Writes are temporarily blocked.'
    );
  });

  it('maps safe_mode to a distinct safe-mode message naming the unlock path', () => {
    const msg = writeFailureMessage('safe_mode');
    expect(msg).toBe(
      'System is in safe mode. Only read-only operations are permitted — run `joe unlock` to resume writes.'
    );
    // Must be distinguishable from the incident_mode message.
    expect(msg).not.toBe(writeFailureMessage('incident_mode'));
  });

  it('maps observation to a calm read-only message distinct from safe mode (no unlock hint)', () => {
    const msg = writeFailureMessage('observation');
    expect(msg).toBe(
      'Joe is in observation mode — it can read and explain but will not make changes. This is the intended read-only posture.'
    );
    // The calm resting posture must NOT present as safe mode and must NOT tell
    // the operator to run unlock (D-0018).
    expect(msg).not.toBe(writeFailureMessage('safe_mode'));
    expect(msg).not.toContain('unlock');
  });

  it('maps internal_error to the try-again message', () => {
    expect(writeFailureMessage('internal_error')).toBe('Unexpected error. Please try again.');
  });

  it('returns undefined for an unknown or absent code', () => {
    expect(writeFailureMessage('something_else')).toBeUndefined();
    expect(writeFailureMessage(undefined)).toBeUndefined();
  });
});
