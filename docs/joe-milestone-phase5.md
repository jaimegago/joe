# Joe Project Milestone: Phase 5 In Progress

Use this document to bootstrap a new Claude conversation about Joe.

## What is Joe?

Joe (Joe Operates Everything) is an AI-powered infrastructure copilot built in Go. It helps platform engineers debug distributed systems through natural conversation.

## Architecture Summary

Two-binary architecture:

- joe (Joe Local): CLI with User Agent, REPL, local tools. Connects to joecored via HTTP.
- joecored (Joe Core): Daemon with HTTP API, Core Agent, Core Services (Graph Store, SQL Store, Adapters).

Both agents use LLMs. User Agent for conversation, Core Agent for discovery/interpretation.

## Current State (from code analysis)

Phase 1 (Foundation): ✅ COMPLETE
- Two binaries working
- HTTP API server + client
- Config with env var overrides
- LLM adapters: Claude + Gemini fully implemented

Phase 2 (User Agent Loop): ✅ COMPLETE
- Tool interface, executor, registry
- User Agent with agentic loop
- REPL with /model command (bubbletea TUI)
- Local tools: echo, ask_user, read_file, write_file, local_git_status, local_git_diff, run_command

Phase 3 (Core Services + API): ✅ COMPLETE
- SQL Store: 8 tables (sources, sessions, session_messages, clarifications, joe_file_cache, onboarding_facts, graph_nodes, graph_edges)
- Graph Store: SQLite-based (not Cayley), fully functional
- API handlers: graph (query, related, summary), sources CRUD, k8s, git
- Core tools: graph_query, graph_related, list_sources, k8s_get, k8s_logs, git_read, git_log, git_diff

Phase 4 (Infrastructure Adapters): ✅ COMPLETE
- K8s adapter: Connect, ListResources, GetResource, GetPodLogs
- Git adapter: Connect, ReadFile, ListFiles, Log, Diff
- Full API endpoints and core tools for both

Phase 5 (Core Agent): CURRENT - PARTIAL
- Clarifications table exists, API endpoints return 501
- Background refresh: NOT IMPLEMENTED
- Auto-discovery: NOT IMPLEMENTED
- Onboarding flow: NOT IMPLEMENTED

## Upcoming Phases

Phase 6: Cloud Adapters
- AWS adapter (EC2, EKS, RDS, ALB, VPC, CloudWatch)
- Azure adapter (VMs, AKS, Azure SQL, VNets, Monitor)
- Graph integration: cloud nodes link to K8s nodes via is_k8s_node edge

Phase 7: Knowledge Store
- Three-tier system (curated, synced, derived)
- Tier 1: Human-curated notes (immutable by LLM)
- Tier 2: Synced sources (Confluence, wiki - external is source of truth)
- Tier 3: LLM-derived insights (autonomous, with provenance)
- Embeddings for semantic search

Phase 8: Documentation Co-Pilot
- Write adapters for wikis
- Joe proposes doc updates, human approves publish

Phase 9: Additional Clients + Polish
- Web UI, VS Code extension
- RBAC / permissions layer
- Notifications (Slack, desktop)

## Key Architecture Decisions

1. Graph Store uses SQLite (not Cayley) - simpler, works well
2. Flat graph, no hierarchy - LLM decides relevance at query time
3. Cloud resources are first-class nodes, linked to K8s via is_k8s_node edge
4. Knowledge Store has three trust tiers with different LLM autonomy levels

## Directory Structure

joe/
├── cmd/joe/, cmd/joecored/
├── internal/
│   ├── adapters/k8s/, adapters/git/  # ✅ Implemented
│   ├── api/                           # ✅ Most endpoints working
│   ├── client/                        # ✅ Full HTTP client
│   ├── store/                         # ✅ 8 tables, all repos
│   ├── graph/                         # ✅ SQLite-based
│   ├── llm/claude/, llm/gemini/       # ✅ Both working
│   ├── tools/local/, tools/core/      # ✅ All tools working
│   ├── repl/                          # ✅ /model command
│   └── useragent/                     # ✅ Agentic loop

## What's Next

Options for next work:
1. Complete Phase 5 (Core Agent) - background refresh, auto-discovery, clarifications
2. Start Phase 6 (Cloud Adapters) - AWS and Azure integration
3. Both in parallel

## Prompt Style for Claude Code

When creating prompts for Claude Code:
- No markdown formatting (no code fences, no backticks)
- No detailed code implementations
- Only: requirements, interface descriptions, behaviors, acceptance criteria
- Let Claude Code write the actual code
