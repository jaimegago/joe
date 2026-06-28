---
title: Register a Kubernetes component
weight: 15
description: Bring a Kubernetes cluster under Joe's management through the web UI — register it inert, promote it with a kubeconfig reference, and take it live.
---

# Register a Kubernetes component

This how-to walks you, click by click, through bringing one Kubernetes cluster under
Joe's management using the web UI. You will **register** the cluster (it lands inert),
**assign it a zone**, **promote** it with a kubeconfig reference, and run a **connectivity
test** that takes it live — no daemon restart. When you finish, Joe can read the cluster
and answer questions about it.

This page covers **Kubernetes only**. Other component types follow the same register →
promote → activate spine but differ in their credential mechanism and activation path
(some come live only at the next daemon restart). They are documented separately — see
[Integrations](../integrations/) for the per-type routing index, and the sibling
how-to pages as they are added.

For *why* the lifecycle is split the way it is — inert registration, governed promotion,
and credentials held as references rather than stored secrets — see
[Components and promotion](../concepts/components-and-promotion/).

## Before you start

- A running Joe with the web UI open, and a human session logged in. If you have not done
  that yet, follow [The web UI and human login](web-ui/) first.
- An **admin principal**. Registering, assigning a zone, and promoting are all
  admin-gated — the affordances below do not appear for a non-admin. Running the
  connectivity test does not require admin.
- A way for Joe to reach the cluster **from where the daemon runs**: either Joe runs
  inside the cluster (an in-cluster service account) or a kubeconfig file is present on
  the daemon's host. You supply a *reference* to one of these during promotion — never the
  credential contents. See [Components and promotion](../concepts/components-and-promotion/)
  for why.

## Step 1 — Open the Components page

In the left sidebar, click **Components**. This page lists every registered component with
its type, zone, connection status, and arming state (**inert** or **armed**).

> 📷 **Screenshot:** `images/guides/register-kubernetes/01-components-page.png` — the
> Components page, empty or with existing rows, showing the columns and the **+ Register
> Component** button.

## Step 2 — Register the cluster (admin) — it lands inert

> **Admin only.** The **+ Register Component** button appears only for an admin principal.

1. Click **+ Register Component** in the top-right of the page.
2. In the dialog:
   - **Component ID** — a stable identifier you choose, e.g. `prod-cluster`.
   - **Type** — open the selector and choose **`kubernetes`**. The type list is populated
     from Joe's authoritative type enum, so pick the value as the UI presents it.
   - **Name** — a human label, e.g. `Production cluster`.
3. Click **Register**.

No credential field is shown, and none is collected here: registration records the
component and nothing more. The new row appears with its zone marked **⚠ unassigned** and
its arming state **inert** — it can take no action yet. A confirmation message reminds you
that you must still assign a zone and promote it.

> 📷 **Screenshot:** `images/guides/register-kubernetes/02-register-dialog.png` — the
> Register Component dialog with **Type** set to `kubernetes` and the ID/Name fields
> filled.

## Step 3 — Assign the component a zone (admin)

A freshly registered component lands in no zone. Assign it one before promoting.

> **Admin only.** The Zones surface is admin-gated.

1. In the left sidebar, click **Zones**.
2. At the top of the page, an **unassigned components** panel lists the component you just
   registered.
3. Use the **Assign Zone** dropdown next to it and pick the zone this cluster belongs to.

The component moves out of the unassigned pool into the zone you chose. For what zones
mean and how they gate access, see
[RBAC, zones, and read posture](../concepts/rbac-zones-and-read-posture/).

> 📷 **Screenshot:** `images/guides/register-kubernetes/03-assign-zone.png` — the Zones
> page unassigned-components panel with the Assign Zone dropdown open.

## Step 4 — Promote the component (admin) — supply a kubeconfig reference

Promotion is the single governed transition from inert to **armed**. For Kubernetes it
collects a **kubeconfig-exec** reference: an in-cluster identity or a kubeconfig path. You
supply a *reference*, never an inline secret — the dialog offers no secret field by
construction.

> **Admin only.** The **Promote** button is shown only to an admin principal.

1. Back on the **Components** page, click the cluster's row to open its detail card.
2. Click **Promote**.
3. The promotion dialog renders the Kubernetes credential form:
   - **Kubeconfig path** — the path to a kubeconfig file on the daemon's host, e.g.
     `/etc/joe/kubeconfig`; **or**
   - **Use the in-cluster service account** — tick this checkbox instead when Joe runs
     inside the target cluster.
   - **Context** *(optional)* — a named context within the kubeconfig.
   - You must provide **at least one** of an in-cluster identity or a kubeconfig path.
4. Click **Continue**. A confirmation step states that arming grants the component a
   credentialed connection under its zone — a privileged, audited change. Confirm with
   **Promote**.

The row's arming state flips to **armed**. Promotion records the reference only; it does
**not** itself open a connection — that is the next step.

> Whatever you reference — the kubeconfig file or the in-cluster identity — must exist
> **where the Joe daemon runs**, because Joe resolves it from its own environment when it
> connects.

> 📷 **Screenshot:** `images/guides/register-kubernetes/04-promote-kubeconfig.png` — the
> Promote dialog showing the Kubeconfig path field, the optional Context field, and the
> in-cluster checkbox.

## Step 5 — Take it live with a connectivity test

Kubernetes is **runtime-registerable**: the connectivity test constructs the adapter,
authenticates with the armed reference, and registers the live connection immediately — no
restart needed.

> Running the test does **not** require an admin principal.

1. In the component's detail card, click **Test Connection**.
2. On success you see **connection successful**, and the component's status moves to
   **connected**. Joe can now read the cluster.

If the test reports a failure, the message describes what went wrong (for example, the
kubeconfig path is not present on the daemon's host, or the credential cannot reach the
API server). Fix the reference where Joe runs, or re-promote with a corrected locator, and
test again.

> 📷 **Screenshot:** `images/guides/register-kubernetes/05-test-connection.png` — the
> component detail card after a successful Test Connection, status shown as connected and
> arming as armed.

## Step 6 — Ask Joe about the cluster

Open **Chat** and ask something that exercises a cluster read, for example:

> What pods are running, and are any of them unhealthy?

Joe uses the live connection you just established to answer. A working credentialed
connection lets Joe **read** the cluster; whether Joe may *change* anything is governed
separately by the write floor and RBAC — arming a component does not by itself grant
mutation. See
[Observation mode and the write floor](../concepts/observation-mode-and-the-write-floor/).

## Screenshots to capture

These are captured separately against a running binary. Each placeholder above names the
target path under `images/guides/register-kubernetes/`:

1. `01-components-page.png` — the Components page with the **+ Register Component** button.
2. `02-register-dialog.png` — the Register dialog with Type set to `kubernetes`.
3. `03-assign-zone.png` — the Zones page unassigned panel with the Assign Zone dropdown.
4. `04-promote-kubeconfig.png` — the Promote dialog's kubeconfig-exec form.
5. `05-test-connection.png` — the detail card after a successful connectivity test.

## Where to go next

- The per-type routing index, including which types come live at runtime versus only at
  the next daemon restart → [Integrations](../integrations/)
- Why registration and promotion are two separate, governed steps →
  [Components and promotion](../concepts/components-and-promotion/)
- Zones and who may read a component → [RBAC, zones, and read posture](../concepts/rbac-zones-and-read-posture/)
