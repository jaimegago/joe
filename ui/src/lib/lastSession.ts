// Remembers the last chat session the user had in view, so returning to the
// Chat tab (the sidebar link points at the bare /chat route) reopens it instead
// of dropping the user on a blank "New chat".
//
// Like the auth token (see api/tokenStorage.ts) this lives in sessionStorage
// ONLY: tab-scoped, cleared when the tab closes. That lifetime matches the
// credential's, so it never outlives the session it belongs to and never leaks
// a session id to disk. A stale id (deleted session, or a different user in the
// same tab) is harmless — ChatPage clears it and falls back to a blank chat
// when the remembered session fails to load.
const KEY = 'joe.chat.lastSession';

export function loadLastSession(): string | null {
  try {
    return sessionStorage.getItem(KEY);
  } catch {
    return null;
  }
}

export function saveLastSession(id: string): void {
  try {
    sessionStorage.setItem(KEY, id);
  } catch {
    // best-effort; a failure just means no restore-on-return
  }
}

export function clearLastSession(): void {
  try {
    sessionStorage.removeItem(KEY);
  } catch {
    // ignore
  }
}
