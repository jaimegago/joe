import { apiClient } from './client';
import {
  ComponentSchema,
  ComponentTypesSchema,
  CreatedComponentSchema,
  PromotionRequirementsSchema,
  PromotionCandidatesSchema,
  PromoteResponseSchema,
} from './schemas';
import { z } from 'zod';
import type {
  Component,
  CreatedComponent,
  PromotionRequirements,
  PromotionCandidates,
  PromoteResponse,
} from './types';

export function fetchComponents(): Promise<Component[]> {
  return apiClient
    .get<unknown>('/api/v1/components')
    .then((r) => z.object({ components: z.array(ComponentSchema) }).parse(r).components);
}

// fetchComponentTypes returns the authoritative component-type enum from the
// backend (GET /api/v1/component-types) so the registration form's type
// selector never hardcodes the set.
export function fetchComponentTypes(): Promise<string[]> {
  return apiClient
    .get<unknown>('/api/v1/component-types')
    .then((r) => ComponentTypesSchema.parse(r).component_types);
}

// createComponent registers a new, inert component via the admin-gated governed
// create endpoint (POST /api/v1/components). The request carries ONLY id/type/
// name — never a config/credential by construction (credentials enter at a
// separate promotion step). The component lands credential-less in the
// unassigned zone under the read-only floor and can do nothing until it is
// separately promoted and zone-assigned.
export function createComponent(input: {
  id: string;
  type: string;
  name: string;
}): Promise<CreatedComponent> {
  return apiClient
    .post<unknown>('/api/v1/components', input)
    .then((r) => CreatedComponentSchema.parse(r));
}

export function fetchComponent(id: string): Promise<Component> {
  return apiClient
    .get<unknown>(`/api/v1/components/${encodeURIComponent(id)}`)
    .then((r) => ComponentSchema.parse(r));
}

export function testComponent(id: string): Promise<{ ok: boolean; message?: string }> {
  return apiClient.post<{ ok: boolean; message?: string }>(
    `/api/v1/components/${encodeURIComponent(id)}/test`,
    {}
  );
}

export function deleteComponent(id: string): Promise<void> {
  return apiClient.delete<void>(`/api/v1/components/${encodeURIComponent(id)}`);
}

// fetchPromotionRequirements describes the SHAPE of the credential reference an
// admin must supply to arm this component (which locator fields, which
// cross-field rules) — or, for a type with no wired provider, the set of types
// that CAN be armed. Drives the provider-conditional promotion form.
export function fetchPromotionRequirements(id: string): Promise<PromotionRequirements> {
  return apiClient
    .get<unknown>(`/api/v1/components/${encodeURIComponent(id)}/promotion-requirements`)
    .then((r) => PromotionRequirementsSchema.parse(r));
}

// fetchPromotionCandidates lists the LIVE credential references the admin can
// choose for this component right now (for the static provider, the env var
// names matching the type's prefix that are currently set in Joe's
// environment). Names/labels only — never a credential value.
export function fetchPromotionCandidates(id: string): Promise<PromotionCandidates> {
  return apiClient
    .get<unknown>(`/api/v1/components/${encodeURIComponent(id)}/promotion-candidates`)
    .then((r) => PromotionCandidatesSchema.parse(r));
}

// PromoteRequest is a credential REFERENCE — the provider discriminator plus
// the relevant locator fields. It NEVER carries an inline `value`: the armed
// record points Joe at a credential in its own environment, it does not store
// the secret. The static reference is an env_var name; the kubeconfig-exec
// reference is a path / context / in-cluster identity (at least one of
// in_cluster or kubeconfig).
export type PromoteRequest =
  | { credential_provider: 'static'; env_var: string }
  | {
      credential_provider: 'kubeconfig-exec';
      kubeconfig?: string;
      context?: string;
      in_cluster?: boolean;
    };

// promoteComponent transitions an inert component to armed (or rotates an
// already-armed component's reference) via the admin-gated, audited promote
// endpoint. The response is outcome-only and never echoes the reference.
export function promoteComponent(id: string, body: PromoteRequest): Promise<PromoteResponse> {
  return apiClient
    .post<unknown>(`/api/v1/components/${encodeURIComponent(id)}/promote`, body)
    .then((r) => PromoteResponseSchema.parse(r));
}
