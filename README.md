# Joe - AI-Powered Infrastructure Copilot

Joe (Joe Operates Everything) helps platform engineers understand, debug, and operate their infrastructure through natural conversation.

## Status

📈 **Phase 4 Complete** - Core foundation built, ready for Core Agent implementation

Architecture & Foundation:
- ✅ Two-binary architecture (`joe` client + `joecored` daemon)
- ✅ HTTP API with full client-server separation
- ✅ LLM adapter interface (AI-agnostic design)
- ✅ Claude 4 Sonnet + Gemini 2.5 adapters with tool support
- ✅ Hot model switching without restart
- ✅ OpenTelemetry instrumentation (tokens, latency, costs)

User Agent & Tools:
- ✅ Interactive REPL with agentic conversation loop
- ✅ Tool execution framework (registry, executor, safety)
- ✅ Local tools (file I/O, git operations, command execution)
- ✅ Core tools (graph queries, K8s resources, git repos via API)
- ✅ Session management with conversation history

Data Layer & Infrastructure:
- ✅ SQL Store (SQLite) - sources, sessions, cache, facts
- ✅ Graph Store (SQLite-based) - nodes, edges, relationships
- ✅ Migration system with schema versioning
- ✅ Kubernetes adapter (client-go, dynamic discovery)
- ✅ Git adapter (go-git, clone/read/log/diff operations)

Testing & Observability:
- ✅ Unit tests, integration tests, E2E test harness
- ✅ OpenTelemetry metrics and tracing
- ✅ Structured logging with configurable levels

**Next Phase 5:** Core Agent implementation (background discovery, autonomous graph maintenance)

## Quick Start

### Prerequisites

- Go 1.25.0 or later
- API key for your chosen LLM provider:
  - Anthropic API key (for Claude 4 Sonnet)
  - Google API key (for Gemini 2.5 Flash/Pro)

### Installation

```bash
git clone https://github.com/jaimegago/joe.git
cd joe
make build
```

This builds two binaries:
- `joe` - Interactive CLI client
- `joecored` - Background daemon that handles LLM interactions

### Configuration

Create `~/.joe/config.yaml`:

```yaml
# Joe Configuration Example
# Copy to ~/.joe/config.yaml and customize

llm:
  # Currently active model key (must match a key in 'available')
  current: claude-sonnet

  # All configured models
  available:
    claude-sonnet:
      provider: claude
      model: claude-sonnet-4-20250514
    gemini-flash:
      provider: gemini
      model: gemini-2.5-flash
    gemini-pro:
      provider: gemini  
      model: gemini-2.5-pro

  # Note: API keys are NEVER stored in config files
  # Set via environment variables:
  #   - Claude: ANTHROPIC_API_KEY
  #   - Gemini: GEMINI_API_KEY or GOOGLE_API_KEY

server:
  address: "localhost:7777"

refresh:
  # Background refresh interval in minutes
  interval_minutes: 5

  # LLM usage limits during background refresh
  llm_budget:
    max_calls_per_hour: 100
    batch_threshold: 10
    batch_timeout_sec: 30

logging:
  level: info                   # debug | info | warn | error
```

Or use the example config:
```bash
cp config.example.yaml ~/.joe/config.yaml
```

### Environment Variables

Override config with environment variables:

```bash
# LLM Provider & Model Key (from config.yaml)
export JOE_LLM_CURRENT=claude-sonnet

# API Keys (required - not stored in config)
export ANTHROPIC_API_KEY="your-anthropic-key"  # For Claude 4 Sonnet
export GEMINI_API_KEY="your-google-key"        # For Gemini 2.5 models
export GOOGLE_API_KEY="your-google-key"        # For Gemini (fallback)

# Server & Logging
export JOE_SERVER_ADDRESS=localhost:7777
export JOE_LOG_LEVEL=debug

# Background refresh (optional)
export JOE_REFRESH_INTERVAL_MINUTES=5
export JOE_REFRESH_MAX_CALLS_PER_HOUR=100
```

### Run

Start the daemon, then the client:

```bash
# Terminal 1: Start the daemon
make run-joecored
# or: ./joecored

# Terminal 2: Start the interactive client
make run-joe
# or: ./joe
```

Or use convenience target to build and run:
```bash
make run-joe
```

## Features

### Interactive REPL

Joe provides an interactive command-line interface:

```
> who are you?
I am Joe, an infrastructure assistant.

> read the README.md file
[Joe reads and displays the file]

> what's the git status?
[Joe runs git status and shows results]
```

### REPL Commands

- `/model` - Interactively switch between LLM models without restart
- `/help` - Show available commands
- `/exit` - Exit Joe

### Available Tools

Joe provides two categories of tools:

**Local Tools** (User Agent executes locally):
- **read_file** - Read contents of local files  
- **write_file** - Write content to local files
- **local_git_status** - Check git repository status  
- **local_git_diff** - Show git diff
- **run_command** - Execute safe shell commands (ls, pwd, date, etc.)
- **echo** - Echo back text (for testing)
- **ask_user** - Prompt user for additional input

