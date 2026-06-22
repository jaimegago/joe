import type { Session } from '@/api/types';

// sessionLabel is the human label for a session row: its title, else its
// summary, else a generic fallback for an untitled, unsummarized session.
export function sessionLabel(s: Session): string {
  return s.title ?? s.summary ?? 'Untitled session';
}

// formatOwner renders a principal ("user:alice@example.com") as the bare
// identity for the "owned by" label. Falls back when the owner is absent.
export function formatOwner(principal?: string): string {
  if (!principal) return 'another user';
  return principal.replace(/^user:/, '');
}
