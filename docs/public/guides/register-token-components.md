---
title: Register a GitHub, GitLab, or PagerDuty component
weight: 16
description: Bring GitHub, GitLab, or PagerDuty under Joe's management through the web UI — register it inert, promote it with an environment-variable reference to an API token, and confirm it is live.
---

# Register a GitHub, GitLab, or PagerDuty component

This how-to walks you, click by click, through bringing one **GitHub**, **GitLab**, or
**PagerDuty** account under Joe's management using the web UI. You will **register** the
component (it lands inert), **assign it a zone**, **promote** it with a reference to an
API token, and run a **connectivity test** to confirm it is live — no daemon restart.

The three types share one shape: each authenticates with a single API token, and Joe
reads that token from an environment variable in its own environment. You give Joe the
variable's *name*, never the token.

| Type | What the token is | Variable name |
| --- | --- | --- |
| `github` | A GitHub personal access token (classic or fine-grained) | `JOE_GITHUB_<LABEL>` |
| `gitlab` | A GitLab personal, group, or project access token | `JOE_GITLAB_<LABEL>` |
| `pagerduty` | A PagerDuty REST API key | `JOE_PAGERDUTY_<LABEL>` |

`<LABEL>` is your choice — `PROD`, `ACME`, anything that tells your components apart.

**What the web UI reaches.** The web UI connects these types to their public endpoints:
`api.github.com`, `gitlab.com`, and `api.pagerduty.com`. The Register dialog has no field
for a different endpoint, so for GitHub Enterprise Server, a self-managed GitLab, or
PagerDuty's EU service region, register through the API instead and set the endpoint in
the component's `config` — `base_url` for GitHub and GitLab, `api_url` for PagerDuty. See
[Components](../../components/) for the API flow.

Other static-credential types — Prometheus, Grafana, Argo CD, and the rest — need an
endpoint URL as well, so they follow that API path too. For a Git repository, see
[Register a Git repository](../register-git/); for a Kubernetes cluster, see
[Register a Kubernetes component](../register-kubernetes/).

For *why* the lifecycle is split the way it is, see
[The component lifecycle](../../concepts/component-lifecycle/).

## Before you start

- A running Joe with the web UI open and a signed-in session — see
  [The web UI and human login](../web-ui/).
- An **admin principal**. Registering, assigning a zone, and promoting are admin-gated.
  Running the connectivity test does not require admin.
- An API token for the service:
  - **GitHub** — a token with read access to the repositories whose pull requests you
    want Joe to read.
  - **GitLab** — a token with the `read_api` scope.
  - **PagerDuty** — a REST API key; a read-only key is enough.
- The token set in an environment variable **where the Joe daemon runs**, named
  `JOE_<TYPE>_<LABEL>` as in the table above — for example:

  ```sh
  export JOE_GITHUB_ACME="github_pat_..."
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
   - **Name** — a human label, e.g. `Acme GitHub`.
   - **Component ID** — prefilled from the Name (e.g. `acme-github`) and read-only by
     default; click **Edit** beside it to set your own. It is a permanent identifier used
     in URLs, zone assignments, and audit records: 1–63 lowercase letters, digits, and
     hyphens, starting and ending with a letter or digit.
   - **Type** — choose **`github`**, **`gitlab`**, or **`pagerduty`**.
3. Click **Register**.

No credential is collected here. The new row appears with its zone marked
**⚠ unassigned** and its arming state **inert**.

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
     example `JOE_GITHUB_ACME` — the **Credential reference** dropdown lists them by label.
     Choose yours.
   - **Enter a label.** If the dropdown is empty, or you click **Don't see it? Enter a
     label**, type the label into **Reference label** — `ACME`, not the full name. The
     dialog shows the full name it resolves to, e.g. `JOE_GITHUB_ACME`. If that variable
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
variable is missing, say — the component stays armed, and the next step is how you retry.

## Step 5 — Confirm it is live with a connectivity test

> Running the test does **not** require an admin principal.

1. In the component's detail card, click **Test Connection**.
2. On success you see **connection successful**, and the component's status moves to
   **connected**.

What the test proves differs by type:

- **PagerDuty** — Joe calls the PagerDuty API with the key, so success means the API is
  reachable and the key was accepted.
- **GitHub and GitLab** — Joe resolves the token but makes no API call when it connects,
  so success means the variable is set and holds a token. A wrong or under-scoped token
  shows up on the first real request, for example when you ask about a pull request in
  Step 6.

If the test fails, the message says why. `resolve credential: …` means the referenced
variable is not set where Joe runs; set it, restart Joe, and test again. To point the
component at a different variable, click **Re-arm** and choose it.

## Step 6 — Ask Joe about it

Open **Chat** and ask something that uses the component, for example:

- GitHub — *Summarize pull request 42 in acme/payments and walk me through its diff.*
- GitLab — *Summarize merge request 7 in project 1234.*
- PagerDuty — *Which PagerDuty incidents are triggered or acknowledged right now?*

Joe reads with the token you referenced. Commenting on or requesting changes to a pull
request is a separate permission, governed by the write floor and RBAC — arming a
component does not by itself grant it. See
[Observation mode and the write floor](../../concepts/observation-mode-and-the-write-floor/).

## Where to go next

- Every connectable type and its credential mechanism →
  [Connectable systems](../../components/connectable-systems/)
- Registering through the API, including types that need an endpoint URL →
  [Components](../../components/)
- Why registration and promotion are separate governed steps →
  [The component lifecycle](../../concepts/component-lifecycle/)