**Core Tools** (User Agent calls joecored API):
- **graph_query** - Query infrastructure knowledge graph
- **graph_related** - Find connected infrastructure nodes
- **graph_summary** - Get contextual graph information
- **list_sources** - Show registered infrastructure sources
- **k8s_get** - Get Kubernetes resources (pods, deployments, etc.)
- **k8s_logs** - Fetch pod logs from connected clusters  
- **git_read** - Read files from registered git repositories
- **git_log** - Get commit history from repositories
- **git_diff** - Show diffs between commits

### Model Hot-Swapping

Switch between configured LLM models on the fly:

```
> /model
Use arrow keys to navigate:
▸ claude-sonnet (current)
  gemini-flash
  gemini-pro
```

Configure available models in `~/.joe/config.yaml` under `llm.available`.

## Architecture

Joe uses a client-server architecture:

- **joe (client)** - Interactive REPL that connects to the daemon
- **joecored (daemon)** - Background service that handles LLM interactions and tool execution

**Architecture Highlights:**
- **Two-binary design** - `joe` (client) + `joecored` (daemon) with HTTP API boundary
- **Dual agents** - User Agent (interactive) + Core Agent (autonomous, Phase 5)
- **AI-agnostic** - Swappable LLM backends (Claude 4, Gemini 2.5)
- **Tool-based execution** - LLM calls tools to perform actions
- **Hot-swappable models** - Change models without restarting
- **Knowledge graph** - SQLite-based graph for infrastructure relationships
- **Full observability** - OpenTelemetry tracing, metrics, structured logging

See [docs/joe-architecture.md](docs/joe-architecture.md) for complete architecture details.

## Testing

Joe has comprehensive testing at multiple levels:

```bash
# Build both binaries
make build

# Run unit tests (fast, no external dependencies) 
make test-unit

# Run integration tests with mocks (no external services)
make test-integration  

# Run end-to-end tests (requires built binaries)
make test-e2e

# Run all test types sequentially
make test-all

# Run tests with coverage
make test-coverage

# Verify code quality
go vet ./...
```

Test categories:
- **Unit tests** - Individual component testing with mocks
- **Integration tests** - API contracts, conversation flows with mock LLM
- **E2E tests** - Full binary lifecycle with automated harness

## Development Phases

Joe is built in iterative phases:

**✅ Phase 1-4: Complete** - Core foundation built  
**🚧 Phase 5: Current** - Core Agent implementation  
**📋 Phase 6: Planned** - Cloud adapters (AWS, Azure)  
**📋 Phase 7: Planned** - Knowledge store with embedding search  
**📋 Phase 8: Planned** - Documentation co-pilot  
**📋 Phase 9: Planned** - Additional clients (Web UI, VS Code)  

See [docs/phase-4-finished-copilot-analysis-and-next-steps.md](docs/phase-4-finished-copilot-analysis-and-next-steps.md) for detailed status.

## Project Structure

```text
joe/
├── cmd/
│   ├── joe/                  # CLI client entry point
│   └── joecored/             # Daemon entry point  
├── internal/
│   ├── adapters/             # Infrastructure adapter registry
│   │   ├── k8s/              # Kubernetes adapter (client-go)
│   │   └── git/              # Git adapter (go-git)
│   ├── api/                  # HTTP API server (joecored)
│   ├── client/               # HTTP client (joe→joecored)
│   ├── config/               # Configuration loading & validation
│   ├── core/                 # Core services container
│   ├── coreagent/            # Core agent logic (Phase 5)
│   ├── llm/                  # LLM interface and implementations
│   │   ├── claude/           # Claude 4 Sonnet adapter
│   │   └── gemini/           # Gemini 2.5 adapters
│   ├── llmfactory/           # LLM adapter factory with hot-swap
│   ├── repl/                 # Interactive REPL and model selector
│   ├── tools/                # Tool framework
│   │   ├── core/             # Core tools (API-based)
│   │   └── local/            # Local tools (filesystem, git, commands)
│   ├── useragent/            # User agent orchestration
│   ├── session/              # Session management
│   ├── store/                # SQL storage layer (SQLite)
│   ├── graph/                # Graph store (SQLite-based)
│   ├── observability/        # OpenTelemetry logging and telemetry
│   ├── logging/              # Structured logging setup
│   └── notify/               # Notification service (Phase 6)
├── docs/                     # Architecture and design documentation  
├── test/                     # Testing infrastructure
│   ├── e2e/                  # End-to-end tests
│   ├── integration/          # Integration tests with mocks
│   └── fixtures/             # Test data and configurations
├── Makefile                  # Build, test, and run targets
└── config.example.yaml       # Example configuration file
```

## Documentation

- [CLAUDE.md](CLAUDE.md) - Project context for AI assistants
- [CONFIG.md](CONFIG.md) - Configuration guide
- [docs/joe-architecture.md](docs/joe-architecture.md) - Full architecture
- [docs/joe-dataflow.md](docs/joe-dataflow.md) - Data flow details
- [docs/joe-prompt.md](docs/joe-prompt.md) - System prompts and behavior
- [docs/go-standards.md](docs/go-standards.md) - Go coding standards
- [docs/observability.md](docs/observability.md) - Logging and telemetry
- [docs/instrumentation.md](docs/instrumentation.md) - LLM instrumentation

## Contributing

This project is in active development. See [docs/go-standards.md](docs/go-standards.md) for coding conventions.

## License

TBD
