---
title: Register a Prometheus, Mimir, Loki, Tempo, Jaeger, Alertmanager, or Falco component
weight: 18
description: Bring a metrics, logs, traces, alerting, or runtime-security backend under Joe's management through the web UI — register it with its URL, promote it with a reference to a bearer token if it needs one, and confirm it is live.
---

# Register a Prometheus, Mimir, Loki, Tempo, Jaeger, Alertmanager, or Falco component

This how-to walks you, click by click, through bringing one **Prometheus**, **Mimir**,
**Loki**, **Tempo**, **Jaeger**, **Alertmanager**, or **Falco** backend under Joe's
management using the web UI. You will **register** the component with its URL (it lands
inert), **assign it a zone**, **promote** it with a reference to a bearer token, and run a
**connectivity test** to confirm it is live — no daemon restart.

The seven types share one shape. Each is reached at a URL you give at registration, and
each may be protected by a bearer token, which Joe reads from an environment variable in
its own environment. You give Joe the variable's *name*, never the token. The token is
optional to Joe: a backend that needs none is covered in
[If the backend needs no token](#if-the-backend-needs-no-token).

| Type | URL field in the dialog | What the URL is | Variable name |
| --- | --- | --- | --- |
| `prometheus` | **Prometheus URL** | The server's address, e.g. `http://prometheus.monitoring:9090` | `JOE_PROMETHEUS_<LABEL>` |
| `mimir` | **Mimir URL** | The address its Prometheus-compatible API is served under — Mimir puts that API behind a path prefix, `/prometheus` by default, e.g. `http://mimir.monitoring:8080/prometheus` | `JOE_MIMIR_<LABEL>` |
| `loki` | **Loki URL** | The server's address, e.g. `http://loki.monitoring:3100` | `JOE_LOKI_<LABEL>` |
| `tempo` | **Tempo URL** | The server's HTTP address, e.g. `http://tempo.monitoring:3200` | `JOE_TEMPO_<LABEL>` |
| `jaeger` | **Jaeger URL** | The Jaeger query service's address, e.g. `http://jaeger-query.tracing:16686` | `JOE_JAEGER_<LABEL>` |
| `alertmanager` | **Alertmanager URL** | The server's address, e.g. `http://alertmanager.monitoring:9093` | `JOE_ALERTMANAGER_<LABEL>` |
| `falco` | **Falcosidekick UI URL** | The address of the Falcosidekick UI, which is where Joe reads Falco's events from, e.g. `http://falcosidekick-ui.falco:2802` | `JOE_FALCO_<LABEL>` |

`<LABEL>` is your choice — `PROD`, `EU`, anything that tells your components apart.

The URL has to be reachable **from where the Joe daemon runs**, not from your browser.

A component of one of these types that was registered before Joe required the URL at
registration, and has none, cannot be given one later: delete it, register it again with
the URL, and re-assign its security zone.

Grafana, Argo CD, Splunk, Dynatrace, and New Relic need a token as well as a URL or an
account ID — see
[Register a Grafana, Argo CD, Splunk, Dynatrace, or New Relic component](../register-url-token-components/).
Terraform and Envoy have no credential at all — see
[Register a Terraform or Envoy component](../register-terraform-envoy/).

For *why* the lifecycle is split the way it is, see
[The component lifecycle](../../concepts/component-lifecycle/).

## Before you start

- A running Joe with the web UI open and a signed-in session — see
  [The web UI and human login](../web-ui/).
- An **admin principal**. Registering, assigning a zone, and promoting are admin-gated.
  Running the connectivity test does not require admin.
- The backend's **URL**, as in the table above.
- If the backend requires a bearer token, that token set in an environment variable
  **where the Joe daemon runs**, named `JOE_<TYPE>_<LABEL>` as in the table above — for
  example:

  ```sh
  export JOE_PROMETHEUS_PROD="..."
  ```

  Joe sends the variable's value as an `Authorization: Bearer` header on every request to
  the backend. It reads its own process environment, so a variable has to be present when
  the daemon starts. If you set it after Joe is already running, restart Joe before you
  promote.

## Step 1 — Open the Components page

In the left sidebar, click **Components**. This page lists every registered component with
its type, zone, connection status, and arming state (**inert** or **armed**).

## Step 2 — Register the component (admin) — it lands inert

> **Admin only.** The **+ Register Component** button appears only for an admin principal.

1. Click **+ Register Component** in the top-right of the page.
2. In the dialog:
   - **Name** — a human label, e.g. `Prod metrics`.
   - **Component ID** — prefilled from the Name (e.g. `prod-metrics`) and read-only by
     default; click **Edit** beside it to set your own. It is a permanent identifier used
     in URLs, zone assignments, and audit records: 1–63 lowercase letters, digits, and
     hyphens, starting and ending with a letter or digit.
   - **Type** — choose the type. The type's fields appear below it.
   - The **URL** field for the type — required, and marked with an asterisk. It must be an
     absolute URL such as `http://host:port`; the dialog says so under the field and keeps
     **Register** disabled until it is.
   - **Tenant ID (X-Scope-OrgID)** *(optional)* — shown for `prometheus`, `mimir`, `loki`,
     and `tempo`. Fill it in only for a multi-tenant backend; Joe then sends it as the
     `X-Scope-OrgID` header on every request.
3. Click **Register**.

No credential is collected here. The new row appears with its zone marked
**⚠ unassigned** and its arming state **inert**. Joe has not contacted the backend yet.

The URL and the tenant ID cannot be edited afterwards. To change either, remove the
component and register it again.

## Step 3 — Assign the component a zone (admin)

A freshly registered component lands in no zone. Assign it one before promoting.

1. In the left sidebar, click **Zones**.
2. In the **unassigned components** panel at the top, find the component you just
   registered.
3. Pick its zone from the **Assign Zone** dropdown next to it.

For what zones mean and how they gate access, see
[RBAC, zones, and read posture](../../concepts/rbac-zones-and-read-posture/).

## Step 4 — Promote the component (admin) — reference the token

Promotion is the single governed transition from inert to **armed**. For these types it
collects one thing: which environment variable holds the bearer token.

> **Admin only.** The **Promote** button is shown only to an admin principal.

1. Back on the **Components** page, click the component's row to open its detail card.
2. Click **Promote**.
3. Choose the credential reference. The dialog lists the matching variables currently set
   in Joe's environment, so there are two ways to fill it:
   - **Pick an existing reference.** If Joe can see variables with the type's prefix — for
     example `JOE_PROMETHEUS_PROD` — the **Credential reference** dropdown lists them by
     label. Choose yours.
   - **Enter a label.** If the dropdown is empty, or you click **Don't see it? Enter a
     label**, type the label into **Reference label** — `PROD`, not the full name. The
     dialog shows the full name it resolves to, e.g. `JOE_PROMETHEUS_PROD`. If that
     variable is not currently set where Joe runs, the dialog warns you. You can still
     arm, but the component cannot connect until the variable exists and Joe has been
     restarted to see it.
4. Click **Continue**. A confirmation step states that arming grants the component a
   credentialed connection under its zone — a privileged, audited change. Confirm with
   **Promote**.

Each component needs **its own** variable. Promotion refuses a variable name another
component already uses, because two components sharing one name would read the same
secret.

The row's arming state flips to **armed**. Promotion also brings the connection up
straight away, using the variable you referenced. If that first connect fails — the
variable is missing, say, or the URL is unreachable — the component stays armed, and the
next step is how you retry.

### If the backend needs no token

Many of these backends run with no authentication. Joe treats the token as optional for
these seven types: it sends an `Authorization` header only when it holds a token.

The Promote dialog has no no-credential choice for them — it always asks for a variable —
so a backend that needs no token is not promoted. Skip Step 4 and go to Step 5: the
connectivity test connects the component with no token and brings it live. Its arming
state stays **inert**, because no credential reference was ever recorded, and Joe
reconnects it the same way each time the daemon starts.

If the backend gains authentication later, set the variable, restart Joe, and promote the
component then.

## Step 5 — Confirm it is live with a connectivity test

> Running the test does **not** require an admin principal.

1. In the component's detail card, click **Test Connection**.
2. On success you see **connection successful**, and the component's status moves to
   **connected**.

The test makes one request to the backend, with the token if there is one and the tenant
ID if you set one, and succeeds when the backend answers it with HTTP 200:

| Type | Request |
| --- | --- |
| `prometheus`, `mimir` | `GET <URL>/api/v1/status/buildinfo` |
| `loki` | `GET <URL>/loki/api/v1/labels` |
| `tempo` | `GET <URL>/api/status` |
| `jaeger` | `GET <URL>/api/services` |
| `alertmanager` | `GET <URL>/api/v2/status` |
| `falco` | `GET <URL>/api/v1/events?limit=1` |

So success means the URL is reachable from Joe and the backend accepted that request.
Whether it also proves the token depends on the backend: one that answers the request
above without checking credentials will pass with a wrong token, and the error then shows
up on the first real query in Step 6.

If the test fails, the message says why:

- `connect to Prometheus at <URL>: …` (or the type's own name) — Joe could not reach the
  URL. Check that it resolves and is reachable from where Joe runs.
- `prometheus health check failed (status 401): …` (or the type's own name, and the status
  the backend returned) — the backend answered and refused. A `401` or `403` points at the
  token or the tenant ID; a `404` usually means the URL is missing a path prefix, or has
  one it should not.
- `resolve credential: …` — the referenced variable is not set where Joe runs. Set it,
  restart Joe, and test again.

To point the component at a different variable, click **Re-arm** and choose it.

## Step 6 — Ask Joe about it

Open **Chat** and ask something that uses the component, for example:

- Prometheus or Mimir — *What is the 5xx rate of the payments service over the last hour,
  according to prod-metrics?*
- Loki — *Show me error logs from the payments namespace in the last 15 minutes.*
- Tempo — *Find traces for the checkout service that took longer than two seconds.*
- Jaeger — *Which services does Jaeger know about, and what do recent checkout traces look
  like?*
- Alertmanager — *Which alerts are active with severity=critical?*
- Falco — *List recent Falco events at Warning priority or higher.*

Joe queries the backend with PromQL, LogQL, or the backend's own search, and reads only.

## Where to go next

- Grafana, Argo CD, Splunk, Dynatrace, and New Relic →
  [Register a Grafana, Argo CD, Splunk, Dynatrace, or New Relic component](../register-url-token-components/)
- Terraform and Envoy →
  [Register a Terraform or Envoy component](../register-terraform-envoy/)
- Every connectable type and its credential mechanism →
  [Connectable systems](../../components/connectable-systems/)
- Why registration and promotion are separate governed steps →
  [The component lifecycle](../../concepts/component-lifecycle/)
