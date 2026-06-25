import { lazy, Suspense, useEffect } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';
import { AppShell } from '@/components/layout/AppShell';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { AuthProvider } from '@/auth/AuthContext';
import { AuthGate } from '@/auth/AuthGate';
import { RequireAdmin } from '@/auth/RequireAdmin';

const GraphPage = lazy(() => import('@/pages/GraphPage').then(m => ({ default: m.GraphPage })));
const ComponentsPage = lazy(() => import('@/pages/ComponentsPage').then(m => ({ default: m.ComponentsPage })));
const ChatPage = lazy(() => import('@/pages/ChatPage').then(m => ({ default: m.ChatPage })));
const SessionsPage = lazy(() => import('@/pages/SessionsPage').then(m => ({ default: m.SessionsPage })));
const AdminPage = lazy(() => import('@/pages/AdminPage').then(m => ({ default: m.AdminPage })));
const UsersPage = lazy(() => import('@/pages/UsersPage').then(m => ({ default: m.UsersPage })));
const CredentialStatusPage = lazy(() => import('@/pages/CredentialStatusPage').then(m => ({ default: m.CredentialStatusPage })));
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
                <Route index element={<Navigate to="/chat" replace />} />
                <Route path="graph" element={<Suspense fallback={<LoadingPage />}><GraphPage /></Suspense>} />
                <Route path="components" element={<Suspense fallback={<LoadingPage />}><ComponentsPage /></Suspense>} />
                <Route path="chat" element={<Suspense fallback={<LoadingPage />}><ChatPage /></Suspense>} />
                <Route path="chat/:sessionId" element={<Suspense fallback={<LoadingPage />}><ChatPage /></Suspense>} />
                <Route path="sessions" element={<Suspense fallback={<LoadingPage />}><SessionsPage /></Suspense>} />
                <Route path="admin" element={<RequireAdmin><Suspense fallback={<LoadingPage />}><AdminPage /></Suspense></RequireAdmin>} />
                <Route path="users" element={<RequireAdmin><Suspense fallback={<LoadingPage />}><UsersPage /></Suspense></RequireAdmin>} />
                <Route path="credentials" element={<RequireAdmin><Suspense fallback={<LoadingPage />}><CredentialStatusPage /></Suspense></RequireAdmin>} />
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
