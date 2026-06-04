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

  it('maps internal_error to the try-again message', () => {
    expect(writeFailureMessage('internal_error')).toBe('Unexpected error. Please try again.');
  });

  it('returns undefined for an unknown or absent code', () => {
    expect(writeFailureMessage('something_else')).toBeUndefined();
    expect(writeFailureMessage(undefined)).toBeUndefined();
  });
});
