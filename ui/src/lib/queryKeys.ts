// Centralized react-query key roots. Raw string literals like ['sessions']
// were duplicated across a dozen files; a single typo silently splits the cache
// (a query caches under one spelling, an invalidation targets another, and the
// list never refreshes). These consts are the one place each high-traffic key
// is spelled, so a query definition and its invalidation can never drift apart.
//
// Each value is the KEY ROOT. A query that needs a parameterized key spreads
// the root and appends its params, e.g.
//   queryKey: [...QUERY_KEYS.session, id]
// so an invalidation of QUERY_KEYS.session still matches by prefix. `as const`
// keeps the tuples literal for exact-match typing.
export const QUERY_KEYS = {
  sessions: ['sessions'] as const,
  session: ['session'] as const,
  components: ['components'] as const,
  zones: ['zones'] as const,
  policies: ['policies'] as const,
  regime: ['regime'] as const,
  unassigned: ['unassigned'] as const,
  componentZones: ['component-zones'] as const,
  llmSettings: ['llm-settings'] as const,
  readPosture: ['read-posture'] as const,
} as const;
