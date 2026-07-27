---
title: Register a Kubernetes component
weight: 15
description: Bring a Kubernetes cluster under Joe's management through the web UI — register it inert, promote it with a static-bearer or Entra-exchange reference, and take it live.
---

# Register a Kubernetes component

This how-to walks you, click by click, through bringing one Kubernetes cluster under
Joe's management using the web UI. You will **register** the cluster (it lands inert),
**assign it a zone**, **promote** it with a credential reference, and run a **connectivity
test** that takes it live — no daemon restart. When you finish, Joe can read the cluster
and answer questions about it.

Joe offers **two Kubernetes authentication methods**, both native and both authenticating
Joe as its own non-human identity:

- **Static bearer** — a long-lived bearer token applied as an `Authorization: Bearer`
  header, for clusters that issue a ServiceAccount token (OpenShift, self-managed, and
  local clusters). The token comes from an environment variable in the daemon's
  environment, or — when Joe runs inside the target cluster — the pod-mounted
  service-account token.
- **Entra exchange** — for AKS, Joe mints a short-lived bearer token through an Azure
  Entra OAuth2 client-credentials exchange using an app registration's client secret.

You choose the method in the Promote dialog. Either way you supply a *reference* to a
credential, never the secret itself, and Joe never ingests a human's kubeconfig.

This page covers **Kubernetes only**. Other component types follow the same register →
promote → activate spine but differ in their credential mechanism and activation path
(some come live only at the next daemon restart). They are documented separately — see
[Components](../../components/) for the per-type routing index, and the sibling
how-to pages as they are added.

For *why* the lifecycle is split the way it is — inert registration, governed promotion,
and credentials held as references rather than stored secrets — see
[The component lifecycle](../../concepts/component-lifecycle/).

## Before you start

- A running Joe with the web UI open and a signed-in session. Either sign-in path works:
  a human OIDC login, covered in [The web UI and human login](../web-ui/), or a
  service-account key, which is what the [Quickstart](../../quickstart/) uses.
- An **admin principal**. Registering, assigning a zone, and promoting are all
  admin-gated — the affordances below do not appear for a non-admin. Admin is held by a
  principal, whichever way that principal signed in. Running the connectivity test does
  not require admin.
- A way for Joe to reach the cluster **from where the daemon runs**: the cluster's
  API-server URL and CA bundle, plus the credential for your chosen method —
  - for **static bearer**, a service-account bearer token, either in an environment
    variable in the daemon's environment or the pod-mounted token when Joe runs inside the
    cluster;
  - for **Entra exchange**, the Azure tenant ID, the app registration's client (application)
    ID, the AKS audience the token is scoped to, and an environment variable holding the
    app registration's client secret.

  You supply a *reference* to the credential during promotion — never the credential
  contents. See [The component lifecycle](../../concepts/component-lifecycle/) for why.

## Step 1 — Open the Components page

In the left sidebar, click **Components**. This page lists every registered component with
its type, zone, connection status, and arming state (**inert** or **armed**).

## Step 2 — Register the cluster (admin) — it lands inert

> **Admin only.** The **+ Register Component** button appears only for an admin principal.

1. Click **+ Register Component** in the top-right of the page.
2. In the dialog:
   - **Name** — a human label, e.g. `Production cluster`.
   - **Component ID** — prefilled automatically from the Name (e.g. `production-cluster`)
     and read-only by default; click **Edit** beside it to set your own. It is a permanent
     identifier used in URLs, zone assignments, and audit records, and must be 1–63
     characters of lowercase letters, digits, and hyphens, starting and ending with a
     letter or digit.
   - **Type** — open the selector and choose **`kubernetes`**. The type list is populated
     from Joe's authoritative type enum, so pick the value as the UI presents it.
3. Click **Register**.

No credential field is shown, and none is collected here: registration records the
component and nothing more. The new row appears with its zone marked **⚠ unassigned** and
its arming state **inert** — it can take no action yet. A confirmation message reminds you
that you must still assign a zone and promote it.

## Step 3 — Assign the component a zone (admin)

A freshly registered component lands in no zone. Assign it one before promoting.

> **Admin only.** The Zones surface is admin-gated.

1. In the left sidebar, click **Zones**.
2. At the top of the page, an **unassigned components** panel lists the component you just
   registered.
3. Use the **Assign Zone** dropdown next to it and pick the zone this cluster belongs to.

The component moves out of the unassigned pool into the zone you chose. For what zones
mean and how they gate access, see
[RBAC, zones, and read posture](../../concepts/rbac-zones-and-read-posture/).

## Step 4 — Promote the component (admin) — supply a credential reference

Promotion is the single governed transition from inert to **armed**. For Kubernetes it
collects the cluster coordinates plus a credential reference for your chosen
authentication method. You supply a *reference*, never an inline secret — the dialog
offers no secret field by construction. Joe authenticates as its own non-human identity
and never ingests a human's kubeconfig.

