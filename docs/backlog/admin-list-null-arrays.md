# Admin list endpoints serialize an empty list as `null`, and three UI parsers reject it

Status: open
Priority: next

## The defect

Four admin list endpoints return `null` rather than `[]` for their array field
when the list is empty — the handler serializes a nil slice — and the UI parses
each with a bare `z.array(...)`, which **rejects `null`**. On a fresh install the
query therefore fails, and the page renders its error state instead of its empty
state.

Measured against a booted binary at `9e815c2` (a fresh SQLite store, one
bootstrapped admin, nothing else registered):

| Endpoint | Empty response | Client parser |
|---|---|---|
| `GET /api/v1/admin/policies` | `{"count":0,"policies":null}` | `fetchPolicies` — **fixed**, see below |
| `GET /api/v1/admin/component-zones` | `{"assignments":null,"count":0}` | `fetchComponentZones` |
| `GET /api/v1/admin/unassigned` | `{"component_ids":null,"count":0}` | `fetchUnassigned` |
| `GET /api/v1/admin/principals` | `{"count":0,"principals":null}` | `fetchPrincipals` |

`GET /api/v1/admin/zones`, `/admins` and `/read-promotions` are **not** affected
— each is non-empty on a fresh install (seeded zones, the bootstrap admin, the
full component-type enum overlaid), so their arrays are never nil in practice.
That is why the defect survived: the affected four are exactly the lists a new
install starts empty.

## What is already fixed, and why only that one

`fetchPolicies` (`ui/src/api/security.ts`) was made null-tolerant by the
`read-posture-zoned-flip-ui` session, because the read-posture control's
zero-grant lockout warning turns on telling **"no grants"** apart from **"the
grants could not be read"** — and the rejecting parse collapsed the first into the
second in exactly the install where the warning matters most. Pinned by
`ui/src/api/security.policies.test.ts`.

The other three were left alone deliberately: that session's scope was the
zoned-flip UI, and three drive-by parser edits are a different change. They are
the same one-line shape:

```ts
z.object({ field: z.array(Schema).nullable() }).parse(r).field ?? []
```

## The open question this item should settle first

**Whether the fix belongs on the client or the wire.** The client fix is four
lines and touches nothing else. The server fix — initializing the slices so the
handlers marshal `[]` — is the better contract (a JSON list field that is
sometimes `null` is a trap for **every** client, not just this UI, and joe's REST
surface is public API), but it changes responses other consumers may already
parse defensively.

Recommendation: fix the wire, keep the client tolerance as belt-and-braces, and
add a handler-level test per endpoint asserting `[]` on an empty store. Do not
close this item by fixing only the three parsers — that leaves the trap in place
for the next client.

## Where the empty states currently go unreachable

- `PoliciesAdminPage` — has an `EmptyState` ("No policies") that could not render
  before the `fetchPolicies` fix; a fresh install hit `QueryError` instead.
- `UsersPage` — same shape via `fetchPrincipals`.
- The component-zone assignment surfaces via `fetchComponentZones` /
  `fetchUnassigned`.

Provenance: session `read-posture-zoned-flip-ui`, found while verifying the
zero-grant path of the read-posture flip against a booted binary.
