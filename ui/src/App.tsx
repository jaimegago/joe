import { lazy, Suspense, useEffect } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';
import { AppShell } from '@/components/layout/AppShell';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { AuthProvider } from '@/auth/AuthContext';
import { AuthGate } from '@/auth/AuthGate';
import { RequireAdmin } from '@/auth/RequireAdmin';

const DashboardPage = lazy(() => import('@/pages/DashboardPage').then(m => ({ default: m.DashboardPage })));
const GraphPage = lazy(() => import('@/pages/GraphPage').then(m => ({ default: m.GraphPage })));
const SourcesPage = lazy(() => import('@/pages/SourcesPage').then(m => ({ default: m.SourcesPage })));
const ChatPage = lazy(() => import('@/pages/ChatPage').then(m => ({ default: m.ChatPage })));
const AdminPage = lazy(() => import('@/pages/AdminPage').then(m => ({ default: m.AdminPage })));
const UsersPage = lazy(() => import('@/pages/UsersPage').then(m => ({ default: m.UsersPage })));
const LLMSettingsPage = lazy(() => import('@/pages/LLMSettingsPage').then(m => ({ default: m.LLMSettingsPage })));

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
            <Routes>
              <Route path="/" element={<AppShell />}>
                <Route index element={<Suspense fallback={<LoadingPage />}><DashboardPage /></Suspense>} />
                <Route path="graph" element={<Suspense fallback={<LoadingPage />}><GraphPage /></Suspense>} />
                <Route path="sources" element={<Suspense fallback={<LoadingPage />}><SourcesPage /></Suspense>} />
                <Route path="chat" element={<Suspense fallback={<LoadingPage />}><ChatPage /></Suspense>} />
                <Route path="chat/:sessionId" element={<Suspense fallback={<LoadingPage />}><ChatPage /></Suspense>} />
                <Route path="admin" element={<RequireAdmin><Suspense fallback={<LoadingPage />}><AdminPage /></Suspense></RequireAdmin>} />
                <Route path="users" element={<RequireAdmin><Suspense fallback={<LoadingPage />}><UsersPage /></Suspense></RequireAdmin>} />
                <Route path="llm-settings" element={<RequireAdmin><Suspense fallback={<LoadingPage />}><LLMSettingsPage /></Suspense></RequireAdmin>} />
              </Route>
            </Routes>
          </AuthGate>
        </BrowserRouter>
      </AuthProvider>
      <Toaster position="top-right" />
    </QueryClientProvider>
  );
}
