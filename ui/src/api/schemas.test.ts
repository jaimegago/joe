import { describe, it, expect } from 'vitest';
import {
  GraphNodeSchema,
  GraphSchema,
  SourceSchema,
  ChatMessageSchema,
  AlertSchema,
  SessionSchema,
  CurrentUserSchema,
} from './schemas';

describe('GraphNodeSchema', () => {
  it('parses a valid node', () => {
    const node = GraphNodeSchema.parse({
      id: 'svc-1',
      kind: 'Service',
      name: 'payment-svc',
      metadata: { replicas: 2 },
    });
    expect(node.id).toBe('svc-1');
    expect(node.namespace).toBeUndefined();
  });

  it('includes optional fields when present', () => {
    const node = GraphNodeSchema.parse({
      id: 'svc-1',
      kind: 'Deployment',
      name: 'api',
      namespace: 'prod',
      cluster: 'us-east',
      metadata: {},
      status: 'healthy',
    });
    expect(node.namespace).toBe('prod');
    expect(node.status).toBe('healthy');
  });

  it('throws on missing required field', () => {
    expect(() =>
      GraphNodeSchema.parse({ kind: 'Service', name: 'svc', metadata: {} })
    ).toThrow();
  });
});

describe('GraphSchema', () => {
  it('parses a graph with nodes and edges', () => {
    const graph = GraphSchema.parse({
      nodes: [{ id: 'n1', kind: 'Pod', name: 'pod-1', metadata: {} }],
      edges: [{ id: 'e1', source: 'n1', target: 'n2', type: 'depends_on' }],
    });
    expect(graph.nodes).toHaveLength(1);
    expect(graph.edges).toHaveLength(1);
  });

  it('parses an empty graph', () => {
    const graph = GraphSchema.parse({ nodes: [], edges: [] });
    expect(graph.nodes).toHaveLength(0);
  });

  it('throws when nodes is missing', () => {
    expect(() => GraphSchema.parse({ edges: [] })).toThrow();
  });
});

describe('SourceSchema', () => {
  it('parses a valid source', () => {
    const source = SourceSchema.parse({
      id: 'k8s-prod',
      type: 'kubernetes',
      name: 'Production K8s',
      config: { server: 'https://k8s.prod' },
      status: 'connected',
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    });
    expect(source.id).toBe('k8s-prod');
    expect(source.zone).toBeUndefined();
  });
});

describe('ChatMessageSchema', () => {
  it('parses a user message', () => {
    const msg = ChatMessageSchema.parse({
      id: 1,
      session_id: 'sess-abc',
      role: 'user',
      content: 'hello',
      created_at: '2024-01-01T00:00:00Z',
    });
    expect(msg.role).toBe('user');
    expect(msg.toolCalls).toBeUndefined();
  });

  it('rejects an invalid role', () => {
    expect(() =>
      ChatMessageSchema.parse({
        id: 1,
        session_id: 's',
        role: 'system',
        content: 'hi',
        created_at: '2024-01-01T00:00:00Z',
      })
    ).toThrow();
  });
});

describe('AlertSchema', () => {
  it('parses a critical alert', () => {
    const alert = AlertSchema.parse({
      id: 'alert-1',
      severity: 'critical',
      source: 'prometheus',
      message: 'CPU high',
      timestamp: '2024-01-01T00:00:00Z',
      acknowledged: false,
    });
    expect(alert.severity).toBe('critical');
  });

  it('rejects an unknown severity', () => {
    expect(() =>
      AlertSchema.parse({
        id: 'a',
        severity: 'fatal',
        source: 's',
        message: 'm',
        timestamp: 't',
        acknowledged: false,
      })
    ).toThrow();
  });
});

describe('CurrentUserSchema', () => {
  it('parses a current-user payload including oidc_enabled', () => {
    const user = CurrentUserSchema.parse({
      principal: 'user:alice',
      is_admin: true,
      rbac_enabled: true,
      oidc_enabled: true,
    });
    expect(user.oidc_enabled).toBe(true);
    expect(user.principal).toBe('user:alice');
  });

  it('requires oidc_enabled', () => {
    expect(() =>
      CurrentUserSchema.parse({ principal: 'user:bob', is_admin: false, rbac_enabled: true })
    ).toThrow();
  });
});

describe('SessionSchema', () => {
  it('parses a session', () => {
    const session = SessionSchema.parse({
      id: 'sess-1',
      started_at: '2024-01-01T00:00:00Z',
      message_count: 5,
    });
    expect(session.message_count).toBe(5);
    expect(session.ended_at).toBeUndefined();
  });
});
