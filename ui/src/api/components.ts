import { apiClient } from './client';
import { ComponentSchema, ComponentTypesSchema, CreatedComponentSchema } from './schemas';
import { z } from 'zod';
import type { Component, CreatedComponent } from './types';

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
