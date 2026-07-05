import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { QueryError } from '@/components/common/QueryError';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { SettingsTab } from '@/components/llm/SettingsTab';
import { UsageTab } from '@/components/llm/UsageTab';
import { ProvidersTab } from '@/components/llm/ProvidersTab';
import { useLLMSettings, useLLMProviders } from '@/hooks/useLLM';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { Cpu } from 'lucide-react';

// LLMSettingsPage is admin-only. It mirrors AdminPage's structure: a
// header, a page container, tabs for the separate concerns (settings,
// usage, providers), and a page-level loading fallback while the queries
// the page depends on resolve.
export function LLMSettingsPage() {
  const settingsQ = useLLMSettings();
  const providersQ = useLLMProviders();
  const meQ = useCurrentUser();

  if (settingsQ.isLoading || providersQ.isLoading) return <LoadingPage />;

  // A failed settings/providers fetch is a genuine error, not an empty config —
  // render an actionable error panel with a retry rather than "No settings".
  if (settingsQ.isError || providersQ.isError) {
    const failed = settingsQ.isError ? settingsQ : providersQ;
    return (
      <>
        <Header title="LLM Settings" />
        <PageContainer>
          <QueryError
            error={failed.error}
            onRetry={() => {
              if (settingsQ.isError) void settingsQ.refetch();
              if (providersQ.isError) void providersQ.refetch();
            }}
            resourceLabel="LLM settings"
          />
        </PageContainer>
      </>
    );
  }

  const settings = settingsQ.data;
  const providers = providersQ.data;
  const isAdmin = meQ.data?.is_admin ?? false;

  return (
    <>
      <Header title="LLM Settings" />
      <PageContainer>
        <Tabs defaultValue="settings">
          <TabsList className="mb-4">
            <TabsTrigger value="settings">Settings</TabsTrigger>
            <TabsTrigger value="usage">Usage</TabsTrigger>
            <TabsTrigger value="providers">Providers</TabsTrigger>
          </TabsList>

          <TabsContent value="settings">
            {settings ? (
              <SettingsTab
                activeModel={settings.active_model}
                costLimits={settings.cost_limits}
                runawayCeiling={settings.runaway_ceiling}
                contextBudget={settings.context_budget}
                models={providers?.providers ?? []}
              />
            ) : (
              <EmptyState
                icon={Cpu}
                title="No settings"
                description="LLM settings are unavailable."
              />
            )}
          </TabsContent>

          <TabsContent value="usage">
            <UsageTab isAdmin={isAdmin} />
          </TabsContent>

          <TabsContent value="providers">
            {providers ? (
              <ProvidersTab providers={providers.providers} current={providers.current} />
            ) : (
              <EmptyState
                icon={Cpu}
                title="No providers"
                description="No LLM models are configured."
              />
            )}
          </TabsContent>
        </Tabs>
      </PageContainer>
    </>
  );
}
