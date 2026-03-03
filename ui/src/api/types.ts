// Graph types (matches /api/v1/graph response)
export interface GraphNode {
  id: string;
  kind: string;
  name: string;
  namespace?: string;
  cluster?: string;
  metadata: Record<string, unknown>;
  labels?: Record<string, unknown>;
  status?: string;
}

export interface GraphEdge {
  id: string;
  source: string;
  target: string;
  type: string;
  metadata?: Record<string, unknown>;
}

export interface Graph {
  nodes: GraphNode[];
  edges: GraphEdge[];
}

// Source types (matches /api/v1/sources response)
export interface Source {
  id: string;
  type: string;
  name: string;
  zone?: string;
  config: Record<string, unknown>;
  status: string;
  last_sync_at?: string;
  last_error?: string;
  created_at: string;
  updated_at: string;
}

// Security types (match /api/v1/admin/zones response)
export interface SecurityZone {
  id: string;
  name: string;
  description: string;
  allowed_actions: string[];
  created_at?: string;
  sourceCount?: number;
}

export interface SourceZoneAssignment {
  source_id: string;
  zone_id: string;
  assigned_by: string;
  assigned_at: string;
  reason?: string;
}

export interface RbacPolicy {
  id: number;
  principal: string;
  zone_id: string;
  created_at: string;
}

// Chat/Session types (match /api/v1/sessions response)
export interface ChatMessage {
  id: number;
  session_id: string;
  role: 'user' | 'assistant';
  content: string;
  tool_name?: string;
  tool_args?: Record<string, unknown>;
  created_at: string;
  toolCalls?: ToolCall[];
}

export interface ToolCall {
  id: string;
  name: string;
  arguments: Record<string, unknown>;
  result?: string;
  status: 'pending' | 'success' | 'error';
}

export interface Session {
  id: string;
  started_at: string;
  ended_at?: string;
  summary?: string;
  message_count: number;
}

// Dashboard types
export interface Alert {
  id: string;
  severity: 'critical' | 'warning' | 'info';
  source: string;
  message: string;
  timestamp: string;
  acknowledged: boolean;
}
