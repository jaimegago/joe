---
title: "Joe and MCP"
weight: 25
---

Joe speaks the Model Context Protocol in one direction only. Joe can act as an MCP server, exposing its own governed tools to your coding agent. Joe does not act as an MCP client: it will not connect out to other MCP servers and run their tools. That asymmetry is deliberate, and this page explains it.

## The guarantee Joe protects

Joe's core promise is that when it runs in observation mode, it will not change anything in your infrastructure — and that this is checkable, not a matter of trust. Every tool Joe can call is classified in Joe's own source code as either a read or a change, and in observation mode the change class is refused before it can run. The guarantee holds because Joe wrote and classified every tool it can call.

## Why consuming MCP would break it

An MCP server exposes tools that Joe did not write and cannot inspect. The protocol has no reliable way to declare whether a given tool merely reads data or actually changes something: the available signals are optional, and by the protocol's own rules they must not be trusted from a server you do not control — a tool that changes things can simply claim to be read-only. If Joe consumed those tools, it would be running code whose effects it cannot verify. To make an external tool usable, Joe would have to take someone's word that it only reads, turning a checkable guarantee into a promise. Joe's whole point is that it is not a promise. So Joe declines the entire direction rather than quietly weaken the guarantee.

## What to do instead

To bring a system under Joe safely, Joe uses a native adapter: Joe's own code that talks to that system's API, classified as a read or a change in Joe's source like every other tool. A native adapter reaches the same data an external MCP server would, and it does so while keeping the observation-mode guarantee intact. If you want Joe to read your Confluence, the answer is a native Confluence adapter, not an external MCP connection.

## The other direction is fully supported

Pointing your coding agent at Joe over MCP is safe and supported. In that direction Joe is [the server](../guides/mcp/): it exposes tools it authored and governs, and your agent inherits Joe's safety gate. The concern only arises when untrusted tools would flow the other way, into Joe's own decision loop.

## A considered stance, not a permanent verdict

This is a judgment about MCP as it stands today, not a rejection of the protocol. If MCP gains an enforceable way for a server to declare a tool's effect — one a client can actually rely on — the objection goes away, and Joe can revisit consuming external tools.