> **Admin only.** The **Promote** button is shown only to an admin principal.

1. Back on the **Components** page, click the cluster's row to open its detail card.
2. Click **Promote**.
3. The promotion dialog renders the Kubernetes credential form. First, the **cluster
   coordinates**, shared by both methods:
   - **API-server URL** — the cluster's API-server endpoint, e.g.
     `https://api.cluster.example.com:6443`.
   - **CA bundle** *(PEM, optional)* — the cluster CA certificate, stored inline so the
     record is self-contained.
   - **Default namespace** *(optional)*.
4. Choose the **Authentication method**, then fill the fields it reveals:
   - **Static bearer token** — supply **either** a **Bearer-token environment variable**
     (the variable in the daemon's environment holding the service-account bearer token —
     Joe stores the name, never the value) **or** tick **Use the in-cluster service-account
     token instead** when Joe runs inside the target cluster and reads the pod-mounted
     token directly. You must provide at least one of the two.
   - **Entra exchange** — supply the **Azure tenant ID**, the **Application (client) ID**,
     the **Audience (scope)** the minted token is scoped to (Joe requests the `/.default`
     scope for it), and the **Client-secret environment variable** holding the app
     registration's client secret. Joe stores the variable name, never the secret. One app
     registration can serve several clusters, so this variable name **may be shared** across
     components.
5. Click **Continue**. A confirmation step states that arming grants the component a
   credentialed connection under its zone — a privileged, audited change. Confirm with
   **Promote**.

The row's arming state flips to **armed**. Promotion records the reference only; it does
**not** itself open a connection — that is the next step.

> The credential variable (the bearer-token variable, the in-cluster token, or the
> client-secret variable) must exist **where the Joe daemon runs**, because Joe resolves it
> from its own environment when it connects.

## Step 5 — Confirm it is live with a connectivity test

Kubernetes is **runtime-registerable**: a successful promote already brought the connection
up, so Joe begins reading the cluster on its next refresh — no restart needed. The
connectivity test confirms that, and is also how you retry if promotion armed the component
but could not reach the cluster at that moment.

> Running the test does **not** require an admin principal.

1. In the component's detail card, click **Test Connection**.
2. On success you see **connection successful**, and the component's status moves to
   **connected**. Joe can now read the cluster.

If the test reports a failure, the message describes what went wrong (for example, the
bearer-token or client-secret environment variable is not set where the daemon runs, the
Entra exchange was refused, or the credential cannot reach the API server). Fix the
reference where Joe runs, or re-promote with a corrected locator, and test again.

## What permissions Joe needs

Joe reads your cluster with the credential you promoted, so what it can see is
bounded by what that credential is allowed to **list**. Joe is built to run with
Kubernetes' built-in **`view`** ClusterRole — a standard, read-only role.

With `view` bound to Joe's ServiceAccount, the graph populates fully **except for
secret nodes**: the `view` role deliberately excludes `secrets`. When Joe cannot
list a resource type, it does **not** fail the whole refresh — it records what it
skipped and the component's status moves to **degraded**, with the detail naming
what was left out (for example, that secrets were skipped for lack of list
permission). Everything the credential *can* read still populates. You will see
this on the component's detail card after the first refresh interval.

If you want the graph to include secret nodes, grant Joe **`list` on `secrets`**
as an explicit, opt-in addition to `view` — for example a small extra Role that
adds `secrets` to the `list` verb. This is entirely optional. When you do, the
degraded status clears on the next refresh and the secret nodes appear.

> **What a secret node contains.** Even with the secrets grant, Joe records only
> each secret's **key names** and object metadata (name, namespace, labels) — it
> **never** records secret values. Granting `list` on secrets lets Joe map which
> workloads reference which secrets; it does not put secret contents in the graph.

Because `view` is a stable, well-known role, the simplest setup is: bind `view`,
accept the degraded-on-secrets status, and add the secrets grant only if you want
secret nodes in the graph.

## Step 6 — Ask Joe about the cluster

Open **Chat** and ask something that exercises a cluster read, for example:

> What pods are running, and are any of them unhealthy?

Joe uses the live connection you just established to answer. A working credentialed
connection lets Joe **read** the cluster; whether Joe may *change* anything is governed
separately by the write floor and RBAC — arming a component does not by itself grant
mutation. See
[Observation mode and the write floor](../../concepts/observation-mode-and-the-write-floor/).

## Where to go next

- The per-type routing index, including which types come live at runtime versus only at
  the next daemon restart → [Components](../../components/)
- Why registration and promotion are two separate, governed steps →
  [The component lifecycle](../../concepts/component-lifecycle/)
- Zones and who may read a component → [RBAC, zones, and read posture](../../concepts/rbac-zones-and-read-posture/)
