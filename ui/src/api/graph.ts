import { apiClient } from './client';
import type { Graph, GraphNode, GraphEdge } from './types';

export interface Subgraph {
  nodes: GraphNode[];
  edges: GraphEdge[];
}

export function fetchGraph(): Promise<Graph> {
  return apiClient.get<Graph>('/api/v1/graph');
}

export function fetchNode(id: string): Promise<GraphNode> {
  return apiClient.get<GraphNode>(`/api/v1/graph/node/${encodeURIComponent(id)}`);
}

export function fetchRelated(id: string, depth = 1): Promise<Subgraph> {
  return apiClient.get<Subgraph>(
    `/api/v1/graph/node/${encodeURIComponent(id)}/related?depth=${depth}`
  );
}
