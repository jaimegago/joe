import { useNavigate } from 'react-router-dom';
import { AlertTriangle } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useRegime } from '@/hooks/useRegime';
import { useDeclareAffordanceClaimed } from './declareAffordance';

// DeclareIncidentButton is the §12.10 GLOBAL declare-incident control: it lives in
// top-level chrome (the sidebar) and is always reachable. Triggered outside a
// session it routes to the sessions area (`/sessions?declare=1`), which presents
// the promote-or-start-new disambiguation (where start-new is
// create-empty-then-promote). It does NOT itself perform the promote — both entry
// points resolve to the one promote-in-place transition (§12.3), driven from the
// sessions area / chat view. While an incident is already active there is nothing
// to declare (single global regime), so the control hides — the IncidentBanner
// carries the active-incident state instead.
//
// It ALSO hides while the view in front of the user already offers its own declare
// affordance for the session in view (the chat-view promote-in-place control), which
// otherwise puts two "Declare incident" buttons on screen at once — most visibly on
// a fresh chat (session fix-duplicate-declare-incident). "Always reachable" is
// preserved: the control only stands down when an equivalent, more specific one is
// on screen, and it returns the instant that one goes away.
export function DeclareIncidentButton() {
  const navigate = useNavigate();
  const { data: regime } = useRegime();
  const inViewAffordanceShown = useDeclareAffordanceClaimed();
  if (regime?.mode === 'incident') return null;
  if (inViewAffordanceShown) return null;

  return (
    <Button
      variant="outline"
      size="sm"
      className="w-full justify-start gap-3 border-amber-300 text-amber-900 hover:bg-amber-50 dark:border-amber-700 dark:text-amber-200 dark:hover:bg-amber-950"
      onClick={() => navigate('/sessions?declare=1')}
    >
      <AlertTriangle className="h-4 w-4" />
      Declare incident
    </Button>
  );
}
