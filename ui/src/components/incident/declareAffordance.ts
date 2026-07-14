import { createContext, useContext, useEffect } from 'react';

// §12.10 puts a declare-incident affordance in TWO places on purpose, and they are
// not the same control: the sidebar's global one is always reachable and routes to
// the sessions area's promote-or-start-new disambiguation, while the chat view's
// carries the secondary promote-THIS-session-in-place entry point. Both are wanted;
// what is not wanted is seeing both at once, which is exactly what an owned,
// unlinked, default session under the normal regime produced (a fresh chat being the
// common case) — two "Declare incident" buttons on screen, session
// fix-duplicate-declare-incident.
//
// This claim registry resolves the overlap without deleting either entry point. The
// in-view (chat) affordance CLAIMS the declare slot while it is rendered; the global
// sidebar control stands down for as long as the slot is claimed and returns the
// moment it is released (navigation away, a non-owner view, an incident-type
// session, an active regime — any case where the chat view stops offering declare).
// Capability is unchanged: whichever control is on screen still does what §12.10
// says it does.
export interface DeclareAffordanceSlot {
  // claimed is true while some in-view affordance holds the slot.
  claimed: boolean;
  // claim / release are stable across renders so the claiming effect below does not
  // re-fire when the claimed flag itself flips.
  claim: () => void;
  release: () => void;
}

export const DeclareAffordanceContext = createContext<DeclareAffordanceSlot | null>(null);

// useClaimDeclareAffordance is called by the in-view declare affordance with the
// same condition that governs whether it renders. Outside a provider it is inert, so
// a component tested in isolation needs no wrapper.
export function useClaimDeclareAffordance(active: boolean): void {
  const slot = useContext(DeclareAffordanceContext);
  const claim = slot?.claim;
  const release = slot?.release;
  useEffect(() => {
    if (!active || !claim || !release) return;
    claim();
    return release;
  }, [active, claim, release]);
}

// useDeclareAffordanceClaimed is the global control's read side: true means an
// in-view affordance is already offering declare and the global one should hide.
export function useDeclareAffordanceClaimed(): boolean {
  return useContext(DeclareAffordanceContext)?.claimed ?? false;
}
