import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Users, Boxes } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { setReadPosture, READ_POSTURE } from '@/api/security';
import type { ReadPosture } from '@/api/security';
import { QUERY_KEYS } from '@/lib/queryKeys';

// ReadPostureControl is the admin write affordance over the install-wide read
// posture (read-posture-latch): it shows the live posture and flips it
// team_flat <-> zoned. Every flip is audited server-side — the mutation service
// commits the posture row and its KindAdminAccess audit row in one transaction —
// so this control adds no forensic surface of its own.
//
// Admin-gated by construction: the only host is the admin page, whose whole route
// is behind <RequireAdmin>, and the backing endpoints 403 a non-admin anyway.
//
// What the posture does NOT govern is as load-bearing as what it does, and the
// copy below says so: the posture is consulted for ActionRead only, so it can
// never widen a mutate (the write floor and write-RBAC govern those
// independently), and it sits BEHIND the zone.Allows gate, so a zone that
// forbids read still forbids it under either posture.

interface PostureFacts {
  label: string;
  icon: typeof Users;
  // summary is the one-line "what this posture means" shown on the posture card.
  summary: string;
}

const POSTURE_FACTS: Record<ReadPosture, PostureFacts> = {
  [READ_POSTURE.teamFlat]: {
    label: 'Team flat',
    icon: Users,
    summary:
      'Every authenticated principal reads every component, regardless of grant. This is the launch default.',
  },
  [READ_POSTURE.zoned]: {
    label: 'Zoned',
    icon: Boxes,
    summary:
      'Reads are grant-based: a principal reads a component only if it holds a grant on that component’s zone.',
  },
};

// grantConsequence states what flipping to `zoned` will do to non-admin reads,
// keyed on how many grants exist. The zero case is the one that locks people out,
// so it is stated as a consequence rather than a caveat.
function grantConsequence(grantCount: number | undefined): string {
  if (grantCount === undefined) {
    return 'The number of configured grants could not be read, so the effect on non-admin reads cannot be summarised here. Check the Policies page after switching.';
  }
  if (grantCount === 0) {
    return 'No grants are configured, so after this switch no non-admin principal will be able to read any component. The Policies page — where grants are created — appears in the Admin nav once the posture is zoned.';
  }
  return `${grantCount} ${grantCount === 1 ? 'grant is' : 'grants are'} configured. Each non-admin principal will read only the components in zones it holds a grant on; a principal with no grant reads nothing.`;
}

export function ReadPostureControl({
  posture,
  grantCount,
}: {
  posture: ReadPosture;
  grantCount: number | undefined;
}) {
  const qc = useQueryClient();
  const [confirming, setConfirming] = useState(false);

  const target: ReadPosture =
    posture === READ_POSTURE.teamFlat ? READ_POSTURE.zoned : READ_POSTURE.teamFlat;
  const current = POSTURE_FACTS[posture];
  const next = POSTURE_FACTS[target];
  const CurrentIcon = current.icon;

  // Invalidating the shared posture key is what makes the flip visible beyond
  // this page: the Sidebar's Policies nav entry and the /admin/policies route
  // guard both read QUERY_KEYS.readPosture, so Policies appears and disappears
  // without a reload. The policies key is invalidated too — its grant rows are
  // what the zoned posture starts consulting.
  const flipMut = useMutation({
    mutationFn: (p: ReadPosture) => setReadPosture(p),
    onSuccess: (applied) => {
      qc.setQueryData(QUERY_KEYS.readPosture, applied);
      toast.success(`Read posture is now ${POSTURE_FACTS[applied].label.toLowerCase()}`);
    },
    onError: (e: Error) => toast.error(e.message),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.readPosture });
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.policies });
    },
  });

  return (
    <div className="space-y-6">
      <div className="space-y-1">
        <h3 className="text-sm font-medium">Read posture</h3>
        <p className="text-muted-foreground max-w-2xl text-sm">
          One install-wide setting deciding who may read components. It governs reads only — it can
          never widen what anyone may change — and it applies underneath zones, so a zone that
          forbids reads still forbids them in either posture. Switching takes effect on the next
          access decision, with no restart, and is recorded.
        </p>
      </div>

      <div className="max-w-2xl space-y-4 rounded-lg border p-4">
        <div className="flex items-center gap-3">
          <CurrentIcon className="text-muted-foreground h-4 w-4" aria-hidden="true" />
          <span className="text-sm font-medium">Current posture</span>
          <Badge variant={posture === READ_POSTURE.zoned ? 'default' : 'secondary'}>
            {current.label}
          </Badge>
          <code className="text-muted-foreground text-xs">{posture}</code>
        </div>
        <p className="text-muted-foreground text-sm">{current.summary}</p>
        <div className="border-t pt-4">
          <Button size="sm" onClick={() => setConfirming(true)} disabled={flipMut.isPending}>
            {flipMut.isPending ? 'Switching…' : `Switch to ${next.label.toLowerCase()}`}
          </Button>
        </div>
      </div>

      <Dialog open={confirming} onOpenChange={setConfirming}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Switch read posture to {next.label.toLowerCase()}?</DialogTitle>
            <DialogDescription>{next.summary}</DialogDescription>
          </DialogHeader>

          <div className="text-muted-foreground space-y-3 text-sm">
            {target === READ_POSTURE.zoned ? (
              <>
                <p>
                  <span className="text-foreground font-medium">Admins are unaffected.</span> The
                  admin capability admits a read regardless of grant, so administrators keep reading
                  every component.
                </p>
                <p>{grantConsequence(grantCount)}</p>
              </>
            ) : (
              <>
                <p>
                  <span className="text-foreground font-medium">This widens read access.</span>{' '}
                  Every authenticated principal — including named service-account API keys, not only
                  people — will be able to read every component.
                </p>
                <p>
                  Existing grants are kept and are not deleted, but they stop affecting reads while
                  the posture is team flat. The Policies page is hidden in this posture; the grants
                  remain manageable over the REST API.
                </p>
              </>
            )}
          </div>

          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirming(false)}>
              Cancel
            </Button>
            <Button
              variant={target === READ_POSTURE.zoned ? 'default' : 'destructive'}
              onClick={() => {
                flipMut.mutate(target);
                setConfirming(false);
              }}
            >
              Switch to {next.label.toLowerCase()}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
