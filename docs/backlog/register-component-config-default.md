# Config-less registration default — deferred fast-follows

Status: open

The `register-component-config-default` fix (D-0057) makes a config-less
registration succeed and land inert: both registration paths now normalize an
absent or empty config to an empty JSON object (`"{}"`) at the shared
`componentgov.NormalizeRegistrationConfig` seam, before encryption, leaving the
`components.config` `NOT NULL` column and the encrypt-at-rest invariant untouched.
That change is deliberately narrow. The following items surfaced while fixing it
and are explicitly **not** addressed here.

## 1. Map registration store-constraint violations to a 4xx/409, not a generic 500

The root cause of the original bug surfaced as an opaque HTTP 500: a nil config
tripped the `NOT NULL` constraint deep in the INSERT, and `handleCreateComponent`
funnels every store error through `writeInternalError` (500). The normalization
fix removes *this* trigger, but as defense-in-depth a constraint violation from
the registration store path should be translated to a clear client error — a 4xx
(or 409 for a uniqueness collision) carrying a specific message — rather than a
generic 500 that reads as a server fault. This is a broader error-mapping change
across the governed registration handlers, not a one-line guard, so it is
deferred.

## 2. Create-response fidelity: the handler echoes the pre-stamp struct

`handleCreateComponent` serializes the in-hand `source` pointer
(`writeJSON(w, http.StatusCreated, source)`) **after** `CreateTx`. In production
`Components` is the encrypted wrapper, whose `CreateTx` stamps `created_at`,
`updated_at`, and the default `status` onto an internal **copy** of the component
(`encryptComponent` returns `copy := *source`), not the original. So the create
response echoes an empty `status` and zero timestamps even though the persisted
row is correctly stamped. The fix is to serialize the stamped record (re-read, or
have the write path stamp the caller's struct), and ideally to return the
read-model `componentView` for consistency with the GET endpoints. Deferred as a
response-shape change with its own test surface.

## 3. The UI cannot supply a non-credential routing locator at registration

D-0029 has registration carry **non-credential routing config** (e.g. an
endpoint), but the current UI registration payload supplies none, and this fix
explicitly does **not** add any routing or locator collection — it only makes the
config-less path persist. Letting an operator provide a non-credential routing
locator at registration (distinct from the credential reference, which enters
only at promotion) is a separate design-and-build: it needs a form field, a
shape/validation contract per component type, and a decision on which types
accept which routing fields. Tracked here as a pointer; to be handled separately.
