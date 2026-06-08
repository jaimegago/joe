import { Eye } from 'lucide-react';
import { useMutateStatus } from '@/hooks/useMutateStatus';

// OBSERVATION_DOCS_URL points at the operational-modes documentation. There is
// no dedicated, published observation-mode doc page or any existing doc-link
// convention in the sibling banners yet, so this is a clearly-marked placeholder
// constant — update it to the canonical observation-mode docs URL once one is
// published.
const OBSERVATION_DOCS_URL = 'https://docs.joe.dev/operational-modes#observation';

// ObservationBanner is the app-shell-wide observation-mode indicator, the
// write-floor counterpart to SafeModeBanner and IncidentBanner. It reads
// GET /api/v1/mutate-status via useMutateStatus and renders a calm, low-alarm
// top-of-page notice only while Joe booted into observation mode.
//
// Visibility gates on reason === "observation", NOT on can_mutate. can_mutate
// is also false in safe mode, so gating on it would make this banner collide
// with SafeModeBanner during a panic. observation and safe_mode are mutually
// exclusive by construction (the floor carries a single reason), so this banner
// and SafeModeBanner can never both render. Observation is the intended default
// read-only posture — not an error and not a lock to clear — so this banner
// never carries the safe-mode recovery wording (resuming writes); that copy
// belongs to safe mode alone. It is an app-shell concern, not chat content — it never enters
// the chat-history snapshot.
export function ObservationBanner() {
  const { data } = useMutateStatus();
  if (data?.reason !== 'observation') return null;

  return (
    <div
      role="status"
      className="flex items-center gap-2 border-b border-blue-300 bg-blue-50 px-4 py-2 text-sm text-blue-900 dark:border-blue-700 dark:bg-blue-950 dark:text-blue-200"
    >
      <Eye className="h-4 w-4 shrink-0" aria-hidden="true" />
      <span>
        <span className="font-semibold">Joe is running in observation mode</span> — it can read and
        explain, but will not make any changes.{' '}
        <a
          href={OBSERVATION_DOCS_URL}
          target="_blank"
          rel="noreferrer"
          className="font-medium underline underline-offset-2"
        >
          Learn more
        </a>
        .
      </span>
    </div>
  );
}
