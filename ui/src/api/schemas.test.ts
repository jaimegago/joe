import { describe, it, expect } from 'vitest';
import {
  GraphNodeSchema,
  GraphSchema,
  ComponentSchema,
  CreatedComponentSchema,
  ComponentTypesSchema,
  ChatMessageSchema,
  AlertSchema,
  SessionSchema,
  CurrentUserSchema,
  PromotionRequirementsSchema,
  PromotionCandidatesSchema,
  PromoteResponseSchema,
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

describe('ComponentSchema', () => {
  it('parses an armed component with the derived arm-state projection', () => {
    // A002 read-model fix: GET /api/v1/components(/{id}) no longer carries the
    // raw config blob; an armed (promoted) component instead serializes
    // armed:true plus the provider Kind. The raw locator keys never appear.
    const source = ComponentSchema.parse({
      id: 'k8s-prod',
      type: 'kubernetes',
      name: 'Production K8s',
      armed: true,
      provider: 'kubeconfig-exec',
      status: 'connected',
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    });
    expect(source.id).toBe('k8s-prod');
    expect(source.zone).toBeUndefined();
    expect(source.armed).toBe(true);
    expect(source.provider).toBe('kubeconfig-exec');
  });

  it('parses an inert component with armed false and no provider', () => {
    // A config-less registration lands inert; the read model projects armed:false
    // and omits the provider Kind entirely. The list surface parses each element
    // against this schema, so the inert shape must parse cleanly.
    const source = ComponentSchema.parse({
      id: 'prod-prometheus',
      type: 'prometheus',
      name: 'Production Prometheus',
      armed: false,
      status: 'unassigned',
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    });
    expect(source.armed).toBe(false);
    expect(source.provider).toBeUndefined();
    expect(source.id).toBe('prod-prometheus');
  });
});

describe('CreatedComponentSchema', () => {
  it('parses a config-less create response (config null, zero-value status/timestamps)', () => {
    // The governed create endpoint returns the raw component; for a config-less
    // registration config is null and (under the at-rest encryption wrapper)
    // status/timestamps are zero-valued. The schema must tolerate this.
    const created = CreatedComponentSchema.parse({
      id: 'prod-prometheus',
      type: 'prometheus',
      name: 'Production Prometheus',
      config: null,
      status: '',
      created_at: '0001-01-01T00:00:00Z',
      updated_at: '0001-01-01T00:00:00Z',
    });
    expect(created.id).toBe('prod-prometheus');
    expect(created.type).toBe('prometheus');
  });

  it('parses a create response with only id/type/name', () => {
    const created = CreatedComponentSchema.parse({
      id: 'c1',
      type: 'github',
      name: 'gh',
    });
    expect(created.config).toBeUndefined();
  });

  it('throws when a required field is missing', () => {
    expect(() => CreatedComponentSchema.parse({ id: 'c1', type: 'github' })).toThrow();
  });
});

describe('ComponentTypesSchema', () => {
  it('parses the component-type enum response', () => {
    const r = ComponentTypesSchema.parse({
      component_types: ['kubernetes', 'prometheus', 'github'],
      count: 3,
    });
    expect(r.component_types).toHaveLength(3);
  });

  it('throws when component_types is missing', () => {
    expect(() => ComponentTypesSchema.parse({ count: 0 })).toThrow();
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

describe('PromotionRequirementsSchema', () => {
  it('parses a wired requirements shape', () => {
    const r = PromotionRequirementsSchema.parse({
      type: 'github',
      wired: true,
      kind: 'static',
      locator_fields: [{ name: 'env_var', required: true }],
      constraints: [
        { rule: 'forbid-inline-value', fields: ['value'], message: 'no inline secret' },
      ],
    });
    expect(r.wired).toBe(true);
    if (r.wired) {
      expect(r.kind).toBe('static');
      expect(r.locator_fields[0].name).toBe('env_var');
    }
  });

  it('parses a kubeconfig-exec at-least-one-of constraint', () => {
    const r = PromotionRequirementsSchema.parse({
      type: 'kubernetes',
      wired: true,
      kind: 'kubeconfig-exec',
      locator_fields: [
        { name: 'in_cluster', required: false },
        { name: 'kubeconfig', required: false },
        { name: 'context', required: false },
      ],
      constraints: [
        {
          rule: 'at-least-one-of',
          fields: ['in_cluster', 'kubeconfig'],
          message: 'supply either in_cluster=true or a kubeconfig path',
        },
      ],
    });
    expect(r.wired).toBe(true);
    if (r.wired) {
      const c = r.constraints.find((x) => x.rule === 'at-least-one-of');
      expect(c?.fields).toEqual(['in_cluster', 'kubeconfig']);
    }
  });

  it('parses an unwired shape carrying armable_types', () => {
    const r = PromotionRequirementsSchema.parse({
      type: 'webhook',
      wired: false,
      armable_types: ['github', 'kubernetes'],
    });
    expect(r.wired).toBe(false);
    if (!r.wired) expect(r.armable_types).toContain('github');
  });

  it('rejects a wired shape missing kind', () => {
    expect(() =>
      PromotionRequirementsSchema.parse({
        type: 'github',
        wired: true,
        locator_fields: [],
        constraints: [],
      })
    ).toThrow();
  });
});

describe('PromotionCandidatesSchema', () => {
  it('parses a static applicable candidate set', () => {
    const r = PromotionCandidatesSchema.parse({
      type: 'github',
      wired: true,
      kind: 'static',
      prefix: 'JOE_GITHUB_',
      applicable: true,
      candidates: [{ label: 'PROD', env_var_name: 'JOE_GITHUB_PROD' }],
    });
    expect(r.wired).toBe(true);
    if (r.wired) {
      expect(r.applicable).toBe(true);
      expect(r.candidates[0].env_var_name).toBe('JOE_GITHUB_PROD');
    }
  });

  it('parses a kubeconfig-exec not-applicable answer (no prefix)', () => {
    const r = PromotionCandidatesSchema.parse({
      type: 'kubernetes',
      wired: true,
      kind: 'kubeconfig-exec',
      applicable: false,
      candidates: [],
    });
    expect(r.wired).toBe(true);
    if (r.wired) {
      expect(r.applicable).toBe(false);
      expect(r.candidates).toHaveLength(0);
      expect(r.prefix).toBeUndefined();
    }
  });

  it('parses an unwired candidate answer', () => {
    const r = PromotionCandidatesSchema.parse({
      type: 'webhook',
      wired: false,
      armable_types: ['github'],
    });
    expect(r.wired).toBe(false);
  });
});

describe('PromoteResponseSchema', () => {
  it('parses an outcome-only promote response', () => {
    const r = PromoteResponseSchema.parse({
      component_id: 'prod-github',
      type: 'github',
      provider: 'static',
      armed: true,
      rearm: false,
    });
    expect(r.armed).toBe(true);
    expect(r.rearm).toBe(false);
  });

  it('marks a rotation with rearm:true', () => {
    const r = PromoteResponseSchema.parse({
      component_id: 'prod-github',
      type: 'github',
      provider: 'static',
      armed: true,
      rearm: true,
    });
    expect(r.rearm).toBe(true);
  });
});
