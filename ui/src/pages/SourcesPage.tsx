import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import {
  Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { fetchSources, testSource, deleteSource } from '@/api/sources';
import { fetchZones } from '@/api/security';
import { STATUS_CONFIG } from '@/lib/constants';
import type { Source } from '@/api/types';
import { Database, RefreshCw } from 'lucide-react';

function StatusDot({ status }: { status: Source['status'] }) {
  const cfg = STATUS_CONFIG[status] ?? STATUS_CONFIG.unknown;
  return (
    <span className="flex items-center gap-1.5 text-sm" style={{ color: cfg.color }}>
      {cfg.dot} {cfg.label}
    </span>
  );
}

export function SourcesPage() {
  const qc = useQueryClient();
  const [filterType, setFilterType] = useState('all');
  const [filterZone, setFilterZone] = useState('all');
  const [filterStatus, setFilterStatus] = useState('all');
  const [selected, setSelected] = useState<Source | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const sourcesQ = useQuery({ queryKey: ['sources'], queryFn: fetchSources });
  const zonesQ = useQuery({ queryKey: ['zones'], queryFn: fetchZones });

  const testMut = useMutation({
    mutationFn: (id: string) => testSource(id),
    onSuccess: (res) => {
      if (res.ok) {
        toast.success(res.message ?? 'Connection successful');
        // The test (re)connects and clears the source's error status server-side,
        // so refresh the list to reflect the recovered status.
        void qc.invalidateQueries({ queryKey: ['sources'] });
      } else {
        toast.error(res.message ?? 'Connection failed');
      }
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const deleteMut = useMutation({
    mutationFn: (id: string) => deleteSource(id),
    onSuccess: () => {
      toast.success('Source removed');
      setSelected(null);
      void qc.invalidateQueries({ queryKey: ['sources'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const sources = sourcesQ.data ?? [];
  const zones = zonesQ.data ?? [];

  // Render the detail card from the live list so status/last_error stay in sync
  // after a Test Connection refreshes the data (the selected copy is stale).
  const selectedLive = selected
    ? (sources.find((s) => s.id === selected.id) ?? selected)
    : null;

  const types = [...new Set(sources.map((s) => s.type))];

  const filtered = sources.filter((s) => {
    if (filterType !== 'all' && s.type !== filterType) return false;
    if (filterZone !== 'all' && s.zone !== filterZone) return false;
    if (filterStatus !== 'all' && s.status !== filterStatus) return false;
    return true;
  });

  if (sourcesQ.isLoading) return <LoadingPage />;

  return (
    <>
      <Header
        title="Sources"
        actions={
          <Button variant="outline" size="sm" onClick={() => void sourcesQ.refetch()}>
            <RefreshCw className="mr-1 h-3 w-3" />
            Refresh
          </Button>
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
              {types.map((t) => <SelectItem key={t} value={t}>{t}</SelectItem>)}
            </SelectContent>
          </Select>
          <Select value={filterZone} onValueChange={setFilterZone}>
            <SelectTrigger className="h-8 w-40 text-xs">
              <SelectValue placeholder="All Zones" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All Zones</SelectItem>
              {zones.map((z) => <SelectItem key={z.id} value={z.id}>{z.id}</SelectItem>)}
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
          <EmptyState icon={Database} title="No sources" description="No sources match the current filters." />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Source</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Zone</TableHead>
                <TableHead>Status</TableHead>
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
                  <TableCell><StatusDot status={s.status} /></TableCell>
                  <TableCell>
                    <Button variant="ghost" size="sm" onClick={(e) => { e.stopPropagation(); setSelected(s); }}>
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
              <CardTitle className="text-sm font-medium">
                {selectedLive.id}
              </CardTitle>
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
                  <p>{selectedLive.last_sync_at ? new Date(selectedLive.last_sync_at).toLocaleString() : '—'}</p>
                </div>
              </div>
              {selectedLive.last_error && (
                <p className="rounded bg-destructive/10 px-2 py-1 text-xs text-destructive">
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
                <Button size="sm" variant="destructive" onClick={() => setConfirmDelete(true)}>
                  Remove
                </Button>
              </div>
            </CardContent>
          </Card>
        )}

        <ConfirmDialog
          open={confirmDelete}
          onOpenChange={setConfirmDelete}
          title={`Remove ${selected?.id}`}
          description="This will remove the source and its graph nodes. This cannot be undone."
          confirmLabel="Remove"
          variant="destructive"
          onConfirm={() => selected && deleteMut.mutate(selected.id)}
        />
      </PageContainer>
    </>
  );
}
