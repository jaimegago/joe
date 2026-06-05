// Reserved principal-kind prefixes, mirroring internal/rbac/identity.go
// (PrefixUser/PrefixGroup/PrefixSvc). The admin REST surface rejects a grant or
// admin-add whose principal lacks one of these prefixes (rbac.HasReservedPrefix),
// so the UI validates the same rule client-side to fail fast before the request.
export const RESERVED_PREFIXES = ['user:', 'group:', 'svc:'] as const;

// hasReservedPrefix reports whether s begins with one of the reserved kind
// prefixes. Same predicate the backend enforces — kept in lockstep so a future
// prefix change is a one-line edit here.
export function hasReservedPrefix(s: string): boolean {
  return RESERVED_PREFIXES.some((p) => s.startsWith(p));
}
