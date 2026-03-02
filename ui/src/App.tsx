import { lazy, Suspense } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';
import { AppShell } from '@/components/layout/AppShell';
import { LoadingPage } from '@/components/common/LoadingSpinner';

const DashboardPage = lazy(() => import('@/pages/DashboardPage').then(m => ({ default: m.DashboardPage })));
const GraphPage = lazy(() => import('@/pages/GraphPage').then(m => ({ default: m.GraphPage })));
const SourcesPage = lazy(() => import('@/pages/SourcesPage').then(m => ({ default: m.SourcesPage })));
const ChatPage = lazy(() => import('@/pages/ChatPage').then(m => ({ default: m.ChatPage })));
const AdminPage = lazy(() => import('@/pages/AdminPage').then(m => ({ default: m.AdminPage })));

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 2,
    },
  },
});

export function App() {
  document.title = 'Joe';
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<AppShell />}>
            <Route index element={<Suspense fallback={<LoadingPage />}><DashboardPage /></Suspense>} />
            <Route path="graph" element={<Suspense fallback={<LoadingPage />}><GraphPage /></Suspense>} />
            <Route path="sources" element={<Suspense fallback={<LoadingPage />}><SourcesPage /></Suspense>} />
            <Route path="chat" element={<Suspense fallback={<LoadingPage />}><ChatPage /></Suspense>} />
            <Route path="chat/:sessionId" element={<Suspense fallback={<LoadingPage />}><ChatPage /></Suspense>} />
            <Route path="admin" element={<Suspense fallback={<LoadingPage />}><AdminPage /></Suspense>} />
          </Route>
        </Routes>
      </BrowserRouter>
      <Toaster position="top-right" />
    </QueryClientProvider>
  );
}
