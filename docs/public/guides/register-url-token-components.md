---
title: Register a Grafana, Argo CD, Splunk, Dynatrace, or New Relic component
weight: 19
description: Bring Grafana, Argo CD, Splunk, Dynatrace, or New Relic under Joe's management through the web UI — register it with its URL or account ID, promote it with an environment-variable reference to an API token, and confirm it is live.
---

# Register a Grafana, Argo CD, Splunk, Dynatrace, or New Relic component

This how-to walks you, click by click, through bringing one **Grafana**, **Argo CD**,
**Splunk**, **Dynatrace**, or **New Relic** account under Joe's management using the web
UI. You will **register** the component with the field that locates it (it lands inert),
**assign it a zone**, **promote** it with a reference to an API token, and run a
**connectivity test** to confirm it is live — no daemon restart.

The five types share one shape. Each needs a locator at registration — a URL, or for New
Relic an account ID — and each authenticates with a single API token, which Joe reads from
an environment variable in its own environment. You give Joe the variable's *name*, never
the token. Unlike the backends in
[Register a Prometheus, Mimir, Loki, Tempo, Jaeger, Alertmanager, or Falco component](../register-url-components/),
these five cannot connect without a token.

| Type | Required at registration | Optional at registration | What the token is | Variable name |
| --- | --- | --- | --- | --- |
| `grafana` | **Grafana URL**, e.g. `https://grafana.example.com` | — | A Grafana service account token | `JOE_GRAFANA_<LABEL>` |
| `argocd` | **Argo CD URL**, e.g. `https://argocd.example.com` | **Skip TLS verification** | An Argo CD API token | `JOE_ARGOCD_<LABEL>` |
| `splunk` | **Splunk URL** — the REST management address, e.g. `https://splunk.example.com:8089` | **Default index** | A Splunk authentication token | `JOE_SPLUNK_<LABEL>` |
| `dynatrace` | **Environment URL**, e.g. `https://abc12345.live.dynatrace.com` | — | A Dynatrace API token that can read metrics | `JOE_DYNATRACE_<LABEL>` |
| `newrelic` | **Account ID** — a whole number | **Region** | A New Relic user API key | `JOE_NEWRELIC_<LABEL>` |

`<LABEL>` is your choice — `PROD`, `ACME`, anything that tells your components apart.

A URL has to be reachable **from where the Joe daemon runs**, not from your browser.

A component of one of these types that was registered before Joe required its URL or
account ID at registration, and has none, cannot be given one later: delete it, register
it again with the field, and re-assign its security zone.

For *why* the lifecycle is split the way it is, see
[The component lifecycle](../../concepts/component-lifecycle/).

## Before you start

- A running Joe with the web UI open and a signed-in session — see
  [The web UI and human login](../web-ui/).
- An **admin principal**. Registering, assigning a zone, and promoting are admin-gated.
  Running the connectivity test does not require admin.
- The component's **URL**, or for New Relic its **account ID**, as in the table above.
- An API token for the service, read-only where the service lets you scope it.
- The token set in an environment variable **where the Joe daemon runs**, named
  `JOE_<TYPE>_<LABEL>` as in the table above — for example:

  ```sh
  export JOE_GRAFANA_PROD="glsa_..."
  ```

  Joe reads its own process environment, so a variable has to be present when the daemon
  starts. If you set it after Joe is already running, restart Joe before you promote.

## Step 1 — Open the Components page

In the left sidebar, click **Components**. This page lists every registered component with
its type, zone, connection status, and arming state (**inert** or **armed**).

## Step 2 — Register the component (admin) — it lands inert

> **Admin only.** The **+ Register Component** button appears only for an admin principal.

1. Click **+ Register Component** in the top-right of the page.
2. In the dialog:
   - **Name** — a human label, e.g. `Prod Grafana`.
   - **Component ID** — prefilled from the Name (e.g. `prod-grafana`) and read-only by
     default; click **Edit** beside it to set your own. It is a permanent identifier used
     in URLs, zone assignments, and audit records: 1–63 lowercase letters, digits, and
     hyphens, starting and ending with a letter or digit.
   - **Type** — choose the type. The type's fields appear below it, the required one
     marked with an asterisk.
   - For `grafana`, `argocd`, `splunk`, and `dynatrace`, the **URL** field. It must be an
     absolute URL such as `https://host:port`; the dialog says so under the field and
     keeps **Register** disabled until it is.
   - For `newrelic`, **Account ID**. It is a whole number, and the dialog refuses
     anything else.
   - The type's optional field, if it has one:
     - **Skip TLS verification** (`argocd`) — tick it only for an Argo CD server whose
       certificate Joe cannot verify, such as a self-signed one. Joe then accepts any
       certificate from that server.
     - **Default index** (`splunk`) — Joe records it with the component and does not use
       it yet. Leave it empty.
     - **Region** (`newrelic`) — leave it empty for the US region. For the EU region type
       `EU`, in capitals; any other value is treated as US.
