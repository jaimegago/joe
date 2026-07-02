---
title: Quickstart
weight: 20
description: From nothing to a running Joe reading a live cluster and answering one question, in observation mode.
---

# Quickstart

This tutorial takes you from an empty checkout to a running `joe` daemon that answers
one real question about your infrastructure — in **observation mode**, where Joe can
read and reason but cannot change anything. You will do nothing irreversible. Follow the
steps in order; each one builds on the last.

Joe knows nothing until you connect a system to it, so you register one Kubernetes
cluster *before* you ask anything. When you finish, you will have a Joe running locally,
in read-only observation mode, reading a live cluster and answering a question about it
through its real interaction surface.

> This is the on-rails path. For the full build-and-run procedure, the complete
> authentication options, and production setup, see [Install and Build](../install-and-build/).
> For *why* Joe works this way, see [Concepts](../concepts/).

## Before you start

You need three things installed:

- **Go 1.25 or newer**
- **Node.js and npm** (the web UI is built and embedded into the binary)
- **git**

You also need two credentials ready:

- An **Anthropic API key**, because Joe's default model is Claude. Have it ready as a
  string.
- Reach details for **one Kubernetes cluster**: its **API-server URL** and **CA bundle**
  (PEM), plus a **service-account bearer token**. This quickstart assumes Joe runs
  **outside** the cluster, so you put the token in an **environment variable in the
  daemon's environment** (e.g. `JOE_KUBERNETES_PROD_TOKEN`) and register a component by
  *reference* to that variable name — never by pasting the token. (If you instead run Joe
  *inside* the target cluster, you can tick the in-cluster option and Joe reads the
  pod-mounted service-account token directly — but that is not the path this quickstart
  takes.) Without a reachable cluster credential, Joe has nothing to read and the install
  is useless. A `kubectl --server <url> --token <token> get pods` that works is a good sign
  the reference will resolve. (For **AKS**, Joe also supports an **Entra exchange** method
  that mints a token from an Azure app registration instead of a static bearer token; this
  quickstart uses the static-bearer path. See
  [Register a Kubernetes component](../guides/register-kubernetes/) for both methods.)

## Step 1 — Build the binary

From the repository root:

```sh
make build
```

This builds the web UI, embeds it, and compiles a single `./joe` binary. There are no
release downloads — building from source is how you get `joe`.

## Step 2 — Set the three environment variables

Joe refuses to boot without an identity configured, and its default model needs a
provider key. In observation mode it stays read-only. These three variables cover all
three:

```sh
# Identity: a bearer key for a service account. Pick any long random string.
export JOE_API_KEY="pick-a-long-random-string"

# LLM provider: Joe's default model is Claude, so it needs an Anthropic key.
export ANTHROPIC_API_KEY="your-anthropic-api-key"

# Boot read-only: raise the write floor so Joe cannot mutate anything.
export JOE_MODE=observation
```

`JOE_API_KEY` is the smallest possible identity configuration — it creates a single
service account (principal `svc:server`) and is what satisfies Joe's refuse-to-boot
identity check. Skip it and the daemon will exit on startup rather than run
ungoverned.

## Step 3 — Start Joe

```sh
./joe
```

Joe starts on `localhost:7777`. You do not need a Joe *server* config file — Joe boots on
built-in defaults. That is separate from the **bearer-token variable** from *Before you
start*: Joe does not read it at boot, and you do not put it in a Joe config file. You name
that variable when you promote the cluster in Step 4, and Joe resolves it from its own
environment at connect time. In the startup logs you will see that the **write floor is up
(observation)**: Joe is read-only. Leave it running and open a second terminal for the
next step.

## Step 4 — Register one Kubernetes cluster

Joe is near-useless until it has a registered component to reason about, so connect one
before you ask anything. Do it through the web UI. Bringing a system under management is
always the same three beats: **register** it (it lands inert — recorded, but
credential-less and unable to act), **promote** it with a credential **reference** (never
a stored secret), then run a **connectivity test** that takes it live. For Kubernetes,
running outside the cluster, that reference is the cluster's **API-server URL** and **CA
bundle** plus the **bearer-token environment variable** you readied in *Before you start* —
the variable must be set where the daemon runs, since Joe resolves it there — and the test
brings the cluster live with no restart.

Registering and promoting are admin actions in the web UI, so this step needs a human
admin login rather than the service-account key from Step 2. The full click-by-click
procedure — logging in, registering, assigning a zone, promoting with a static-bearer
reference, and testing — lives in the guide:

> **[Register a Kubernetes component](../guides/register-kubernetes/)** — do the steps
> there, then come back.

When the component's connectivity test reports success, the cluster is live and Joe can
read it.

## Step 5 — Ask Joe about the cluster

Now that Joe has something real to read, send a message to its agentic task endpoint,
authenticating with the same `JOE_API_KEY` you set above:

```sh
curl -s http://localhost:7777/api/v1/tasks \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"message": "What Kubernetes workloads do you know about, and is anything unhealthy?"}'
```

Joe runs a full agentic turn and returns a JSON response; the answer is in the
`final_answer` field. It answers from the live cluster — still read-only, because the
write floor is up. You have gone from an empty install to a governed agent reading real
infrastructure, exercising the whole path end to end: identity, the LLM, the agent loop,
and a live component read.

## What you just did

- Built `joe` from source — the only supported way to get the binary.
- Gave Joe the minimal identity it requires (one service account via `JOE_API_KEY`),
  so it agreed to boot.
- Ran it in observation mode, with the write floor up, so nothing it did could change a
  managed system.
- Registered and promoted one Kubernetes cluster through the UI and took it live, so Joe
  had real infrastructure to read.
- Drove its real interaction surface — the agentic task endpoint — and got an answer back
  about that live cluster.

## Where to go next

- The full build, run, and authentication procedure (including OIDC login for humans
  and the admin bootstrap) → [Install and Build](../install-and-build/)
- Every configuration key and environment variable → [Configuration](../configuration/)
- Register the other system types Joe supports → [Components](../components/)
- The full Kubernetes register-and-promote walkthrough → [Register a Kubernetes component](../guides/register-kubernetes/)
- Understand observation mode, principals, and governance → [Concepts](../concepts/)
