// Link targets a run finding reaches its subject node and its evidence by.
// Kept apart from the components that use them so the component files export
// only components (react-refresh).

// graphNodeHref links a finding to its subject node on the Graph page, which
// selects the node named by ?node=.
export function graphNodeHref(nodeId: string): string {
  return `/graph?node=${encodeURIComponent(nodeId)}`;
}

// evidenceHref links a finding to the run's session where it was established.
export function evidenceHref(sessionId: string): string {
  return `/chat/${encodeURIComponent(sessionId)}`;
}

// autoJoeRunHref links a run pointer — the Findings page's per-scope run
// section — to that run's reading on the Auto Joe page, which marks the run
// named by ?run=.
export function autoJoeRunHref(runId: string): string {
  return `/auto-joe?run=${encodeURIComponent(runId)}`;
}
