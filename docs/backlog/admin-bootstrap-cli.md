# Admin bootstrap CLI — open residue

Status: open
Priority: next

Residue from session `admin-bootstrap-cli` (D-0129), which added
`joe admin bootstrap` as the third writer to `admin_principals` and closed the
writer set with `internal/rbac/admin_writers_guard_test.go`. Each item below is
out of that session's scope by decision, not by oversight.

## 1. Email-case hazard in the admin roster (read-only investigation)

**This is an investigation, not a fix to attempt.** It needs its own decision
about what the intended identity-comparison rule is, and that decision is not
made. Do not "correct" the comparisons until it is — one of them is
deliberately case-insensitive and changing it silently is how a bootstrap admin
stops being re-granted.

Three sites disagree about whether a `user:<email>` principal is compared
case-sensitively:

- **Minting.** `rbac.UserPrincipal` (`internal/rbac/identity.go`) lowercases only
  for the reserved-prefix collision check and returns `PrefixUser + email` with
  the email's case as the identity provider supplied it. An IdP that varies case
  across logins therefore mints `user:Alice@example.com` on one login and
  `user:alice@example.com` on the next.
- **Bootstrap match.** The OIDC callback compares the verified email to
  `auth.admin_email` with `strings.EqualFold` (`internal/auth/handlers.go`), so
  both spellings match and both get granted.
- **Removal guard.** `removeAdmin` (`internal/api/admin.go`) derives
  `user:<adminEmail>` and compares with `EqualFold` too, so it treats the two
  spellings as one principal.

But `admin_principals.principal` is a TEXT primary key, and `IsAdmin` and the
last-admin count are exact-match SQL. So two case-variant rows are **two admins**
to the existence check and the roster count, and **one admin** to the removal
guard. Consequences to work through, none of them yet demonstrated against a
real IdP:

- The last-admin 409 guard could be satisfied by a count of 2 that is really one
  human, allowing a removal that leaves zero effective admins.
- `joe admin bootstrap`'s containment clause counts rows, so a roster holding
  only case-variants of one human still reads as non-empty — correct here, but
  worth stating explicitly rather than relying on.
- The roster UI lists what look like two distinct admins for one person.

The new CLI does **not** widen this: it accepts service-account principals only,
and those are minted from a config-file name via `rbac.ServicePrincipal` and
matched exactly, so no case variance is possible on that path. The hazard is
confined to `user:` principals, which reach the table only through the OIDC
bootstrap and the REST surface.

Deliverable: a written finding on what the intended rule is (normalize at mint?
compare case-insensitively everywhere? constrain the column?), what it would
break, and whether any real IdP in scope actually varies case. Then a decision
entry, then possibly a change.

## 2. A refused bootstrap writes no audit row

`SQLRepository.AddFirstAdmin` returns `(false, nil)` and commits nothing when the
roster is non-empty, so a refused attempt leaves no trace. The reasoning at the
time: nothing changed, `adminEvent` hardcodes `DecisionAllow`, and the audit
schema's deny shape was not worth designing around a command that can only be
run by someone who already has filesystem access to the database.

The counter-argument is that a refused privilege-escalation attempt is exactly
the kind of thing an audit log exists for, and that "the caller already had
filesystem access" is an argument about severity, not about visibility. If this
is taken up, it wants a `DecisionDeny` path through `adminEvent` rather than a
one-off row, since the same gap exists for other refused mutations.

## 3. The `cli:` actor prefix is unmodelled

`auth.ActorCLIBootstrap` is the literal string `cli:admin-bootstrap`, chosen so
it cannot collide with a mintable principal (`rbac`'s reserved prefixes are
`user:`, `group:`, `svc:`). Nothing in `internal/rbac/identity.go` knows about
`cli:` — it is not in `reservedPrefixes`, so a service account could in principle
be named such that... no, it could not: `ServicePrincipal` always applies the
`svc:` prefix, so `cli:` is unreachable by construction from every minting path.
The value is safe today and the reasoning is recorded on the constant.

What is unmodelled is the general question of **non-principal actors in the
audit log**. Joe writes `agent:core` for the autonomous refresh loop and now
`cli:admin-bootstrap` for the offline grant; both are actors that are not
identities. If a third appears, the prefix set deserves to be declared in one
place with the same collision guarantee `reservedPrefixes` gives, rather than
each site arguing its own safety in a comment.
