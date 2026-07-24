import { Link } from 'react-router';
import { Badge } from '@/components/ui/badge';
import { EmptyState } from '@/components/common/EmptyState';
import { cn } from '@/lib/utils';
import { sessionLabel } from '@/lib/sessionLabel';
import { SessionRow, type SessionRowActions } from './SessionRow';
import { isResolvedIncidentState, type IncidentCluster } from '@/lib/sessionGrouping';
import { AlertTriangle, CheckCircle2, ShieldAlert, GitBranch } from 'lucide-react';

// INCIDENT_STATE_LABEL renders the §5b-1 lifecycle state as a human chip label.
const INCIDENT_STATE_LABEL: Record<string, string> = {
  declared: 'Declared',
  being_worked: 'Being worked',
  believed_mitigated: 'Believed mitigated',
  resolved: 'Resolved',
  reviewed: 'Reviewed',
};

function stateLabel(state: string | null | undefined): string {
  if (!state) return 'Incident';
  return INCIDENT_STATE_LABEL[state] ?? state;
}

// masterName resolves the human title a cluster should show: the master's own
// title when the master row is present, else the master title a child carries
// (the P0 self-join projection), else the bare id as a last resort.
function masterName(cluster: IncidentCluster): string {
  if (cluster.master) return sessionLabel(cluster.master);
  const titled = cluster.children.find((c) => c.linked_incident_title);
  return titled?.linked_incident_title ?? cluster.masterId;
}

export interface IncidentClusterListProps extends SessionRowActions {
  clusters: IncidentCluster[];
}

// IncidentClusterList renders the incident view: every incident-involved session
// grouped into clusters (master + linked children). An ACTIVE cluster (master
// not resolved/reviewed) reads in the amber incident treatment; a RESOLVED
// cluster reads muted/terminal. Children nest under their master with an indent
// + connecting rail and a badge naming and linking back to the master.
export function IncidentClusterList({ clusters, ...actions }: IncidentClusterListProps) {
  if (clusters.length === 0) {
    return (
      <EmptyState
        icon={ShieldAlert}
        title="No incidents"
        description="Declared incidents and the sessions linked to them appear here."
      />
    );
  }

  return (
    <div className="space-y-4">
      {clusters.map((cluster) => {
        const resolved = isResolvedIncidentState(cluster.master?.incident_state);
        const name = masterName(cluster);
        return (
          <div
            key={cluster.masterId}
            className={cn(
              'overflow-hidden rounded-md border',
              resolved ? 'border-border' : 'border-amber-300 dark:border-amber-700'
            )}
          >
            {/* Master header — the group header for the cluster. */}
            <div className={cn(resolved ? 'bg-muted/50' : 'bg-amber-50 dark:bg-amber-950/40')}>
              {cluster.master ? (
                <SessionRow
                  {...actions}
                  session={cluster.master}
                  icon={
                    resolved ? (
                      <CheckCircle2
                        className="h-4 w-4 shrink-0 text-muted-foreground"
                        aria-hidden="true"
                      />
                    ) : (
                      <AlertTriangle
                        className="h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400"
                        aria-hidden="true"
                      />
                    )
                  }
                  badge={
                    <Badge
                      variant="outline"
                      className={cn(
                        'shrink-0',
                        resolved
                          ? 'border-border text-muted-foreground'
                          : 'border-amber-400 bg-amber-100 font-semibold text-amber-900 dark:border-amber-600 dark:bg-amber-950 dark:text-amber-200'
                      )}
                    >
                      <AlertTriangle className="mr-1 h-3 w-3" aria-hidden="true" />
                      Incident · {stateLabel(cluster.master.incident_state)}
                    </Badge>
                  }
                />
              ) : (
                // Orphan cluster — the master row was not in this list (e.g.
                // beyond the cap). Degrade to a non-navigable header that still
                // names the master; the children below are kept grouped.
                <div className="flex items-center gap-3 px-4 py-3">
                  <ShieldAlert
                    className="h-4 w-4 shrink-0 text-muted-foreground"
                    aria-hidden="true"
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 truncate font-medium">
                      <span className="truncate">{name}</span>
                      <Badge variant="outline" className="shrink-0 text-muted-foreground">
                        Incident
                      </Badge>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      incident master not in this list
                    </p>
                  </div>
                </div>
              )}
            </div>

            {/* Linked children — nested under the master with an indent + rail. */}
            {cluster.children.length > 0 && (
              <ul className="space-y-px border-t border-border/60 py-1 pl-4">
                {cluster.children.map((child) => (
                  <li
                    key={child.id}
                    className="border-l-2 border-amber-200 pl-3 dark:border-amber-800"
                  >
                    <SessionRow
                      {...actions}
                      session={child}
                      icon={
                        <GitBranch
                          className="h-4 w-4 shrink-0 text-muted-foreground"
                          aria-hidden="true"
                        />
                      }
                      badge={
                        <Link
                          to={`/chat/${cluster.masterId}`}
                          className="shrink-0"
                          aria-label={`Linked to incident ${name}`}
                        >
                          <Badge
                            variant="outline"
                            className={cn(
                              'shrink-0 hover:bg-accent',
                              resolved
                                ? 'border-border text-muted-foreground'
                                : 'border-amber-300 text-amber-900 dark:border-amber-700 dark:text-amber-200'
                            )}
                          >
                            <AlertTriangle className="mr-1 h-3 w-3" aria-hidden="true" />
                            Linked to {name}
                          </Badge>
                        </Link>
                      }
                    />
                  </li>
                ))}
              </ul>
            )}
          </div>
        );
      })}
    </div>
  );
}
