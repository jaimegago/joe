# A route to change a registered component's routing config
Status: open
Priority: later

There is no route that changes a component's config after registration. `/components`
has GET, POST and DELETE, plus POST `promote` and POST `test`; the only PATCH in the API
is `/sessions/{id}` (`internal/api/server.go`). A routing field set at registration — a
`url`, `account_id`, `state_path`, or an endpoint override — can only be changed by
deleting the component and registering it again, which also removes its security-zone
assignment (the assignment cascades on delete).

Since D-0168 a registration-required field must be present at registration, so nothing
registered now is stranded without its locator and this route is not needed to repair one.
It is needed to *change* one: a backend that moves, a typo in a URL, a tenant that changes.

## The invariant any such route must hold

**Changing a routing field on an armed component returns it to unarmed.** Re-pointing a
`url` would otherwise deliver the component's existing credential to a new host without
passing promotion again. The route clears the credential reference (the
`credential.CredentialBearingFields` set) in the same governed, audited transaction that
writes the new routing value, so the operator must promote again before the component can
authenticate anywhere. This is the governance property the design rests on, ruled with the
registration contract (joe-pm `threads/registration-routing-locator.md`, decision 3); it is
stated here so the route is designed against it rather than discovering it.

## What else it must carry

- The same shared shape check registration runs: `componentgov.ValidateRegistrationConfig`
  against the type's declared contract, for the merged result.
- Admin-gated, with an audit row, like create, delete and promote.
- No connect probe on the write path, by the same reasoning as D-0029.
- A UI surface for it, rendered from the same per-type descriptor the Register dialog uses.
