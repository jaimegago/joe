# Service-account key-collision error doesn't name the colliding value's origin

Status: open
Priority: later

`internal/auth/serviceaccount.go:51`:

```go
return nil, fmt.Errorf("service account %q: key already used by %q", sa.Name, existing)
```

Rendered example: `service account "joe-admin": key already used by "svc:server"`.

`existing` (bound at `serviceaccount.go:50`) is the previously-minted `rbac.Principal`
— it names *which account* already holds the colliding key, but not *where that
account's key value came from*. A `server.service_accounts` key can be set two ways
that produce an identical collision message: directly in the YAML `key:` field, or via
the `JOE_API_KEY` environment variable silently rewriting the reserved `server`
account's key at load time (`setServerServiceAccountKey`,
`internal/config/config.go:551-553`). An operator staring at "key already used by
svc:server" with a config file that clearly shows two distinct YAML keys has no way to
know from the message alone that `JOE_API_KEY` is the actual second source of the
collision — they have to already know about the env-var override to think to check it.

Observed as a real trap during the v0.2.0 release run (`docs/project/DECISIONS.md`,
D-0137: "declaring a `server.service_accounts` key equal to `JOE_API_KEY` fails boot
on a key-collision error").

## Improvement

Name both colliding parties' origins, not just the holding principal — e.g.
distinguish "key set in config file" from "key set via JOE_API_KEY" in the error, so
the reader knows which specific line (YAML key, or the `JOE_API_KEY` env var) to
change. No code change made in this session; this is a documentation-of-the-gap item
only.
