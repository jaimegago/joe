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
