import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  testComponent,
  deleteComponent,
  promoteComponent,
  createComponent,
} from '@/api/components';
import type { PromoteRequest } from '@/api/components';
import { fetchZones } from '@/api/security';
import { ApiRequestError } from '@/api/client';
import { useComponents } from '@/hooks/useComponents';
import { QueryError } from '@/components/common/QueryError';
import { QUERY_KEYS } from '@/lib/queryKeys';
import { PromoteComponentForm } from '@/components/admin/PromoteComponentForm';
import { ComponentRegisterForm } from '@/components/admin/ComponentRegisterForm';
import { ComponentZoneAssign } from '@/components/admin/ComponentZoneAssign';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { STATUS_CONFIG } from '@/lib/constants';
import type { Component } from '@/api/types';
import { Database } from 'lucide-react';

function StatusDot({ status }: { status: Component['status'] }) {
  const cfg = STATUS_CONFIG[status] ?? STATUS_CONFIG.unknown;
  return (
    <span className="flex items-center gap-1.5 text-sm" style={{ color: cfg.color }}>
      {cfg.dot} {cfg.label}
    </span>
  );
}

// ArmBadge renders a component's inert-vs-armed state from the read-model
// `armed` field, matched to the page's existing status-badge weight.
function ArmBadge({ armed }: { armed: boolean }) {
  return armed ? <Badge variant="success">armed</Badge> : <Badge variant="secondary">inert</Badge>;
}

