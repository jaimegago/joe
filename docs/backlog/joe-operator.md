Joe operator — install Joe, and declare its skills and components, as Kubernetes resources
Status: open
Priority: later
Blocked-by: export-import-components

A Kubernetes operator for Joe: it installs and upgrades Joe, and lets an operator
declare Joe's skills and registered components as cluster resources, kept in git,
instead of configuring them by hand through the UI or CLI. Raised in the
maintainer's notes (joe-pm notes entry `p5z2v.11`), under "Future".

**Why it is blocked.** The operator needs a declarative, portable description of a
component registration to reconcile, and none exists. `export-import-components`
is that format. Designing the operator before it would invent a second one.

Open, and for the design pass when this is picked up:

- Credentials. Registrations reference secrets (D-0150's static references); an
  operator would source them from Kubernetes Secrets, and the encryption-key
  story in `export-import-components` applies.
- Skills. Whether a declared skill passes the same approve/reject lifecycle
  `skills-governance-hardening` gates, or whether declaration is approval.
- Scope. Joe running in the cluster it manages is not the only deployment; the
  operator is one install path, not the install path.
