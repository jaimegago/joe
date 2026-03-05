import { apiClient } from './client';
import { GraphSchema, GraphNodeSchema, SubgraphSchema } from './schemas';
import type { Graph, GraphNode, Subgraph } from './types';

export function fetchGraph(): Promise<Graph> {
  return apiClient.get<unknown>('/api/v1/graph').then((r) => GraphSchema.parse(r));
}

export function fetchNode(id: string): Promise<GraphNode> {
  return apiClient
    .get<unknown>(`/api/v1/graph/node/${encodeURIComponent(id)}`)
    .then((r) => GraphNodeSchema.parse(r));
}

export function fetchRelated(id: string, depth = 1): Promise<Subgraph> {
  return apiClient
    .get<unknown>(`/api/v1/graph/node/${encodeURIComponent(id)}/related?depth=${depth}`)
    .then((r) => SubgraphSchema.parse(r));
}
