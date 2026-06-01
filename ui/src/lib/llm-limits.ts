// describeLimit maps a limit's backend state + effective value to the
// source label shown to the operator. The convention is the backend's
// (internal/api/llmsettings.go): a backstop_fallback state is the
// hardcoded default; a configured-positive value is an operator-set
// limit; a configured value whose effective is zero is the
// explicit-disable case the backend maps to an effective of zero.
export type LimitSource = 'default' | 'operator' | 'disabled';

export function describeLimit(limit: {
  state: string;
  effective: number;
}): { source: LimitSource; label: string } {
  if (limit.state === 'backstop_fallback') {
    return { source: 'default', label: 'Default (backstop)' };
  }
  if (limit.effective > 0) {
    return { source: 'operator', label: 'Operator-set limit' };
  }
  return { source: 'disabled', label: 'Disabled' };
}
