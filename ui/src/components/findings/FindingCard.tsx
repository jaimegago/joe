import { Link } from 'react-router';
import { formatDistanceToNow } from 'date-fns';
import { Badge } from '@/components/ui/badge';
import type { RunFinding, RunFindingBand, RunFindingLabel } from '@/api/types';
import { graphNodeHref, evidenceHref } from '@/lib/findingLinks';

// Band and label vocabularies are closed server-side (internal/runfindings);
// these maps render them and are keyed exhaustively so a new value fails the
// type check here rather than rendering unstyled.
const BAND_VARIANT: Record<RunFindingBand, 'destructive' | 'warning' | 'secondary' | 'outline'> = {
  P0: 'destructive',
  P1: 'warning',
  P2: 'secondary',
  P3: 'outline',
};

const LABEL_VARIANT: Record<RunFindingLabel, 'default' | 'secondary' | 'success' | 'warning'> = {
  new: 'default',
  standing: 'secondary',
  resolved: 'success',
  unknown: 'warning',
};

export function BandBadge({ band }: { band: RunFindingBand }) {
  return (
    <Badge variant={BAND_VARIANT[band]} data-testid="finding-band">
      {band}
    </Badge>
  );
}

export function LabelBadge({ label }: { label: RunFindingLabel }) {
  return (
    <Badge variant={LABEL_VARIANT[label]} data-testid="finding-label">
      {label}
    </Badge>
  );
}

interface FindingCardProps {
  finding: RunFinding;
}

// FindingCard renders one finding record: every line here is a field of the
// record, so what the view shows and what the run's session turn says come
// from the same thing. The band is the server's; the UI does not rank.
export function FindingCard({ finding: f }: FindingCardProps) {
  const d = f.derivation;
  const established = new Date(f.established_at);
  return (
    <li className="rounded-md border p-3 text-sm" data-testid="finding-card">
      <div className="flex flex-wrap items-center gap-2">
        <BandBadge band={f.band} />
        <LabelBadge label={f.label} />
        <Badge variant="outline">{f.type.replace('_', ' ')}</Badge>
        <Link
          to={graphNodeHref(f.subject_node_id)}
          className="font-mono text-xs text-primary underline-offset-2 hover:underline"
          data-testid="subject-link"
        >
          {f.subject_node_id}
        </Link>
        <span className="text-xs text-muted-foreground">({f.subject_component_id})</span>
      </div>
      <p className="mt-2 font-medium">{f.claim}</p>
      {f.condition && (
        <p className="mt-1 text-muted-foreground">
          <span className="font-medium text-foreground">Condition:</span> {f.condition}
        </p>
      )}
      <p className="mt-1 text-xs text-muted-foreground" data-testid="finding-derivation">
        {f.type === 'graph_update'
          ? 'Graph update — always P3, not ranked against issues and risks.'
          : `Base ${d.base_band} from ${d.consequence.replace(/_/g, ' ')}; reach ${d.reach.count} dependent${d.reach.count === 1 ? '' : 's'}; weight ${d.weight}; ${d.movement}.`}
      </p>
      <p className="mt-1 text-xs text-muted-foreground">
        Evidence: <span className="font-mono">{d.evidence_line}</span> —{' '}
        <Link
          to={evidenceHref(f.evidence_session_id)}
          className="text-primary underline-offset-2 hover:underline"
          data-testid="evidence-link"
        >
          open the run&apos;s session
        </Link>
        {!Number.isNaN(established.getTime()) && (
          <> · established {formatDistanceToNow(established, { addSuffix: true })}</>
        )}
      </p>
      {f.rationale && <p className="mt-1 text-xs text-muted-foreground">{f.rationale}</p>}
      {f.proposal && (
        <div
          className="mt-2 rounded border border-dashed p-2 text-xs"
          data-testid="finding-proposal"
        >
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">Proposal</span>
            <Badge variant="warning">{f.proposal.status}</Badge>
            <Badge variant="outline">{f.proposal.shape.replace(/_/g, ' ')}</Badge>
            {!f.proposal.executable && <Badge variant="secondary">not executable</Badge>}
          </div>
          <p className="mt-1">
            <span className="text-muted-foreground">Target:</span>{' '}
            <span className="font-mono">{f.proposal.target.system_id}</span>
            <span className="text-muted-foreground"> · Base:</span> {f.proposal.base}
          </p>
          <pre className="mt-1 whitespace-pre-wrap font-mono">{f.proposal.change}</pre>
          <p className="mt-1">
            <span className="text-muted-foreground">Expected effect:</span>{' '}
            {f.proposal.expected_effect}
          </p>
          <p className="mt-1">
            <span className="text-muted-foreground">Limits:</span> {f.proposal.limits}
          </p>
        </div>
      )}
    </li>
  );
}
