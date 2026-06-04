import { ShieldAlert } from 'lucide-react';

// ZeroZoneEmptyState is the first-run dead-end guard for a brand-new user who
// has authenticated but holds no zone grants yet (OPERATOR_SURFACE_-
// VERIFICATION.md items 9/11). Without it, such a user lands in a fully
// rendered chat that 403s every message with an opaque error. This replaces
// the chat input (which would only fail) with an explanation of why and what
// to do next.
export function ZeroZoneEmptyState({ principal }: { principal: string | null }) {
  return (
    <div className="flex h-full flex-col items-center justify-center px-6 text-center">
      <div className="max-w-md">
        <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-muted">
          <ShieldAlert className="h-6 w-6 text-muted-foreground" aria-hidden="true" />
        </div>
        <h2 className="text-lg font-semibold">Access pending</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          You&apos;re signed in as{' '}
          <span className="font-medium text-foreground">{principal ?? 'this account'}</span>, but no
          zones have been assigned to you yet. Ask your administrator to grant you access to one or
          more zones.
        </p>
        <p className="mt-3 text-xs text-muted-foreground">
          Zones group infrastructure by risk and control which actions you can take. Access is
          granted per zone (see the security zones documentation).
        </p>
      </div>
    </div>
  );
}
