---
title: Register a Terraform or Envoy component
weight: 20
description: Bring a Terraform state file or an Envoy admin API under Joe's management through the web UI — register it with its path or URL, assign it a zone, and confirm it is live. These two types have no credential, so there is no promotion step.
---

# Register a Terraform or Envoy component

This how-to walks you, click by click, through bringing one **Terraform** state file or
one **Envoy** proxy under Joe's management using the web UI. You will **register** the
component with the field that locates it, **assign it a zone**, and run a **connectivity
test**, which brings it live — no daemon restart.

These are the two **credential-less** types. Joe reads a Terraform state file from its
own filesystem and an Envoy proxy through its unauthenticated admin API, so there is no
credential to reference and **no promotion step**: a component of either type is never
promoted, and its arming state reads **inert** for as long as it exists. That is the
expected state, not an unfinished one.

| Type | Required at registration | Optional at registration |
| --- | --- | --- |
| `terraform` | **State file path** — a `.tfstate` file, or a directory holding `terraform.tfstate`, on the host Joe runs on | — |
| `envoy` | **Admin API URL**, e.g. `http://envoy.edge:9901` | **Request timeout (ms)** |

A component of one of these types that was registered before Joe required the path or URL
at registration, and has none, cannot be given one later: delete it, register it again
with the field, and re-assign its security zone.

Every other type has a credential and a promotion step — see
[Connectable systems](../../components/connectable-systems/) for the list and its guides.
For *why* registration and promotion are separate, see
[The component lifecycle](../../concepts/component-lifecycle/).

## Before you start

- A running Joe with the web UI open and a signed-in session — see
  [The web UI and human login](../web-ui/).
- An **admin principal**. Registering and assigning a zone are admin-gated. Running the
  connectivity test does not require admin.
- For **Terraform**, a state file the Joe daemon can read **on its own host** — a local
  file or a mounted one. Joe does not read a remote backend such as an object-storage
  bucket; it opens the path you give it. A state file can hold secrets in its resource
  attributes. Joe redacts outputs that the state marks sensitive; treat everything else in
  the file as readable by anyone who may ask Joe about this component.
- For **Envoy**, the proxy's **admin API** address, reachable from where the Joe daemon
  runs. The admin API has no authentication of its own, so anything that can reach that
  address can read it; Joe only reads.

## Step 1 — Open the Components page

In the left sidebar, click **Components**. This page lists every registered component with
its type, zone, connection status, and arming state.

## Step 2 — Register the component (admin)

> **Admin only.** The **+ Register Component** button appears only for an admin principal.

1. Click **+ Register Component** in the top-right of the page.
2. In the dialog:
   - **Name** — a human label, e.g. `Network state` or `Edge proxy`.
   - **Component ID** — prefilled from the Name (e.g. `network-state`) and read-only by
     default; click **Edit** beside it to set your own. It is a permanent identifier used
     in URLs, zone assignments, and audit records: 1–63 lowercase letters, digits, and
     hyphens, starting and ending with a letter or digit.
   - **Type** — choose **`terraform`** or **`envoy`**. The type's fields appear below it.
   - For `terraform`, **State file path** — required. Give an absolute path, such as
     `/var/lib/joe/state/network.tfstate`, or a directory that holds a file named
     `terraform.tfstate`. Registration records the path as written and does not check
     that it exists; the connectivity test does.
   - For `envoy`, **Admin API URL** — required. It must be an absolute URL such as
     `http://host:9901`, and the dialog keeps **Register** disabled until it is. Enter it
     without a trailing slash: Joe appends each admin path directly to it.
   - For `envoy`, **Request timeout (ms)** *(optional)* — how long Joe waits for the admin
     API to answer. Leave it empty for the default, 10000.
3. Click **Register**.

The new row appears with its zone marked **⚠ unassigned** and its arming state **inert**.
Joe has not opened the file or contacted the proxy yet.

The dialog's own text, and the confirmation message after you register, tell you to
promote the component to supply credentials. That wording is written for the types that
have a credential. These two have none, so skip it.

None of these fields can be edited afterwards. To change one, remove the component and
register it again.

## Step 3 — Assign the component a zone (admin)

A freshly registered component lands in no zone. Assign it one.

1. In the left sidebar, click **Zones**.
2. In the **unassigned components** panel at the top, find the component you just
   registered.
3. Pick its zone from the **Assign Zone** dropdown next to it.

For what zones mean and how they gate access, see
[RBAC, zones, and read posture](../../concepts/rbac-zones-and-read-posture/).

## Step 4 — Bring it live with a connectivity test

> Running the test does **not** require an admin principal.

1. Back on the **Components** page, click the component's row to open its detail card.
2. Click **Test Connection**.
3. On success you see **connection successful**, and the component's status moves to
   **connected**.

For these two types the test is what connects the component the first time; after that,
Joe reconnects it by itself each time the daemon starts.

What the test does:

- **Terraform** — Joe opens the path, or `terraform.tfstate` inside it if it is a
  directory, reads the file, and checks that it is JSON. Success means the daemon can read
  the file. Joe reads the file again each time you ask about it, so a newer state is
  picked up without another test.
- **Envoy** — Joe requests `<URL>/server_info` and succeeds when the proxy answers with
  HTTP 200.

If the test fails, the message says why:

- `resolve state path: …` — the path does not exist as the daemon sees it, or is a
  directory with no `terraform.tfstate` in it.
- `read state file …` — the file exists and the daemon cannot read it; check its
  permissions.
- `parse state file …: not valid Terraform JSON state` — the file is not JSON.
- `connect to envoy admin: …` — Joe could not reach the admin API URL, or it did not
  answer within the timeout.
- `envoy admin returned status …` — something answered at that URL, and not with the
  admin API's `server_info`.

The detail card also shows a **Promote** button to an admin. For these types it opens a
dialog saying the type cannot be armed because no credential provider is wired for it.
Close it; there is nothing to do there.

## Step 5 — Ask Joe about it

Open **Chat** and ask something that uses the component, for example:

- Terraform — *Which aws_instance resources does network-state manage, and what are its
  outputs?*
- Envoy — *Which upstream clusters on edge-proxy have unhealthy hosts?*

For Terraform, Joe can list the managed resources in the state, look one up by its
address, and list the outputs. For Envoy, it can list the upstream clusters with the
health of their hosts, read a section of the configuration dump, and read statistics by
name prefix. Both are read-only.

## Where to go next

- Types that are promoted with a token reference →
  [Register a Prometheus, Mimir, Loki, Tempo, Jaeger, Alertmanager, or Falco component](../register-url-components/)
  and
  [Register a Grafana, Argo CD, Splunk, Dynatrace, or New Relic component](../register-url-token-components/)
- Every connectable type and its credential mechanism →
  [Connectable systems](../../components/connectable-systems/)
- Why registration and promotion are separate governed steps →
  [The component lifecycle](../../concepts/component-lifecycle/)
