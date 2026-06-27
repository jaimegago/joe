---
title: Quickstart
weight: 20
description: From nothing to a running Joe answering one question, in observation mode.
---

# Quickstart

This tutorial takes you from an empty checkout to a running `joe` daemon that answers
one question — in **observation mode**, where Joe can read and reason but cannot change
anything. You will do nothing irreversible. Follow the steps in order; each one builds
on the last.

When you finish, you will have a Joe running locally, in read-only observation mode,
that has answered a question through its real interaction surface.

> This is the on-rails path. For the full build-and-run procedure, the complete
> authentication options, and production setup, see [Install and Build](../install-and-build/).
> For *why* Joe works this way, see [Concepts](../concepts/).

## Before you start

You need three things installed:

- **Go 1.25 or newer**
- **Node.js and npm** (the web UI is built and embedded into the binary)
- **git**

You also need an **Anthropic API key**, because Joe's default model is Claude. Have it
ready as a string.

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

Joe starts on `localhost:7777`. You do not need a config file — Joe boots on built-in
defaults. In the startup logs you will see that the **write floor is up
(observation)**: Joe is read-only. Leave it running and open a second terminal for the
next step.

## Step 4 — Ask Joe one question

Send a message to Joe's agentic task endpoint, authenticating with the same
`JOE_API_KEY` you set above:

```sh
curl -s http://localhost:7777/api/v1/tasks \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"message": "What can you help me with, and what infrastructure do you currently know about?"}'
```

Joe runs a full agentic turn and returns a JSON response. The answer is in the
`final_answer` field. Because you have not connected any systems yet, Joe will tell you
its graph is empty — that is the expected, correct answer for a fresh install, and it
confirms the whole path works end to end: identity, the LLM, and the agent loop.

That is your one answer. Joe is up, governed, read-only, and responding.

## What you just did

- Built `joe` from source — the only supported way to get the binary.
- Gave Joe the minimal identity it requires (one service account via `JOE_API_KEY`),
  so it agreed to boot.
- Ran it in observation mode, with the write floor up, so nothing it did could change a
  managed system.
- Drove its real interaction surface — the agentic task endpoint — and got an answer
  back.

## Where to go next

- The full build, run, and authentication procedure (including OIDC login for humans
  and the admin bootstrap) → [Install and Build](../install-and-build/)
- Every configuration key and environment variable → [Configuration](../configuration/)
- Connect Joe to real systems so it has something to map → [Integrations](../integrations/)
- Understand observation mode, principals, and governance → [Concepts](../concepts/)
