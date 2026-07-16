---
title: "Joe and MCP"
weight: 25
---

Joe speaks the Model Context Protocol in one direction only. Joe can act as an MCP server, exposing its own governed tools to your coding agent. Joe does not act as an MCP client: it will not connect out to other MCP servers and run their tools. That asymmetry is deliberate, and this page explains it.

## The guarantee Joe protects

Joe makes checkable promises about what it can and cannot do — promises you can verify rather than take on faith. Every tool Joe can call is classified in Joe's source code as either a read or a change, and Joe enforces that classification before a tool runs. This shows up in two places. In observation mode, the change class is refused outright, so Joe will not alter your infrastructure at all. And when you divide your infrastructure into zones and grant Joe read-only access to a zone, that boundary means what it says: only tools classified as reads can run there. Both guarantees rest on the same foundation — Joe knows, from its own source code, whether each tool reads or changes something. The tools are part of Joe's source, fully under the control of Joe's authors, and each is classified as it is written; that classification is then exercised by OASIS, Joe's open safety-evaluation framework. Because the classification is grounded in code Joe's authors control and test, the read-or-change boundary is something an operator can rely on rather than something they have to take on trust.

And Joe is open source, which adds a further layer of verification: you do not have to take any of this on our word. The classification of every tool, the write floor that enforces it, and the zone boundaries all live in code you can read. Go check it yourself.

## Why consuming MCP would break it

An MCP server exposes tools that Joe's authors did not write and Joe cannot inspect. The protocol has no reliable way to declare whether a given tool merely reads data or actually changes something: the available signals are optional, and by the protocol's own rules they must not be trusted from a server you do not control — a tool that changes things can simply claim to be read-only. If Joe consumed those tools, it would be running code whose effects it cannot verify, and every guarantee built on knowing a tool's effect would inherit that blind spot. Observation mode could no longer promise it makes no changes, and a read-only zone could no longer promise it only reads — because an external tool an operator believed was read-only might change things anyway. To make an external tool usable, Joe would have to take someone's word about what it does, turning a checkable guarantee into a promise. Joe's whole point is that it is not a promise. So Joe declines the entire direction rather than quietly weaken the guarantee.

## What to do instead

To bring a system under Joe safely, Joe uses a native adapter: Joe's own code that talks to that system's API, classified as a read or a change in Joe's source like every other tool. A native adapter reaches the same data an external MCP server would, and it does so while keeping the observation-mode guarantee intact. If you want Joe to read your Confluence, the answer is a native Confluence adapter, not an external MCP connection.

## The other direction is fully supported

Pointing your coding agent at Joe over MCP is safe and supported. In that direction Joe is [the server](../../guides/mcp/): every tool it exposes over MCP is a read by construction — each is wired straight to a query, and no change-class tool exists on that surface — so an agent connected to Joe cannot alter your infrastructure through it, and its reads stay subject to Joe's access control and audit like any other read. The concern only arises when untrusted tools would flow the other way, into Joe's own decision loop.

## A considered stance, not a permanent verdict

This is a judgment about MCP as it stands today, not a rejection of the protocol. If MCP gains an enforceable way for a server to declare a tool's effect — one a client can actually rely on — the objection goes away, and Joe can revisit consuming external tools.
