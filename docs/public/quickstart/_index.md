---
title: Quickstart
weight: 20
description: From nothing to a running Joe reading a live cluster and answering one question, in observation mode.
---

# Quickstart

This tutorial takes you from nothing to a running `joe` daemon that answers
one real question about your infrastructure — in **observation mode**, where Joe can
read and reason but cannot change anything. You will do nothing irreversible. Follow the
steps in order; each one builds on the last.

Joe knows nothing until you connect a system to it, so you register one Kubernetes
cluster *before* you ask anything. When you finish, you will have a Joe running locally,
in read-only observation mode, reading a live cluster and answering a question about it
through its real interaction surface.

> This is the on-rails path. For the full obtain-and-run procedure, the complete
> authentication options, and production setup, see [Install and Build](../install-and-build/).
> For *why* Joe works this way, see [Concepts](../concepts/).

## Before you start

You need a shell, a SHA-256 utility (your system already has one), and two credentials
ready:

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

## Step 1 — Download and verify the binary

Open the repository's [GitHub Releases](https://github.com/jaimegago/joe/releases) page
and download two files from the latest release: the archive matching your operating
system and CPU architecture, and the `checksums.txt` published beside it. The release's
own asset list is the authority on which platforms that release shipped.

Now verify the archive before you run it. You are about to execute this binary against a
production cluster credential, and a checksum is the only thing a release publishes that
proves the bytes you got are the bytes that were built — nothing here is signed. From the
directory holding both files, on Linux:

```sh
sha256sum --ignore-missing --check checksums.txt
```

On macOS:

```sh
shasum --algorithm 256 --ignore-missing --check checksums.txt
```

`--ignore-missing` lets you check the one archive you downloaded against a file that
lists every published asset. You should see `OK`. If anything prints `FAILED`, stop —
do not extract it.

Then extract:

```sh
tar -xzf <the archive you downloaded>
./joe --help
```

You now have a runnable `joe` in the current directory. The rest of this tutorial runs it
from here.

> **Building from source instead?** That is a first-class peer path, not a fallback —
> reach for it if you are contributing to Joe, running an untagged commit from `main`, or
> on a platform outside the release matrix. It needs a Go toolchain, Node.js, and git.
> [Install and Build](../install-and-build/) has the procedure; rejoin this tutorial at
> Step 2 with a `./joe` in hand.

## Step 2 — Write the config file

Joe refuses to boot without an identity configured. Identity means **service accounts**:
named machine identities, each with a bearer key, each authenticating as the principal
`svc:<name>`. You will define two, because you need two different things from them.

Create `~/.joe/config.yaml` (Joe's default config path — it reads this file on startup
without being told to):

```yaml
server:
  service_accounts:
    - name: server
      key: "pick-a-long-random-string"
    - name: joe-admin
      key: "pick-a-different-long-random-string"
```

`server` is the general-purpose account. `joe-admin` is a **dedicated administration
account**: in Step 4 you grant admin to that one and nothing else, so admin does not ride
on the key every ordinary caller holds. Joe's own tooling steers you to a dedicated
account for exactly this reason — see
[Operations](../operations/) for the rationale in full.

Everything else in this tutorial runs on Joe's built-in defaults, so those few lines are
the whole file.

> **Write this before you start the daemon.** Joe reads its service accounts once, at
> boot. Adding an account to a running Joe does nothing until you restart it.

Then set two environment variables — Joe's default model needs a provider key, and the
write floor keeps the install read-only:

```sh
# LLM provider: Joe's default model is Claude, so it needs an Anthropic key.
export ANTHROPIC_API_KEY="your-anthropic-api-key"

# Boot read-only: raise the write floor so Joe cannot mutate anything.
export JOE_MODE=observation
```

## Step 3 — Start Joe

```sh
./joe
```

Joe starts on `localhost:7777` and loads the config file you just wrote. That file is
separate from the **bearer-token variable** for your cluster from *Before you start*: Joe
does not read that at boot, and you do not put it in a Joe config file. You name that
variable when you promote the cluster in Step 6, and Joe resolves it from its own
environment at connect time. In the startup logs you will see that the **write floor is up
(observation)**: Joe is read-only. Leave it running and open a second terminal for the
next step.

## Step 4 — Grant the first admin

Registering and promoting a component are admin actions, and this install has **no
admin**. It also has no identity provider, so there is no login through which Joe could
bootstrap one. The way in is an offline command that grants admin to a configured service
account, on a database that has no admin yet:

```sh
./joe admin bootstrap svc:joe-admin
```

It contacts no daemon — it writes to Joe's database directly — and the Joe you left
running picks the grant up without a restart. It is a **first-admin** command only: with
an admin now present it refuses to run again, and every later grant goes through Joe's
admin API. See [Operations](../operations/) for that surface.

## Step 5 — Log in to the web UI

Open <http://localhost:7777> in a browser. With no identity provider configured, the login
page asks for a **service-account key**. Paste the `joe-admin` key from Step 2 and sign
in.

You land on **Chat**, and because this principal now holds admin you also see the **Admin**
group in the sidebar.

## Step 6 — Register one Kubernetes cluster

Joe is near-useless until it has a registered component to reason about, so connect one
before you ask anything. Do it through the web UI. Bringing a system under management is
always the same three beats: **register** it (it lands inert — recorded, but
credential-less and unable to act), **promote** it with a credential **reference** (never
a stored secret), then run a **connectivity test** that takes it live. For Kubernetes,
running outside the cluster, that reference is the cluster's **API-server URL** and **CA
bundle** plus the **bearer-token environment variable** you readied in *Before you start* —
the variable must be set where the daemon runs, since Joe resolves it there — and the test
brings the cluster live with no restart.

The full click-by-click procedure — registering, assigning a zone, promoting with a
static-bearer reference, and testing — lives in the guide:

> **[Register a Kubernetes component](../guides/register-kubernetes/)** — do the steps
> there in the session you just signed into, then come back.

When the component's connectivity test reports success, the cluster is live and Joe can
read it.

## Step 7 — Ask Joe about the cluster

Now that Joe has something real to read, open **Chat** in the web UI and ask:

> What Kubernetes workloads do you know about, and is anything unhealthy?

Joe runs a full agentic turn and answers from the live cluster — still read-only, because
the write floor is up. You have gone from an empty install to a governed agent reading real
infrastructure, exercising the whole path end to end: identity, the LLM, the agent loop,
and a live component read.

## What you just did

- Obtained `joe` as a published release binary and verified it against the release
  checksums before running it.
- Gave Joe the identity it requires — two service accounts in its config file, one of them
  a dedicated administration account — so it agreed to boot.
- Ran it in observation mode, with the write floor up, so nothing it did could change a
  managed system.
- Granted the install its first admin offline, the only way in for a deployment with no
  identity provider.
- Registered and promoted one Kubernetes cluster through the UI and took it live, so Joe
  had real infrastructure to read.
- Drove its real interaction surface — the web UI's chat — and got an answer back about
  that live cluster.

## Where to go next

- The full build, run, and authentication procedure (including OIDC login for humans)
  → [Install and Build](../install-and-build/)
- Granting further admins, and the admin surfaces generally → [Operations](../operations/)
- Every configuration key and environment variable → [Configuration](../configuration/)
- Register the other system types Joe supports → [Components](../components/)
- The full Kubernetes register-and-promote walkthrough → [Register a Kubernetes component](../guides/register-kubernetes/)
- Understand observation mode, principals, and governance → [Concepts](../concepts/)
