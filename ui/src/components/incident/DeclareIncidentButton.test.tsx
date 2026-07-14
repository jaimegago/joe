import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { DeclareIncidentButton } from './DeclareIncidentButton';
import { DeclareAffordanceProvider } from './DeclareAffordanceProvider';
import { useClaimDeclareAffordance } from './declareAffordance';
import { useRegime } from '@/hooks/useRegime';
import { createWrapper } from '@/test/utils';

vi.mock('@/hooks/useRegime', () => ({ useRegime: vi.fn() }));
const mockUseRegime = vi.mocked(useRegime);

function setRegime(mode: 'normal' | 'incident') {
  mockUseRegime.mockReturnValue({ data: { mode } } as ReturnType<typeof useRegime>);
}

// InViewDeclare stands in for the chat view's own declare affordance: it claims the
// declare slot exactly when it renders its button, which is what ChatPage does.
function InViewDeclare({ shown }: { shown: boolean }) {
  useClaimDeclareAffordance(shown);
  return shown ? <button>Declare incident</button> : null;
}

// renderShell mirrors the real AppShell nesting: the provider wraps BOTH the global
// sidebar control and the routed view that may offer its own declare affordance. The
// rerender it returns reuses the SAME provider instance, so a re-render exercises the
// live claim/release path rather than remounting the slot back to empty.
function renderShell(inView: boolean) {
  const { Wrapper } = createWrapper();
  const tree = (shown: boolean) => (
    <Wrapper>
      <MemoryRouter>
        <DeclareAffordanceProvider>
          <DeclareIncidentButton />
          <InViewDeclare shown={shown} />
        </DeclareAffordanceProvider>
      </MemoryRouter>
    </Wrapper>
  );
  const { rerender } = render(tree(inView));
  return { rerender: (shown: boolean) => rerender(tree(shown)) };
}

describe('DeclareIncidentButton', () => {
  beforeEach(() => {
    mockUseRegime.mockReset();
    setRegime('normal');
  });

  it('renders the global control when no view offers its own declare affordance', () => {
    renderShell(false);
    expect(screen.getAllByRole('button', { name: /declare incident/i })).toHaveLength(1);
  });

  // The regression net for session fix-duplicate-declare-incident: a fresh chat used
  // to show the sidebar control AND the chat view's own promote-in-place control at
  // the same time. Exactly one declare affordance may be on screen.
  it('stands down so exactly one declare affordance shows when the view offers its own', () => {
    renderShell(true);
    expect(screen.getAllByRole('button', { name: /declare incident/i })).toHaveLength(1);
  });

  it('returns when the in-view affordance goes away', () => {
    const { rerender } = renderShell(true);
    rerender(false);
    // Still exactly one — but now it is the global control again, not the in-view one.
    expect(screen.getAllByRole('button', { name: /declare incident/i })).toHaveLength(1);
  });

  it('hides entirely while an incident is already active', () => {
    setRegime('incident');
    renderShell(false);
    expect(screen.queryByRole('button', { name: /declare incident/i })).not.toBeInTheDocument();
  });
});