export function ComponentsPage() {
  const qc = useQueryClient();
  const [filterType, setFilterType] = useState('all');
  const [filterZone, setFilterZone] = useState('all');
  const [filterStatus, setFilterStatus] = useState('all');
  const [selected, setSelected] = useState<Component | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [promoteTarget, setPromoteTarget] = useState<Component | null>(null);
  const [showRegisterComponent, setShowRegisterComponent] = useState(false);

  const meQ = useCurrentUser();
  const isAdmin = meQ.data?.is_admin === true;

  // Use the shared hook (which polls every 30s) rather than an inline query, so
  // the list stays fresh and the ['components'] key is defined in one place.
  const componentsQ = useComponents();
  const zonesQ = useQuery({ queryKey: QUERY_KEYS.zones, queryFn: fetchZones });

  const testMut = useMutation({
    mutationFn: (id: string) => testComponent(id),
    onSuccess: (res) => {
      if (res.ok) {
        toast.success(res.message ?? 'Connection successful');
        // The test (re)connects and clears the source's error status server-side,
        // so refresh the list to reflect the recovered status.
        void qc.invalidateQueries({ queryKey: QUERY_KEYS.components });
      } else {
        toast.error(res.message ?? 'Connection failed');
      }
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const deleteMut = useMutation({
    mutationFn: (id: string) => deleteComponent(id),
    onSuccess: () => {
      toast.success('Component removed');
      setSelected(null);
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.components });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const promoteMut = useMutation({
    mutationFn: ({ id, body }: { id: string; body: PromoteRequest; rearm: boolean }) =>
      promoteComponent(id, body),
    onSuccess: (_res, vars) => {
      // Keep the action's vocabulary through the flow: the button said
      // Promote/Re-arm, the toast says Promoted/Re-armed.
      toast.success(
        vars.rearm
          ? `Component re-armed — ${vars.id} credential rotated.`
          : `Component promoted — ${vars.id} is now armed.`
      );
      setPromoteTarget(null);
      // Flip the row to armed and refresh the credential-status views.
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.components });
      void qc.invalidateQueries({ queryKey: ['credential-status'] });
    },
    onError: (e: Error) => {
      // The backend refuses a malformed/inline reference with a 400; surface
      // what to fix rather than a bare message.
      if (e instanceof ApiRequestError && e.status === 400) {
        toast.error(`Promotion rejected: ${e.message}`);
        return;
      }
      toast.error(e.message);
    },
  });

  // Register (create) is an admin-only inline affordance (moved here from the
  // retired Admin "Components" tab, session admin-nav-consolidation). It is gated
  // client-side on isAdmin to match the server-side governed create endpoint,
  // which remains the real enforcement.
  const registerComponentMut = useMutation({
    mutationFn: (data: { id: string; type: string; name: string }) => createComponent(data),
    onSuccess: (comp) => {
      // The component is registered INERT — credential-less, in the unassigned
      // zone, under the read-only floor. Point the operator at the next
      // governance steps rather than implying it is ready to use.
      toast.success(
        `Component "${comp.id}" registered (inert). Assign it a zone (below) and promote it to supply credentials before it can act.`
      );
      setShowRegisterComponent(false);
      // A new registration lands unassigned; refresh the dependent lists so it
      // surfaces in the unassigned pool and the component views.
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.components });
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.unassigned });
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.componentZones });
    },
    onError: (e: Error) => {
      // Duplicate id is a 409 from the governed create endpoint. The
      // credential-rejection 400 is defended against here even though this form
      // sends no config and so cannot trigger it.
      if (e instanceof ApiRequestError && e.status === 409) {
        toast.error('A component with that ID already exists. Choose a different ID.');
        return;
      }
      if (e instanceof ApiRequestError && e.status === 400) {
        toast.error(`Registration rejected: ${e.message}`);
        return;
      }
      toast.error(e.message);
    },
  });

  const components = componentsQ.data ?? [];
  const zones = zonesQ.data ?? [];

  // Render the detail card from the live list so status/last_error stay in sync
  // after a Test Connection refreshes the data (the selected copy is stale).
  const selectedLive = selected ? (components.find((s) => s.id === selected.id) ?? selected) : null;

  const types = [...new Set(components.map((s) => s.type))];

  const filtered = components.filter((s) => {
    if (filterType !== 'all' && s.type !== filterType) return false;
    if (filterZone !== 'all' && s.zone !== filterZone) return false;
    if (filterStatus !== 'all' && s.status !== filterStatus) return false;
    return true;
  });

  if (componentsQ.isLoading) return <LoadingPage />;

  if (componentsQ.isError) {
    return (
      <>
        <Header title="Components" />
        <PageContainer>
          <QueryError
            error={componentsQ.error}
            onRetry={() => void componentsQ.refetch()}
            resourceLabel="components"
          />
        </PageContainer>
      </>
    );
  }

  return (
    <>
      <Header
        title="Components"
        actions={
          <div className="flex gap-2">
            {isAdmin && (
              <Button size="sm" onClick={() => setShowRegisterComponent(true)}>
                + Register Component
              </Button>
            )}
          </div>
        }
      />
      <PageContainer>
        <div className="mb-4 flex gap-2">
          <Select value={filterType} onValueChange={setFilterType}>
            <SelectTrigger className="h-8 w-36 text-xs">
              <SelectValue placeholder="All Types" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Types</SelectItem>
              {types.map((t) => (
                <SelectItem key={t} value={t}>
                  {t}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={filterZone} onValueChange={setFilterZone}>
            <SelectTrigger className="h-8 w-40 text-xs">
              <SelectValue placeholder="All Zones" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Zones</SelectItem>
              {zones.map((z) => (
                <SelectItem key={z.id} value={z.id}>
                  {z.id}
                </SelectItem>
              ))}
              <SelectItem value="unassigned">Unassigned</SelectItem>
            </SelectContent>
          </Select>
          <Select value={filterStatus} onValueChange={setFilterStatus}>
            <SelectTrigger className="h-8 w-36 text-xs">
              <SelectValue placeholder="All Statuses" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Statuses</SelectItem>
              <SelectItem value="connected">Connected</SelectItem>
              <SelectItem value="disconnected">Disconnected</SelectItem>
              <SelectItem value="error">Error</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {filtered.length === 0 ? (
          <EmptyState
            icon={Database}
            title="No components"
            description="No components match the current filters."
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Component</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Zone</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Arming</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map((s) => (
                <TableRow
                  key={s.id}
                  className="cursor-pointer"
                  onClick={() => setSelected(s)}
                  data-state={selected?.id === s.id ? 'selected' : undefined}
                >
                  <TableCell className="font-mono text-sm">{s.id}</TableCell>
                  <TableCell>
                    <Badge variant="secondary">{s.type}</Badge>
                  </TableCell>
                  <TableCell>
                    {s.zone ? (
                      <span className="text-sm">{s.zone}</span>
                    ) : (
                      <Badge variant="warning">⚠ unassigned</Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    <StatusDot status={s.status} />
                  </TableCell>
                  <TableCell>
                    <ArmBadge armed={s.armed} />
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={(e) => {
                        e.stopPropagation();
                        setSelected(s);
                      }}
                    >
                      View
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}

        {selected && selectedLive && (
          <Card className="mt-4">
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">{selectedLive.id}</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <p className="text-xs text-muted-foreground">Type</p>
                  <p>{selectedLive.type}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Zone</p>
                  <p>{selectedLive.zone ?? '—'}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Status</p>
                  <StatusDot status={selectedLive.status} />
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Last Sync</p>
                  <p>
                    {selectedLive.last_sync_at
                      ? new Date(selectedLive.last_sync_at).toLocaleString()
                      : '—'}
                  </p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Arming</p>
                  <div className="flex items-center gap-2">
                    <ArmBadge armed={selectedLive.armed} />
                    {selectedLive.armed && selectedLive.provider && (
                      <span className="text-xs text-muted-foreground">{selectedLive.provider}</span>
                    )}
                  </div>
                </div>
              </div>
              {selectedLive.last_error && (
                <p
                  className={
                    selectedLive.status === 'degraded'
                      ? 'rounded bg-yellow-500/10 px-2 py-1 text-xs text-yellow-700 dark:text-yellow-500'
                      : 'rounded bg-destructive/10 px-2 py-1 text-xs text-destructive'
                  }
                >
                  {selectedLive.last_error}
                </p>
              )}
              <div className="flex gap-2 pt-1">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={testMut.isPending}
                  onClick={() => testMut.mutate(selected.id)}
                >
                  Test Connection
                </Button>
                {isAdmin && (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setPromoteTarget(selectedLive)}
                  >
                    {selectedLive.armed ? 'Re-arm' : 'Promote'}
                  </Button>
                )}
                {isAdmin && (
                  <Button size="sm" variant="destructive" onClick={() => setConfirmDelete(true)}>
                    Remove
                  </Button>
                )}
              </div>
            </CardContent>
          </Card>
        )}

        {/* Admin-only zone-assignment console, relocated inline from the retired
            Admin "Components" tab (session admin-nav-consolidation). Gated
            client-side on isAdmin to match the server-side governed endpoints,
            which remain the real enforcement. */}
        {isAdmin && (
          <div className="mt-8">
            <h2 className="mb-3 text-sm font-semibold text-muted-foreground">Zone assignments</h2>
            <ComponentZoneAssign />
          </div>
        )}

        <ConfirmDialog
          open={confirmDelete}
          onOpenChange={setConfirmDelete}
          title={`Remove ${selected?.id}`}
          description="This will remove the component and its graph nodes. This cannot be undone."
          confirmLabel="Remove"
          variant="destructive"
          onConfirm={() => selected && deleteMut.mutate(selected.id)}
        />

        {promoteTarget && (
          <PromoteComponentForm
            open
            onOpenChange={(o) => {
              if (!o) setPromoteTarget(null);
            }}
            component={promoteTarget}
            onSubmit={(body) =>
              promoteMut.mutate({ id: promoteTarget.id, body, rearm: promoteTarget.armed })
            }
            isLoading={promoteMut.isPending}
          />
        )}

        <ComponentRegisterForm
          key={showRegisterComponent ? 'open' : 'closed'}
          open={showRegisterComponent}
          onOpenChange={setShowRegisterComponent}
          onSubmit={(data) => registerComponentMut.mutate(data)}
          isLoading={registerComponentMut.isPending}
        />
      </PageContainer>
    </>
  );
}
