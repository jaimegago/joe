# Joe Project Milestone: Phase 3 Complete

Use this document to bootstrap a new Claude conversation about Joe.

## What is Joe?

Joe (Joe Operates Everything) is an AI-powered infrastructure copilot built in Go. It helps platform engineers debug distributed systems through natural conversation.

## Architecture Summary

Two-binary architecture from day one:

- joe (Joe Local): CLI with User Agent, REPL, local tools. Connects to joecored via HTTP.
- joecored (Joe Core): Daemon with HTTP API, Core Agent, Core Services (Graph Store, SQL Store, Adapters).

Both agents use LLMs. User Agent for conversation, Core Agent for discovery/interpretation.

## Current State

Phase 1 (Foundation): COMPLETE
- Two binaries: cmd/joe/, cmd/joecored/
- HTTP API skeleton with /api/v1/status
- HTTP client connecting joe to joecored
- Config loading
- LLM Adapter + Claude + Gemini implementations

Phase 2 (User Agent Loop): COMPLETE
- Tool interface, executor, registry
- User Agent with agentic loop
- REPL with /model command for hot-swapping LLMs
- Local tools: echo, ask_user, read_file, write_file, local_git_status, local_git_diff, run_command

Phase 3 (Core Services + API): COMPLETE
- SQL Store: COMPLETE (sources, sessions, clarifications, cache, facts tables with repositories)
- Graph Store: COMPLETE (SQLite-backed, graph_nodes + graph_edges tables, recursive CTEs for traversal)
- Core Services: COMPLETE (wired with Graph + SQL stores in joecored)
- API handlers: COMPLETE (graph/query, graph/related/{nodeID}, graph/summary — 83% test coverage)
- HTTP client methods: COMPLETE (GraphQuery, GraphRelated, GraphSummary)
- Core tools: COMPLETE (graph_query, graph_related — wired via NewDefaultRegistryWithClient)
- Full data path: User Agent → core tool → HTTP client → joecored API → graph store → response

## Key Architecture Documents

All in the joe repo or available as outputs:

- joe-architecture.md: Full architecture with diagrams, config schema, phases
- CLAUDE.md: Context file for Claude Code with directory structure, interfaces, patterns
- joe-dataflow.md: Data flow diagrams showing joe ↔ joecored communication
- joe-prompt.md: Prompt for generating .joe/ context files

## Directory Structure

joe/
├── cmd/
│   ├── joe/           # Joe Local CLI
│   └── joecored/      # Joe Core daemon
├── internal/
│   ├── api/           # HTTP API handlers (joecored)
│   ├── client/        # HTTP client (joe → joecored)
│   ├── config/        # Configuration
│   ├── core/          # Core Services struct
│   ├── coreagent/     # Core Agent (discovery, refresh)
│   ├── useragent/     # User Agent (agentic loop)
│   ├── llm/           # LLM adapters (claude/, gemini/, ollama/)
│   ├── tools/
│   │   ├── local/     # Local tools (readfile, writefile, gitstatus, gitdiff, runcmd)
│   │   └── core/      # Core tools (graph_query, graph_related — call joecored API)
│   ├── graph/         # Graph store (SQLite-backed, implemented)
│   ├── store/         # SQL store (SQLite, implemented)
│   ├── repl/          # REPL with /model command
│   └── adapters/      # Infrastructure adapters (K8s, Git, ArgoCD - interfaces only)

## Config Structure

llm:
  current: claude-sonnet
  available:
    claude-sonnet:
      provider: claude
      model: claude-sonnet-4-20250514
      api_key_env: ANTHROPIC_API_KEY
    # ... more models

server:
  address: ":7777"

## What's Next

Phase 4 (Infrastructure):

1. K8s adapter (joecored) — connect to clusters, read resources, get logs
2. K8s API endpoints — expose via HTTP
3. K8s core tools (joe) — k8s_get, k8s_logs for User Agent
4. Git adapter (joecored) — clone/read repos
5. Git API endpoints + core tools

## Prompt Style for Claude Code

When creating prompts for Claude Code:
- No markdown formatting (no code fences, no backticks)
- No detailed code implementations
- Only: requirements, interface descriptions, behaviors, acceptance criteria
- Let Claude Code write the actual code

## Key Interfaces

LLMAdapter: Chat(ctx, ChatRequest) → ChatResponse, ChatStream, Embed
Tool: Name(), Description(), Parameters(), Execute(ctx, args) → (any, error)
CoreClient: GraphQuery, GraphRelated, K8sGet, K8sLogs, Clarifications (HTTP client)
GraphStore: AddNode, AddEdge, GetNode, Query, Related, Path, DeleteNode, DeleteEdge, Summary
SourceRepository, SessionRepository, ClarificationRepository, CacheRepository, FactRepository
