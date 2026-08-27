import { lazy, Suspense, useEffect } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';
import { AppShell } from '@/components/layout/AppShell';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { AuthProvider } from '@/auth/AuthContext';
import { AuthGate } from '@/auth/AuthGate';
import { RequireAdmin } from '@/auth/RequireAdmin';
import { RequireZonedPosture } from '@/auth/RequireZonedPosture';
import { ErrorBoundary } from '@/components/common/ErrorBoundary';

const GraphPage = lazy(() => import('@/pages/GraphPage').then((m) => ({ default: m.GraphPage })));
const ComponentsPage = lazy(() =>
  import('@/pages/ComponentsPage').then((m) => ({ default: m.ComponentsPage }))
);
const ChatPage = lazy(() => import('@/pages/ChatPage').then((m) => ({ default: m.ChatPage })));
const SessionsPage = lazy(() =>
  import('@/pages/SessionsPage').then((m) => ({ default: m.SessionsPage }))
);
const UsersPage = lazy(() => import('@/pages/UsersPage').then((m) => ({ default: m.UsersPage })));
const CredentialStatusPage = lazy(() =>
  import('@/pages/CredentialStatusPage').then((m) => ({ default: m.CredentialStatusPage }))
);
const LLMSettingsPage = lazy(() =>
  import('@/pages/LLMSettingsPage').then((m) => ({ default: m.LLMSettingsPage }))
);
// Admin-only surfaces, each its own route under the Admin nav subgroup. The
// former in-page Admin tab row (AdminPage tab-host) was removed in session
// admin-nav-consolidation; every former tab is now a standalone route here.
const ZonesAdminPage = lazy(() =>
  import('@/pages/admin/ZonesAdminPage').then((m) => ({ default: m.ZonesAdminPage }))
);
const PoliciesAdminPage = lazy(() =>
  import('@/pages/admin/PoliciesAdminPage').then((m) => ({ default: m.PoliciesAdminPage }))
);
const ReadPostureAdminPage = lazy(() =>
  import('@/pages/admin/ReadPostureAdminPage').then((m) => ({ default: m.ReadPostureAdminPage }))
);
const AutonomousReadsAdminPage = lazy(() =>
  import('@/pages/admin/AutonomousReadsAdminPage').then((m) => ({
    default: m.AutonomousReadsAdminPage,
  }))
);
const SkillsAdminPage = lazy(() =>
  import('@/pages/admin/SkillsAdminPage').then((m) => ({ default: m.SkillsAdminPage }))
);
const AdminsAdminPage = lazy(() =>
  import('@/pages/admin/AdminsAdminPage').then((m) => ({ default: m.AdminsAdminPage }))
);

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 2,
    },
  },
});

export function App() {
  useEffect(() => {
    document.title = 'Joe';
  }, []);
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <BrowserRouter>
          <AuthGate>
            <ErrorBoundary>
              <Routes>
                <Route path="/" element={<AppShell />}>
                  <Route index element={<Navigate to="/chat" replace />} />
                  <Route
                    path="graph"
                    element={
                      <Suspense fallback={<LoadingPage />}>
                        <GraphPage />
                      </Suspense>
                    }
                  />
                  <Route
                    path="components"
                    element={
                      <Suspense fallback={<LoadingPage />}>
                        <ComponentsPage />
                      </Suspense>
                    }
                  />
                  <Route
                    path="chat"
                    element={
                      <Suspense fallback={<LoadingPage />}>
                        <ChatPage />
                      </Suspense>
                    }
                  />
                  <Route
                    path="chat/:sessionId"
                    element={
                      <Suspense fallback={<LoadingPage />}>
                        <ChatPage />
                      </Suspense>
                    }
                  />
                  <Route
                    path="sessions"
                    element={
                      <Suspense fallback={<LoadingPage />}>
                        <SessionsPage />
                      </Suspense>
                    }
                  />
                  <Route
                    path="credentials"
                    element={
                      <RequireAdmin>
                        <Suspense fallback={<LoadingPage />}>
                          <CredentialStatusPage />
                        </Suspense>
                      </RequireAdmin>
                    }
                  />
                  {/* Admin subgroup routes. /admin lands on the first child. */}
                  <Route path="admin" element={<Navigate to="/admin/zones" replace />} />
                  <Route
                    path="admin/zones"
                    element={
                      <RequireAdmin>
                        <Suspense fallback={<LoadingPage />}>
                          <ZonesAdminPage />
                        </Suspense>
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path="admin/policies"
                    element={
                      <RequireAdmin>
                        <RequireZonedPosture>
                          <Suspense fallback={<LoadingPage />}>
                            <PoliciesAdminPage />
                          </Suspense>
                        </RequireZonedPosture>
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path="admin/read-posture"
                    element={
                      <RequireAdmin>
                        <Suspense fallback={<LoadingPage />}>
                          <ReadPostureAdminPage />
                        </Suspense>
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path="admin/autonomous-reads"
                    element={
                      <RequireAdmin>
                        <Suspense fallback={<LoadingPage />}>
                          <AutonomousReadsAdminPage />
                        </Suspense>
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path="admin/skills"
                    element={
                      <RequireAdmin>
                        <Suspense fallback={<LoadingPage />}>
                          <SkillsAdminPage />
                        </Suspense>
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path="admin/admins"
                    element={
                      <RequireAdmin>
                        <Suspense fallback={<LoadingPage />}>
                          <AdminsAdminPage />
                        </Suspense>
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path="admin/users"
                    element={
                      <RequireAdmin>
                        <Suspense fallback={<LoadingPage />}>
                          <UsersPage />
                        </Suspense>
                      </RequireAdmin>
                    }
                  />
                  <Route
                    path="llm-settings"
                    element={
                      <RequireAdmin>
                        <Suspense fallback={<LoadingPage />}>
                          <LLMSettingsPage />
                        </Suspense>
                      </RequireAdmin>
                    }
                  />
                </Route>
              </Routes>
            </ErrorBoundary>
          </AuthGate>
        </BrowserRouter>
      </AuthProvider>
      <Toaster position="top-right" />
    </QueryClientProvider>
  );
}
