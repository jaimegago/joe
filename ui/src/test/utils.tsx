import type { ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

// createWrapper builds an isolated QueryClient + provider for a single
// test. Retries are off so a rejected query surfaces immediately rather
// than after the production retry budget. The client is returned so a test
// can spy on it (e.g. invalidateQueries).
export function createWrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  }
  return { qc, Wrapper };
}
