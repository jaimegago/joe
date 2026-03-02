export const NODE_KIND_CONFIG: Record<string, { color: string; bgColor: string; icon: string }> = {
  Deployment: { color: '#3b82f6', bgColor: '#eff6ff', icon: '🚀' },
  Service: { color: '#8b5cf6', bgColor: '#f5f3ff', icon: '🔗' },
  Pod: { color: '#60a5fa', bgColor: '#eff6ff', icon: '📦' },
  Database: { color: '#f97316', bgColor: '#fff7ed', icon: '🗄️' },
  Cache: { color: '#eab308', bgColor: '#fefce8', icon: '⚡' },
  Queue: { color: '#22c55e', bgColor: '#f0fdf4', icon: '📬' },
  External: { color: '#6b7280', bgColor: '#f9fafb', icon: '🌐' },
  Secret: { color: '#ef4444', bgColor: '#fef2f2', icon: '🔐' },
  ConfigMap: { color: '#14b8a6', bgColor: '#f0fdfa', icon: '📋' },
  Ingress: { color: '#06b6d4', bgColor: '#ecfeff', icon: '🔀' },
  Node: { color: '#84cc16', bgColor: '#f7fee7', icon: '🖥️' },
  Namespace: { color: '#a855f7', bgColor: '#faf5ff', icon: '📁' },
};

export const DEFAULT_NODE_CONFIG = { color: '#6b7280', bgColor: '#f9fafb', icon: '⚙️' };

export const EDGE_TYPE_CONFIG: Record<string, { style: string; label: string; color: string }> = {
  depends_on: { style: 'solid', label: 'depends on', color: '#6b7280' },
  runs_on: { style: 'dashed', label: 'runs on', color: '#9ca3af' },
  stores_in: { style: 'solid', label: 'stores in', color: '#f97316' },
  uses_secret: { style: 'dotted', label: 'uses', color: '#ef4444' },
  managed_by: { style: 'dashed', label: 'managed by', color: '#9ca3af' },
  metrics_in: { style: 'dashed', label: 'metrics', color: '#3b82f6' },
  logs_in: { style: 'dashed', label: 'logs', color: '#22c55e' },
  alerts_in: { style: 'dashed', label: 'alerts', color: '#f97316' },
  ingress_for: { style: 'solid', label: 'ingress for', color: '#06b6d4' },
};

export const STATUS_CONFIG: Record<string, { color: string; dot: string; label: string }> = {
  healthy: { color: '#22c55e', dot: '●', label: 'Healthy' },
  degraded: { color: '#eab308', dot: '◐', label: 'Degraded' },
  unhealthy: { color: '#ef4444', dot: '○', label: 'Unhealthy' },
  unknown: { color: '#9ca3af', dot: '◌', label: 'Unknown' },
  connected: { color: '#22c55e', dot: '●', label: 'Connected' },
  disconnected: { color: '#9ca3af', dot: '○', label: 'Disconnected' },
  error: { color: '#ef4444', dot: '○', label: 'Error' },
};

export const SEVERITY_CONFIG: Record<string, { color: string; variant: 'default' | 'destructive' | 'warning' | 'secondary' }> = {
  critical: { color: '#ef4444', variant: 'destructive' },
  warning: { color: '#eab308', variant: 'warning' },
  info: { color: '#3b82f6', variant: 'secondary' },
};
