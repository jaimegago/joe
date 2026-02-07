# Joe - AI-Powered Infrastructure Copilot

Joe (Joe Operates Everything) helps platform engineers understand, debug, and operate their infrastructure through natural conversation.

## Status

🚧 **Active Development** - Core features implemented

Currently implemented:

- ✅ Project scaffolding and architecture
- ✅ LLM adapter interface (AI-agnostic design)
- ✅ Claude adapter with tool support
- ✅ Gemini adapter with tool support
- ✅ Tool execution framework
- ✅ REPL / interactive mode with hot model switching
- ✅ Local tools (file read/write, git status/diff, command execution)
- ✅ Client-server architecture (joe + joecored daemon)
- ✅ Configuration system with environment variable overrides
- ⏳ SQL store (SQLite)
- ⏳ Graph store (Cayley)
- ⏳ Full agentic loop with knowledge retention

## Quick Start

### Prerequisites

- Go 1.25 or later
- API key for your chosen LLM provider:
  - Anthropic API key (for Claude)
  - Google API key (for Gemini)

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
# LLM Configuration
llm:
  provider: gemini              # claude | gemini
  current_model: gemini-2.5-flash
  available_models:
    - gemini-2.5-flash
    - gemini-2.5-pro
    - gemini-2.0-flash
    - claude-3-5-sonnet-20241022
    - claude-3-7-sonnet-20250219

# Server Configuration
server:
  address: localhost:7777

# Logging
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
# LLM Provider & Model
export JOE_LLM_PROVIDER=gemini
export JOE_LLM_MODEL=gemini-2.5-flash

# API Keys
export ANTHROPIC_API_KEY="your-anthropic-key"  # For Claude
export GEMINI_API_KEY="your-google-key"        # For Gemini (primary)
export GOOGLE_API_KEY="your-google-key"        # For Gemini (fallback)

# Server & Logging
export JOE_SERVER_ADDRESS=localhost:7777
export JOE_LOG_LEVEL=debug
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

### Local Tools

Joe can execute local operations:

- **read_file** - Read contents of local files
- **write_file** - Write content to local files
- **local_git_status** - Check git repository status
- **local_git_diff** - Show git diff
- **run_command** - Execute safe shell commands (ls, pwd, date, etc.)
- **echo** - Echo back text (for testing)
- **ask_user** - Prompt user for additional input

### Model Hot-Swapping

Switch between LLM models on the fly:

```
> /model
Use arrow keys to navigate:
  gemini-2.5-flash (current)
  gemini-2.5-pro
  gemini-2.0-flash
  claude-3-5-sonnet-20241022
  claude-3-7-sonnet-20250219
```

## Architecture

Joe uses a client-server architecture:

- **joe (client)** - Interactive REPL that connects to the daemon
- **joecored (daemon)** - Background service that handles LLM interactions and tool execution

Key design principles:
- **AI-agnostic** - Swappable LLM backends (Claude, Gemini)
- **Tool-based** - LLM calls tools to perform actions
- **Hot-swappable** - Change models without restarting
- **Agentic** - LLM reasons → calls tools → processes results → continues

See [docs/joe-architecture.md](docs/joe-architecture.md) for full details.

## Development

Build and test:

```bash
# Build both binaries
make build

# Run tests
make test
# or: go test ./...

# Run specific package tests
go test ./internal/llm/gemini -v

# Run with coverage
go test -cover ./...

# Verify code
go vet ./...
```

## Project Structure

```text
joe/
├── cmd/
│   ├── joe/                  # CLI client entry point
│   └── joecored/             # Daemon entry point
├── internal/
│   ├── adapters/             # LLM adapter management
│   ├── api/                  # HTTP API server
│   ├── client/               # HTTP client for joe→joecored
│   ├── config/               # Configuration loading
│   ├── core/                 # Core services
│   ├── coreagent/            # Core agent logic
│   ├── llm/                  # LLM interface and implementations
│   │   ├── claude/           # Anthropic Claude adapter
│   │   └── gemini/           # Google Gemini adapter
│   ├── llmfactory/           # LLM adapter factory
│   ├── repl/                 # Interactive REPL and model selector
│   ├── tools/                # Tool framework
│   │   └── local/            # Local tools (file, git, command)
│   ├── useragent/            # User agent orchestration
│   ├── session/              # Session management
│   ├── store/                # Storage layer (planned)
│   ├── graph/                # Graph store (planned)
│   └── observability/        # Logging and telemetry
├── docs/                     # Architecture documentation
├── Makefile                  # Build targets
└── config.example.yaml       # Example configuration
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
