---
title: The Slack bot
weight: 60
description: Bring up joe slack as a Socket Mode daemon client.
---

# The Slack bot

`joe slack` connects Joe to Slack so your team can talk to it from a channel. It runs
over Slack **Socket Mode**, so it needs no public URL or inbound webhook. Like
[`joe mcp`](mcp/), it is a **daemon client, not a second server**: it holds a Socket Mode
connection to Slack and forwards to a running `joe` server over HTTP. It does not bind a
port of its own.

## Tokens and environment

`joe slack` requires two Slack tokens and reads the daemon connection from the same
variables the other clients use:

- `SLACK_BOT_TOKEN` — the Bot User OAuth token (`xoxb-…`). **Required.**
- `SLACK_APP_TOKEN` — an app-level token with the `connections:write` scope (`xapp-…`),
  which is what enables Socket Mode. **Required.**
- `JOE_SERVER` — the daemon base URL. Defaults to `http://localhost:7777`.
- `JOE_API_KEY` — the bearer token used to authenticate to the daemon. Optional, but
  required in practice whenever the daemon enforces auth.

If either Slack token is missing, `joe slack` refuses to start and tells you which one.

## Bring it up

Create a Slack app, enable Socket Mode, install it to your workspace to mint the bot
token, and generate an app-level token with `connections:write`. Then:

```sh
export SLACK_BOT_TOKEN="xoxb-..."
export SLACK_APP_TOKEN="xapp-..."
export JOE_SERVER="http://localhost:7777"
export JOE_API_KEY="<service-account-key>"

joe slack
```

The process stays in the foreground holding the Socket Mode connection; stop it with
`SIGINT`/`SIGTERM`. Run it alongside an already-running `joe` daemon — it depends on that
daemon being reachable at `JOE_SERVER`.

## Where to go next

- The other daemon front-end → [The MCP server](mcp/)
- Running the daemon this client connects to → [Operations](../operations/)