3. Click **Register**.

No credential is collected here. The new row appears with its zone marked
**⚠ unassigned** and its arming state **inert**. Joe has not contacted the service yet.

None of these fields can be edited afterwards. To change one, remove the component and
register it again.

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
collects one thing: which environment variable holds the token.

> **Admin only.** The **Promote** button is shown only to an admin principal.

1. Back on the **Components** page, click the component's row to open its detail card.
2. Click **Promote**.
3. Choose the credential reference. The dialog lists the matching variables currently set
   in Joe's environment, so there are two ways to fill it:
   - **Pick an existing reference.** If Joe can see variables with the type's prefix — for
     example `JOE_GRAFANA_PROD` — the **Credential reference** dropdown lists them by
     label. Choose yours.
   - **Enter a label.** If the dropdown is empty, or you click **Don't see it? Enter a
     label**, type the label into **Reference label** — `PROD`, not the full name. The
     dialog shows the full name it resolves to, e.g. `JOE_GRAFANA_PROD`. If that variable
     is not currently set where Joe runs, the dialog warns you. You can still arm, but the
     component cannot connect until the variable exists and Joe has been restarted to
     see it.
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

## Step 5 — Confirm it is live with a connectivity test

> Running the test does **not** require an admin principal.

1. In the component's detail card, click **Test Connection**.
2. On success you see **connection successful**, and the component's status moves to
   **connected**.

The test makes one request to the service with the token, and succeeds when the service
answers it without an error. What that proves differs by type:

- **Splunk** — Joe requests the server's info from the management API
  (`/services/server/info`), so success means the URL is reachable and Splunk accepted the
  token.
- **Dynatrace** — Joe lists one metric (`/api/v2/metrics`), so success means the URL is
  reachable and the token is accepted and allowed to read metrics.
- **New Relic** — Joe runs a small NerdGraph query for the key's own user, against the
  region you registered, so success means New Relic accepted the key. The account ID is
  not part of the check: a wrong one passes here.
- **Grafana and Argo CD** — Joe requests Grafana's health endpoint (`/api/health`) or
  Argo CD's version endpoint (`/api/version`). Both services normally answer those without
  checking the token, so success means the URL is reachable and the variable holds a
  token, not that the token is accepted. A wrong or under-scoped token shows up on the
  first real request, for example when you ask about a dashboard or an application in
  Step 6.

If the test fails, the message says why:

- `token is required: promote the component with a credential reference`, or
  `api_key is required: …` — the component has not been promoted, or the referenced
  variable is set but empty.
- `resolve credential: …` — the referenced variable is not set where Joe runs. Set it,
  restart Joe, and test again.
- `connect to Grafana at <URL>: …` (or the type's own name) — Joe could not reach the URL.
  For Argo CD the message starts `ping Argo CD at <URL>`, and a certificate Joe cannot
  verify fails here too.
- `grafana health check failed (status 401): …` (or the type's own name, and the status
  the service returned) — the service answered and refused the request.
- `connect to New Relic: …` — New Relic could not be reached, or refused the key; the rest
  of the message carries the status it returned.

To point the component at a different variable, click **Re-arm** and choose it.

## Step 6 — Ask Joe about it

For **Grafana** and **Argo CD**, open **Chat** and ask something that uses the component,
for example:

- Grafana — *Find the Grafana dashboards for the payments service, and tell me which
  Grafana alerts are active.*
- Argo CD — *Which Argo CD applications are out of sync, and what changed in the last sync
  of payments?*

Joe reads with the token you referenced. Arming a component does not by itself grant any
change to the service — that is governed separately by the write floor and RBAC; see
[Observation mode and the write floor](../../concepts/observation-mode-and-the-write-floor/).

For **Splunk**, **Dynatrace**, and **New Relic**, Joe does not yet have a tool that
queries the service, so there is nothing to ask it in chat. A connected component of these
types is listed on the Components page and appears as a node in the
[knowledge graph](../../concepts/knowledge-graph/), and that is all it does today.

## Where to go next

- Backends that need a URL and, at most, a bearer token →
  [Register a Prometheus, Mimir, Loki, Tempo, Jaeger, Alertmanager, or Falco component](../register-url-components/)
- GitHub, GitLab, and PagerDuty →
  [Register a GitHub, GitLab, or PagerDuty component](../register-token-components/)
- Every connectable type and its credential mechanism →
  [Connectable systems](../../components/connectable-systems/)
- Why registration and promotion are separate governed steps →
  [The component lifecycle](../../concepts/component-lifecycle/)
