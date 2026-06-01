import { useState } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { UsageTable } from './UsageTable';
import { useUsageAggregate, usePerModelUsage, usePerPrincipalUsage } from '@/hooks/useLLM';
import type { UsageWindowParam } from '@/api/llm';

const WINDOWS: { value: UsageWindowParam; label: string }[] = [
  { value: 'hour', label: 'This hour' },
  { value: 'day', label: 'Today' },
  { value: 'month', label: 'This month' },
];

function WindowSelect({
  value,
  onChange,
}: {
  value: UsageWindowParam;
  onChange: (v: UsageWindowParam) => void;
}) {
  return (
    <Select value={value} onValueChange={(v) => onChange(v as UsageWindowParam)}>
      <SelectTrigger className="w-40">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {WINDOWS.map((w) => (
          <SelectItem key={w.value} value={w.value}>{w.label}</SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

// UsageTab renders aggregate rollups, a per-model breakdown over a
// selectable window, and — only for admins — a per-principal breakdown.
// The per-principal endpoint is admin-gated server-side; the hook is
// passed enabled=isAdmin so a non-admin never requests it.
export function UsageTab({ isAdmin }: { isAdmin: boolean }) {
  const [modelWindow, setModelWindow] = useState<UsageWindowParam>('day');
  const [principalWindow, setPrincipalWindow] = useState<UsageWindowParam>('day');

  const aggregateQ = useUsageAggregate();
  const perModelQ = usePerModelUsage(modelWindow);
  const perPrincipalQ = usePerPrincipalUsage(principalWindow, isAdmin);

  if (aggregateQ.isLoading) return <LoadingPage />;
  const aggregate = aggregateQ.data;

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">Today</CardTitle>
        </CardHeader>
        <CardContent>
          <UsageTable rows={aggregate?.today ?? []} />
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">This week</CardTitle>
        </CardHeader>
        <CardContent>
          <UsageTable rows={aggregate?.week ?? []} />
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">This month</CardTitle>
        </CardHeader>
        <CardContent>
          <UsageTable rows={aggregate?.month ?? []} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex-row items-center justify-between pb-2">
          <CardTitle className="text-sm">Per model</CardTitle>
          <WindowSelect value={modelWindow} onChange={setModelWindow} />
        </CardHeader>
        <CardContent>
          {perModelQ.isLoading ? (
            <LoadingPage />
          ) : (
            <UsageTable rows={perModelQ.data?.rows ?? []} dimension="model" />
          )}
        </CardContent>
      </Card>

      {isAdmin && (
        <Card>
          <CardHeader className="flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm">Per principal</CardTitle>
            <WindowSelect value={principalWindow} onChange={setPrincipalWindow} />
          </CardHeader>
          <CardContent>
            {perPrincipalQ.isLoading ? (
              <LoadingPage />
            ) : (
              <UsageTable rows={perPrincipalQ.data?.rows ?? []} dimension="principal" />
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
