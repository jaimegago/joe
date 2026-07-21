---
title: Operations
weight: 80
description: Running, observing, and recovering Joe in production.
---

# Operations

This section is the operator's how-to for running Joe in production: standing the
daemon up, watching it through metrics and traces, recovering it from safe mode after
an emergency stop, and reaching it with a break-glass credential when normal login is
unavailable.

It documents real behavior and its limits. For *why* Joe is governed the way it is —
observation mode, the write floor, the executor gate order — see
[Concepts](../concepts/). For the exact config keys and environment variables behind
every knob mentioned here, see [Configuration](../configuration/); this page names the
keys but does not restate their defaults.

## Running the daemon

The bare `joe` command — no subcommand — *is* the server. It boots the HTTP API, the
core agent, the adapters, and the graph store, and then runs until stopped:

```bash
joe                       # boot on built-in defaults
joe --config /etc/joe/config.yaml
```

The API listens on `localhost:7777` by default; set `server.address` to change it.
Run `joe` under whatever supervises your other long-lived services — `systemd`, a
container runtime, a process manager — and let that own restart-on-exit. Restart is
not incidental: it is the **only** way Joe leaves safe mode (see
[Recovering from safe mode](#recovering-from-safe-mode-the-write-floor) below), so the
supervisor must be configured to bring Joe back up after it exits.

Joe **refuses to start without an identity configuration** — at least one service
account or a complete OIDC issuer. There is no permit-all runtime mode; if the boot
guard is unsatisfied, Joe exits with an explanatory error rather than coming up open.
See [Install and Build](../install-and-build/) for the identity it requires.

### TLS

Server TLS is enabled only when **both** `server.tls_cert_file` and
`server.tls_key_file` point at a certificate and its key; set one without the other and
Joe stays plaintext. When neither is set, Joe logs a warning at startup that
connections are unencrypted — terminate TLS at a proxy in front of Joe, or set both
keys. (`server.tls_enabled` is a *client-side* flag telling subcommands to dial over
HTTPS — it is not the server switch.) The key reference is in
[Configuration](../configuration/).

### State on disk

Joe keeps its state in two places under its home directory (`~/.joe` by default):

- **The database** — an embedded SQLite store at `~/.joe/joe.db`. This is the graph,
  sessions, RBAC state, the panic row, and the audit log. It is
  a single file; no external database server is involved. Back it up as a unit, and
  treat it as sensitive — the audit log and identity state live here. Note that Joe
  **deletes some of this data automatically on a timer**: a background retention sweeper
  hard-purges expired sessions (and their transcripts) by default, so a backup is the
  only way to recover a session Joe has purged (see [Retention and
  growth](#retention-and-growth)).
- **The session archive** — cold-storage session artifacts under
  `~/.joe/session-archive`, overridable with `server.session_archive_dir`. This
  directory is **populated by the retention sweeper** when the session policy's terminal
  action is *archive*: an expired session is moved here, out of the live database, as a
  versioned artifact (see [Retention and growth](#retention-and-growth)). With the
  default terminal action it stays empty.

Both paths follow Joe's home directory; point them at durable, backed-up storage in
production.

### Process health

Joe does **not** serve a dedicated `/livez`, `/readyz`, or `/healthz` endpoint, and the
status endpoint is **not** unauthenticated — it sits behind the same edge auth as the
rest of the API, so a monitor must present a credential (a service-account bearer key) to
poll it:

```bash
curl -H "Authorization: Bearer <service-account-key>" http://localhost:7777/api/v1/status
# {"status":"ok","version":"…","time":"2026-06-28T…Z"}
```

`GET /api/v1/status` returns a slim `status` / `version` / `time` payload — a 200 means
the API is up and serving; without a credential it returns `401`. For the full build
identity (commit, build time, and the UI digest), `GET /api/v1/version` serializes the
complete build-info record (also authenticated). If you need a credential-free process-up
signal, scrape the unauthenticated Prometheus metrics endpoint on its separate port
instead (see [Metrics](#metrics) below). Do not expect a Kubernetes-style readiness gate,
because none is wired.

## Retention and growth

Everything Joe stores lives in the one SQLite file described under [State on
disk](#state-on-disk). Two facts about how that file changes over time matter for an
operator: a background sweeper **deletes** some session data on a timer by default, and
several tables only ever **grow**.

### The session retention sweeper

Joe runs a retention sweeper in the background from the moment the daemon boots — it is
**on by default, not opt-in**. On a fixed interval (periodically, not continuously) it
applies the single, install-wide session retention policy and drains abandoned
login-flow state. Every deletion or lifecycle change it makes is written to the
[audit log](#tables-that-only-grow) in the same transaction, attributed to a dedicated
service principal, so automated expirations are as traceable as human ones.

The policy has three knobs, edited from the **Sessions** page (or the admin
**Governance** tab) and applied install-wide:

- **Trash-grace auto-purge — on by default.** When a session is trashed — by its owner,
  or by the sweeper under inactivity expiry — it is stamped with a purge deadline of
  trashed-time plus the trash-grace window. Once that deadline passes, the sweeper
  **hard-purges** the session: the row is deleted and its transcript goes with it,
  because chat messages are removed by database cascade when their session is deleted. A
  purged session is gone from the live database — only a backup can bring it back.
- **Inactivity expiry — off by default.** The sweeper can also expire sessions left
  untouched longer than an inactivity window, but that window ships **disabled**: nothing
  auto-expires on inactivity until an admin turns it on. When enabled, an expired session
  gets the policy's terminal action.
- **Terminal action — trash-then-purge (default) or archive.** The default action trashes
  an expired session, which then follows the trash-grace path above. The alternative,
  *archive*, moves the session out of the live database into the [session archive
  directory](#state-on-disk) as a cold-storage artifact. Archive requires a configured
  archive directory; without one the sweeper leaves the session **active and logs that it
  did so**, rather than deleting it or marking it archived with no real artifact behind
  the mark.

**A footgun worth stating plainly.** If the trash-grace window is set to **zero**, or no
retention policy can be resolved at the moment a session is trashed, that session is
trashed with **no purge deadline**. The sweeper only purges sessions that carry a
deadline, so such sessions sit in trash **indefinitely** until an admin purges them by
hand. This is intended semantics — a zero grace means *never auto-purge*, not *purge
now* — but it means trash is not guaranteed to drain on its own under every policy, and
an operator choosing that setting should know they have opted into manual cleanup.

### Tables that only grow

Several tables have **no automatic deletion path** in this version and grow monotonically
with use. Size backups and disk capacity accordingly:

- **The audit log grows without bound, by design.** It is **append-only** — inserts only,
  enforced both in application code and by database triggers that reject any update or
  delete — so there is deliberately no rotation or pruning in this version. The only
  sanctioned way to reclaim its space is to drop and recreate the table wholesale, which
  discards the **entire** audit history; there is no selective trim. A retention/rotation
  mechanism is planned as a later extension, not shipped today.
- **LLM-usage and code-review-job records accumulate.** One usage record is written per
  model call, and review-job records accrue as Joe runs; neither has a prune path today.
- **The legacy session tables are frozen.** An older sessions/messages store predates the
  current session subsystem. Nothing writes to it now and it has no deletion path; it is
  deliberately **retained** (a future feature depends on it) and simply does not change in
  size.
- **The infrastructure graph is bounded, not just self-reconciling.** Graph nodes and
  edges are *not* in this list. While a component is live, Joe reconciles its topology
  on each refresh, adding and removing nodes and edges to match what the component
  currently reports. When a component is deleted, its graph rows are removed with it.
  Together, the two keep the graph from growing without bound.

Where any of this needs to change — audit rotation, a usage-retention or roll-up policy,
a database-size signal for operators — it is tracked as deferred work. None of it blocks
running Joe today; this section is the operator-facing posture as it stands.

## Observability

Joe is instrumented with OpenTelemetry. Metrics are exported to Prometheus by default;
traces are instrumented but **not exported until you opt in**. Both are configured
entirely through `OTEL_*` environment variables documented in
[Configuration](../configuration/).

### Metrics

The Prometheus metrics endpoint is served on its **own port, separate from the API**.
By default that is port `9090` at path `/metrics` (set `OTEL_METRICS_PORT` to change
it); the API stays on `7777`. Scrape the metrics port, not the API port:

```bash
curl http://localhost:9090/metrics
```

Metrics export is on by default (`OTEL_METRICS_EXPORTER=prometheus`); set it to `none`
to turn the endpoint off. What the running binary actually emits:

- **Build identity** — `joe_build_info`, a constant-`1` gauge carrying version, commit,
  build time, and the embedded-UI digest as labels. Scrape it across replicas to catch
  a stale build.
- **Business gauges** — current counts of components, graph nodes, graph edges, and
  connected adapters.
- **HTTP server metrics** — request count, error count, in-flight requests, and request
  duration for the API, recorded by middleware.
- **LLM call metrics** — per-call counters and a latency histogram on the live model
  chain: `llm_requests_total`, `llm_errors_total`, `llm_tokens_input_total`,
  `llm_tokens_output_total`, and the `llm_request_duration` histogram, each labeled by
  provider, model, and operation. The token counters let you estimate spend directly
  from Prometheus.
- **Internal operation metrics** — counters, error counters, and duration histograms
  for tool executions, adapter calls, graph operations, database operations, core-agent
  refresh and discovery cycles, user-agent runs, and chat sessions.

That list is the whole emitted surface. There are no cache-hit/miss metrics — they do
not exist in the binary — so do not build dashboards expecting them.

### Traces

Tracing is instrumented (every LLM call opens a span), but the **traces exporter
defaults to off**: `OTEL_TRACES_EXPORTER` is `none` out of the box, so spans are
created and immediately discarded — nothing leaves the process. Traces are not
exported until you set an exporter:

```bash
# Print spans to stdout (development)
export OTEL_TRACES_EXPORTER=stdout

# Ship spans to an OTLP collector (Jaeger, DataDog agent, etc.)
export OTEL_TRACES_EXPORTER=otlp
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
```

Until you change `OTEL_TRACES_EXPORTER`, assume you have **no** trace data, regardless
of any tracing backend you have running.

## Recovering from safe mode (the write floor)

Joe has a kill switch. When it fires, Joe records the event and exits, then reboots
into **safe mode** with the *write floor* raised — reads still run, every mutating
action is denied. Recovery is deliberately a host operation, not an API call. For the
model behind this, read [Observation mode and the write
floor](../concepts/observation-mode-and-the-write-floor/).

### How the floor comes up

The floor is resolved once at boot from two inputs and then sealed for the life of the
process:

- **Observation mode** — Joe's default read-only posture. The floor comes up when
  `JOE_MODE` is unset and, explicitly, with `JOE_MODE=observation`.
- **A sticky panic** — a panic recorded in the database (the single `cluster_panic_state`
  row) raises the floor on every subsequent boot until it is cleared. A panic wins over
  everything: it holds the floor up regardless of the mode setting.

### Triggering an emergency stop

Any of these records a panic and brings Joe down into safe mode on its next start:

```bash
joe panic --reason "runaway mutation detected"     # CLI → POSTs to the running server
curl -X POST http://localhost:7777/api/v1/panic \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -d '{"reason":"runaway mutation detected"}'        # HTTP API
kill -USR1 <joe-pid>                                  # Unix signal to the live process
```

The Web UI exposes the same control. In every case Joe writes the panic to the
`cluster_panic_state` row and exits with code 2; the supervisor restarts it, and it
comes up read-only. Inspect the state at any time:

```bash
curl http://localhost:7777/api/v1/panic/status \
  -H "Authorization: Bearer $JOE_API_KEY"
# {"safe_mode":true,"triggered_at":"…","trigger_source":"signal","trigger_reason":"…"}
```

### There is no unlock API

The write floor is sealed at boot and **nothing in the running process can lower it**.
There is deliberately no unlock endpoint — `POST /api/v1/unlock` does not exist (it
returns 404). No authenticated call, no admin grant, no UI action lowers a raised
floor. Recovery requires changing the boot inputs and restarting; there is no live
down-transition.

### Recovering

Recovery has one step you take now and a second that is forthcoming. Clearing the panic
returns Joe to its **normal observation (read-only) posture**; coming back up *able to
write* is not reachable today and will apply under the governed full-capabilities mode.
Neither condition is sufficient alone:

1. **Acknowledge the panic.** On the host where Joe runs, clear the sticky panic row
   with the local CLI:

   ```bash
   joe unlock --reason "false alarm — incident resolved"
   ```

   `joe unlock` opens the database **directly**. It does not contact or signal any
   running process and does not touch a live floor — it edits the panic row only. It is
   idempotent: run it on an already-clear Joe and it reports there was nothing to clear.
   The `--reason` is optional and, when given, is written to the log as an
   acknowledgment. The cleared state takes effect only at the **next** boot. Because it
   never signals the daemon, you can run it while Joe is down (the usual case after a
   panic exit) or while a sealed-floor Joe is still up — either way it changes nothing
   until restart.

2. **Arm writes (forthcoming).** Coming back up *able to write* is not something a
   restart can do today: Joe's shipped default is observation, so clearing the panic
   returns Joe to the read-only observation posture rather than a writable one. When the
   governed full-capabilities mode lands, this is the step where you would ensure no
   *other* read-only posture holds the floor up on the next boot — clearing the panic
   would do nothing if the daemon still came up in observation mode, and leaving
   observation mode would do nothing while the panic row is still set. Until then,
   recovery ends at the observation posture.

Then restart Joe. The floor re-resolves from the now-clear row and the current mode. With
the panic cleared, Joe returns to its observation (read-only) posture; the floor will come
down only under the forthcoming full-capabilities mode, once both conditions above can be
met.

One more layer sits above the floor: even once the floor is down under full mode,
individual mutating actions remain **denied by default**. Each must be opted in under the
`act` section of the safety policy before Joe will run it. Bringing the floor down
restores only the *ability* to mutate; it does not by itself arm any specific write. See
[Governed
safety](../concepts/governed-safety/) for the action-safety model and
[Configuration](../configuration/) for the policy file.

## Break-glass access

Break-glass is the path an authorized operator uses to reach Joe when the normal OIDC
human-login flow is unavailable — the issuer is down, misconfigured, or unreachable. It
is an operational role, not a separate subsystem: a break-glass credential is an
ordinary **service-account bearer key** that resolves to a `svc:<name>` principal and
has been granted admin and reserved for emergencies. It is the same mechanism as the
loopback key a co-located subcommand uses; the two differ only in policy — who holds the
key and when you reach for it.

### Configuring a key

Service-account keys are defined under `server.service_accounts` in the config file —
each entry is a `name` and a `key`, and Joe mints the principal `svc:<name>` for it:

```yaml
server:
  service_accounts:
    - name: breakglass-oncall
      key: "a-long-random-high-entropy-secret"
```

The keys are stored **in plaintext** — there is no hashing at rest, so the key *is* the
credential and the config file *is* a secret store. Restrict its permissions to the
account running `joe`, and never commit it. The one account you can provision through
the environment is the reserved loopback account `svc:server`, set with `JOE_API_KEY`;
any named break-glass account must be defined in the config YAML. Key handling is
covered in [Configuration](../configuration/) and
[Install and Build](../install-and-build/).

### Granting admin

A service account becomes admin exactly like a human principal — the principal is an
opaque string and admin grants do no prefix-specific logic. Admin is minted on a small,
named set of paths. The **cold-start paths** — the ones that can create the *first*
admin, with no existing admin to authorize them — are the **OIDC admin-email bootstrap**
(`auth.admin_email`) and the offline **`joe admin bootstrap`** command. The **admin REST
surface** is the single writer thereafter.

**The first admin on an install with no identity provider.** With service accounts and no
OIDC issuer configured, the login and callback routes are never registered, so the
admin-email bootstrap cannot run at all. `joe admin bootstrap` is the way in — it grants
admin to a configured service account on a database that has no admin yet:

```bash
joe admin bootstrap svc:joe-admin     # the bare name also works: joe admin bootstrap joe-admin

# Started the daemon with an explicit config? Pass the same file:
joe admin bootstrap svc:joe-admin --config /etc/joe/config.yaml
```

It runs offline against Joe's own database and config file, contacts no daemon, and a
running Joe picks the grant up without a restart. `--config` names that config file and
mirrors the daemon's flag, so you can reuse your `joe --config ...` value verbatim — the
same file supplies both the service accounts the principal is checked against and the
database the grant lands in. Omit it and the default `~/.joe/config.yaml` is used; name a
file that does not exist and the command fails rather than falling back. It is
deliberately narrow:

- It grants to **service accounts only** — the principal must name an entry in
  `server.service_accounts`. A `user:` or `group:` argument is refused: with no identity
  provider that principal can never authenticate, so the grant would sit inert and then
  arm for whoever first presents that identifier once a provider is configured. Where
  there *is* an identity provider, `auth.admin_email` is the path for human admins.
- It is **refused the moment any admin exists**, and there is no override flag. It opens
  a database that has no admin at all; every later grant goes through the admin API.
- The grant is audited exactly as a REST grant is — the roster row and its audit row
  commit in the same transaction.

Name a **dedicated** administration account here rather than the shared general-purpose
key, so admin does not ride on the bearer secret every caller already holds.

**Every grant after the first** goes through the admin REST surface, which writes an
append-only audit row in the same transaction as the change.

```bash
# Grant admin to a break-glass account (needs an existing admin's credential)
curl -X POST http://localhost:7777/api/v1/admin/admins \
  -H "Authorization: Bearer <existing-admin-credential>" \
  -H "Content-Type: application/json" \
  -d '{"principal":"svc:breakglass-oncall","reason":"on-call break-glass for OIDC outages"}'

# Revoke it when the situation is resolved
curl -X DELETE http://localhost:7777/api/v1/admin/admins/svc:breakglass-oncall \
  -H "Authorization: Bearer <existing-admin-credential>"

# List who currently holds admin
curl http://localhost:7777/api/v1/admin/admins \
  -H "Authorization: Bearer <existing-admin-credential>"
```

### Using the key

Send the key as a bearer token against any protected endpoint:

```bash
curl -H "Authorization: Bearer a-long-random-high-entropy-secret" \
  http://localhost:7777/api/v1/graph
```

The [Web UI](../guides/web-ui/) exposes the same path: when OIDC is enabled the login
screen offers a **"Use a service-account key"** disclosure, and the key entered there is
sent as the same bearer header. The UI holds it in the browser's `sessionStorage` only —
cleared when the tab closes, never written to disk or a cookie. A valid human session
cookie takes **precedence** over a bearer key, so break-glass applies exactly when there
is no active human session — which is the situation it exists for.

### Confirming it is enforced

Authentication is always enforced in a running Joe: the daemon refuses to boot without
at least one of OIDC or service accounts, so there is no permit-all mode to fall through.
The startup log tells you which credentials are loaded:

| Startup log line | Meaning for break-glass |
| --- | --- |
| `API authentication enabled (OIDC login + service-account keys)` | Enforcing; service-account keys loaded — break-glass is a real boundary. |
| `API authentication enabled (service-account keys)` | Enforcing; service-account keys loaded — break-glass is a real boundary. |
| `API authentication enabled (OIDC login)` | Enforcing, but no service-account keys configured — there is no break-glass key to present. |

If the line mentions **service-account keys**, your break-glass key is loaded and
enforced. If it mentions only OIDC, add the account under `server.service_accounts` and
restart.

### What the audit shows

Break-glass use is recorded in the append-only audit log with action `break_glass_use`:
each row captures the `svc:<name>` principal presented, the source address, and an allow
decision. Read its evidentiary value precisely:

- It proves a **credential** was presented from an address and allowed — it does **not**
  identify which human used a shared key. Where attribution matters, give operators their
  own named accounts (`svc:operator-alice`) rather than a shared one.
- The log is **deduplicated** to at most one row per `(principal, source address)` per
  session-TTL window; the row count is not a request count. The dedup state is in memory,
  so a restart may add one extra row for an already-seen pair.
- The posture is **fail-open-but-loud**: an audit-write failure does not block a
  break-glass request — an audit outage must not lock operators out when they most need
  access — but the failure is logged loudly for follow-up.

For the identity model behind all of this, see [Principals and
identity](../concepts/principals-and-identity/); for zones and admin scope, see [RBAC,
zones, and the read posture](../concepts/rbac-zones-and-read-posture/).
