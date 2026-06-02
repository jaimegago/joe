// Bearer-token persistence for the dev / break-glass login (Stream H1).
//
// The token lives in sessionStorage ONLY — never localStorage, never a
// cookie, never on disk. sessionStorage is scoped to the tab and cleared
// when it closes, which is the right lifetime for a break-glass credential.
// The in-memory copy on apiClient is the source of truth for requests; this
// module is purely the rehydrate-across-reload backing store.
const TOKEN_KEY = 'joe.auth.token';

// loadToken returns the persisted token, or null if none is stored or
// sessionStorage is unavailable (e.g. SSR / privacy mode).
export function loadToken(): string | null {
  try {
    return sessionStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function saveToken(token: string): void {
  try {
    sessionStorage.setItem(TOKEN_KEY, token);
  } catch {
    // best-effort; a failure just means no cross-reload persistence
  }
}

export function clearStoredToken(): void {
  try {
    sessionStorage.removeItem(TOKEN_KEY);
  } catch {
    // ignore
  }
}
