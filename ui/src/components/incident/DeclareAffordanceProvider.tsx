import { useCallback, useMemo, useState, type ReactNode } from 'react';
import { DeclareAffordanceContext } from './declareAffordance';

// DeclareAffordanceProvider holds the declare-slot claim for the app shell, which is
// the lowest node that contains BOTH declare entry points (the sidebar's global
// control and the routed chat view's in-view one). See declareAffordance.ts for why
// the slot exists.
//
// The claim is a COUNT, not a boolean: a route change can mount the next view's
// affordance before the previous one's cleanup runs, and counting makes that overlap
// harmless where a boolean would leave the slot stuck or prematurely free.
export function DeclareAffordanceProvider({ children }: { children: ReactNode }) {
  const [claims, setClaims] = useState(0);
  const claim = useCallback(() => setClaims((n) => n + 1), []);
  const release = useCallback(() => setClaims((n) => Math.max(0, n - 1)), []);
  const value = useMemo(() => ({ claimed: claims > 0, claim, release }), [claims, claim, release]);
  return (
    <DeclareAffordanceContext.Provider value={value}>{children}</DeclareAffordanceContext.Provider>
  );
}
