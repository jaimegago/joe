import { AlertTriangle, Lock } from 'lucide-react';
import { EmptyState } from '@/components/common/EmptyState';
import { ApiRequestError } from '@/api/client';

interface QueryErrorProps {
  // The error thrown by the failed query (react-query's `error`). Discriminated
  // for the auth/authorization/unconfigured cases so a permission or
  // config problem is not misreported as a generic connectivity failure.
  error: unknown;
  // onRetry re-runs the failed query (react-query's `refetch`). Omitted for a
  // 403/401/503, which retrying cannot resolve.
  onRetry?: () => void;
  // resourceLabel names the thing that failed to load (e.g. "components",
  // "zones") so the copy reads naturally. Defaults to a generic noun.
  resourceLabel?: string;
}

// QueryError is the shared failed-query panel: it turns a rejected react-query
// into an actionable error state with a Retry, discriminating an authorization
// denial (403) and an expired session (401) — which retrying cannot fix — from
// a genuine connectivity/server failure, which offers a retry. It mirrors the
// bespoke error mapping GraphPage/UsersPage grew, factored out so every list
// page reports a failed load the same way instead of a misleading empty state.
export function QueryError({ error, onRetry, resourceLabel = 'data' }: QueryErrorProps) {
  if (error instanceof ApiRequestError) {
    if (error.status === 403) {
      return (
        <EmptyState
          icon={Lock}
          title="No access"
          description={`Your account doesn't have permission to view ${resourceLabel}. Ask an administrator to grant access.`}
        />
      );
    }
    if (error.status === 401) {
      return (
        <EmptyState
          icon={Lock}
          title="Session expired"
          description="Your session has expired. Please sign in again."
        />
      );
    }
    if (error.status === 503) {
      return (
        <EmptyState
          icon={AlertTriangle}
          title="Temporarily unavailable"
          description={`The ${resourceLabel} service is not available right now. It may require additional configuration.`}
        />
      );
    }
  }

  return (
    <EmptyState
      icon={AlertTriangle}
      title={`Couldn't load ${resourceLabel}`}
      description="The request failed. This may be a transient error — try again."
      action={onRetry ? { label: 'Retry', onClick: onRetry } : undefined}
    />
  );
}
