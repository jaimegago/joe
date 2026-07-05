import { useCallback, useEffect, useRef } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { fetchSession } from '@/api/chat';
import { ApiRequestError } from '@/api/client';
import { QUERY_KEYS } from '@/lib/queryKeys';
import type { Session } from '@/api/types';

// How long, after a session id first comes into view, to keep polling for its
// async-generated title before giving up (the backend's title goroutine has a
// ~30s timeout; this is that plus a buffer). Bounded by elapsed wall-clock —
// NOT by a poll/update counter, which mutation cache-writes would inflate.
const TITLE_AWAIT_MS = 40_000;
const TITLE_POLL_INTERVAL_MS = 3000;

// SessionStatus is the explicit lifecycle of the session-in-view's metadata.
// Everything the UI gates on (owner controls, read-only viewer, gone empty
// state) derives from this single value rather than from a tangle of
// data?/error? checks scattered across the page.
export type SessionStatus =
  | 'loading' // no data yet (or a benign transient 404 on a just-created id)
  | 'owner' // the caller owns it — full controls
  | 'reader' // a non-owner reading someone else's session — read-only
  | 'gone'; // 404 — a deleted/never-existed id

export interface UseSessionResult {
  status: SessionStatus;
  session: Session | undefined;
  isOwner: boolean;
  isReader: boolean;
  isLinkedToIncident: boolean;
  // isIncidentSession is true when THIS session is the incident master (the one
  // promoted in place, §12.3) — distinct from isLinkedToIncident, which marks a
  // participant. The "Incident Session" badge and the resolve/lifecycle controls
  // key off it. incidentState is its lifecycle position (undefined otherwise).
  isIncidentSession: boolean;
  incidentState: Session['incident_state'];
  // applyUpdate writes a mutation's full response into the cache for the session
  // it actually changed (updated.id is authoritative), then refreshes the lists.
  // Keying on updated.id — not the hook's current id — is what keeps a toggle
  // that resolves after the user navigated away from stamping the wrong session.
  applyUpdate: (updated: Session) => void;
}

function is404(err: unknown): boolean {
  return err instanceof ApiRequestError && err.status === 404;
}

// deriveSessionStatus is the pure status decision, factored out so every branch
// is unit-testable without driving React Query. `had404` is whether the current
// error is a 404; `locallyCreated` exempts a just-minted id from 'gone'.
export function deriveSessionStatus(
  data: Session | undefined,
  had404: boolean,
  locallyCreated: boolean
): SessionStatus {
  // A 404 means the session was deleted or never existed → 'gone'. (A
  // just-created id is exempt: its first fetch can lose the read-after-write
  // race, which is benign, not gone.)
  if (had404 && !locallyCreated) return 'gone';
  if (data?.read_only === false) return 'owner';
  if (data?.read_only === true) return 'reader';
  // Pending, or a just-created id whose first fetch transiently 404'd.
  return 'loading';
}

// nextTitlePollMs is the pure bounded title-await decision (the refetchInterval
// body), factored out for deterministic testing. Polls every 3s ONLY while the
// session is still untitled, the fetch is succeeding, and we are within the
// elapsed cap — never inflated by mutation cache-writes (it keys off wall-clock,
// not a poll/update counter).
export function nextTitlePollMs(args: {
  hasTitle: boolean;
  isError: boolean;
  elapsedMs: number;
}): number | false {
  if (args.hasTitle) return false;
  if (args.isError) return false;
  if (args.elapsedMs > TITLE_AWAIT_MS) return false;
  return TITLE_POLL_INTERVAL_MS;
}

// useSession is the single source of truth for the in-view session's METADATA
// (title, ownership, incident linkage). It owns the ['session', id] query, derives an
// explicit status, and runs a robust bounded title-await. It deliberately knows
// nothing about routing, the transcript, or last-session restore — those stay in
// ChatPage/useChat.
//
// opts.locallyCreated marks an id this tab just minted via createSession. The
// row is written before createSession returns, but a metadata fetch can still
// briefly lose a read-after-write race and 404; for such an id a 404 is treated
// as transient ('loading'), never 'gone', so a fresh session the user is
// actively chatting in is never rendered as dead.
export function useSession(
  sessionId: string | null,
  opts?: { locallyCreated?: boolean }
): UseSessionResult {
  const qc = useQueryClient();
  const locallyCreated = opts?.locallyCreated ?? false;

  // Per-id elapsed clock for the title-await, stamped in an effect (Date.now() is
  // impure, so not during render) and re-stamped whenever the id changes — so
  // switching sessions restarts the window and the previous session's clock can
  // never leak into the new one.
  const titleClock = useRef<{ id: string | null; startedAt: number }>({
    id: null,
    startedAt: 0,
  });
  useEffect(() => {
    titleClock.current = { id: sessionId, startedAt: Date.now() };
  }, [sessionId]);

  const q = useQuery({
    queryKey: [...QUERY_KEYS.session, sessionId],
    queryFn: () => fetchSession(sessionId!),
    enabled: sessionId != null,
    // Bounded title-await: poll only while the session is still untitled and the
    // fetch is succeeding, capped by elapsed time. refetchIntervalInBackground
    // keeps it running when the tab is hidden — the previous interval silently
    // paused in background tabs, which is why titles often never appeared.
    refetchInterval: (query) => {
      const clock = titleClock.current;
      // Until the effect stamps this id's start, treat elapsed as 0 (keep polling).
      const elapsedMs = clock.id === sessionId ? Date.now() - clock.startedAt : 0;
      return nextTitlePollMs({
        hasTitle: Boolean(query.state.data?.title),
        isError: query.state.status === 'error',
        elapsedMs,
      });
    },
    refetchIntervalInBackground: true,
  });

  const data = q.data;
  const status = deriveSessionStatus(data, is404(q.error), locallyCreated);

  // When the async title finally lands, refresh the browse list / dashboard so
  // their "New chat" rows pick it up live too.
  const title = data?.title;
  useEffect(() => {
    if (title) void qc.invalidateQueries({ queryKey: QUERY_KEYS.sessions });
  }, [title, qc]);

  const applyUpdate = useCallback(
    (updated: Session) => {
      qc.setQueryData([...QUERY_KEYS.session, updated.id], updated);
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.sessions });
    },
    [qc]
  );

  return {
    status,
    session: data,
    isOwner: status === 'owner',
    isReader: status === 'reader',
    isLinkedToIncident: data?.linked_incident_id != null,
    isIncidentSession: data?.type === 'incident',
    incidentState: data?.incident_state,
    applyUpdate,
  };
}
