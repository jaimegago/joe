import { useQuery } from '@tanstack/react-query';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { MetricsCard } from '@/components/dashboard/MetricsCard';
import { AlertsList } from '@/components/dashboard/AlertsList';
import { RecentSessions } from '@/components/dashboard/RecentSessions';
import { ComponentsHealth } from '@/components/dashboard/ComponentsHealth';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { LoadingSpinner } from '@/components/common/LoadingSpinner';
import { fetchComponents } from '@/api/components';
import { fetchAlerts } from '@/api/alerts';
import { fetchSessions } from '@/api/chat';
import { Button } from '@/components/ui/button';
import { RefreshCw } from 'lucide-react';

export function DashboardPage() {
  const componentsQ = useQuery({
    queryKey: ['components'],
    queryFn: fetchComponents,
    refetchInterval: 30_000,
  });
  const alertsQ = useQuery({ queryKey: ['alerts'], queryFn: fetchAlerts, refetchInterval: 30_000 });
  const sessionsQ = useQuery({
    queryKey: ['sessions', 'all', 5],
    queryFn: () => fetchSessions({ limit: 5 }),
    refetchInterval: 30_000,
  });

  const components = componentsQ.data ?? [];
  const alerts = alertsQ.data ?? [];
  const sessions = sessionsQ.data ?? [];

  const connectedCount = components.filter((s) => s.status === 'connected').length;
  const errorCount = components.filter((s) => s.status === 'error').length;
  const criticalAlerts = alerts.filter((a) => a.severity === 'critical').length;

  return (
    <>
      <Header
        title="Dashboard"
        actions={
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              void componentsQ.refetch();
              void alertsQ.refetch();
              void sessionsQ.refetch();
            }}
          >
            <RefreshCw className="mr-1 h-3 w-3" />
            Refresh
          </Button>
        }
      />
      <PageContainer>
        {/* Metrics row */}
        <div className="mb-6 grid grid-cols-3 gap-4">
          <MetricsCard
            title="Components"
            value={componentsQ.isLoading ? '…' : connectedCount}
            subLabel={
              componentsQ.isLoading
                ? 'loading'
                : errorCount > 0
                  ? `${errorCount} error${errorCount > 1 ? 's' : ''}`
                  : 'All healthy'
            }
            colorClass={errorCount > 0 ? 'text-destructive' : 'text-green-600'}
          />
          <MetricsCard
            title="Active Alerts"
            value={alertsQ.isLoading ? '…' : alerts.length}
            subLabel={
              alertsQ.isLoading
                ? 'loading'
                : criticalAlerts > 0
                  ? `${criticalAlerts} critical`
                  : 'None critical'
            }
            colorClass={criticalAlerts > 0 ? 'text-destructive' : undefined}
          />
          <MetricsCard
            title="Sessions Today"
            value={sessionsQ.isLoading ? '…' : sessions.length}
            subLabel="recent sessions"
          />
        </div>

        {/* Alerts + Sessions */}
        <div className="mb-6 grid grid-cols-2 gap-4">
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">Active Alerts</CardTitle>
            </CardHeader>
            <CardContent>
              {alertsQ.isLoading ? <LoadingSpinner size="sm" /> : <AlertsList alerts={alerts} />}
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">Recent Sessions</CardTitle>
            </CardHeader>
            <CardContent>
              {sessionsQ.isLoading ? (
                <LoadingSpinner size="sm" />
              ) : (
                <RecentSessions sessions={sessions} />
              )}
            </CardContent>
          </Card>
        </div>

        {/* Components health */}
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-medium">Components Health</CardTitle>
          </CardHeader>
          <CardContent>
            {componentsQ.isLoading ? (
              <LoadingSpinner size="sm" />
            ) : components.length === 0 ? (
              <p className="text-sm text-muted-foreground">No components configured</p>
            ) : (
              <ComponentsHealth components={components} />
            )}
          </CardContent>
        </Card>
      </PageContainer>
    </>
  );
}
