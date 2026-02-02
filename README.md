# Joe - AI-Powered Infrastructure Copilot

Joe (Joe Operates Everything) helps platform engineers understand, debug, and operate their infrastructure through natural conversation.

## Status

🚧 **Early Development** - Phase 1 in progress

Currently implemented:

- ✅ Project scaffolding and architecture
- ✅ LLM adapter interface (AI-agnostic design)
- ✅ Claude adapter with tool support
- ✅ Gemini adapter with tool support
- ✅ Tool execution framework
- ⏳ SQL store (SQLite)
- ⏳ Graph store (Cayley)
- ⏳ Agentic loop
- ⏳ REPL / interactive mode

## Quick Start

### Prerequisites

- Go 1.23 or later
- API key for your chosen LLM provider:
  - Anthropic API key (for Claude)
  - Google API key (for Gemini)

### Installation

```bash
git clone https://github.com/jaimegago/joe.git
cd joe
go build ./cmd/joe
```

### Configuration

Set your API key for your chosen provider:

```bash
# For Claude
export ANTHROPIC_API_KEY="your-anthropic-key"

# For Gemini
export GEMINI_API_KEY="your-google-key"
# or
export GOOGLE_API_KEY="your-google-key"
```

For other configuration options, create `~/.joe/config.yaml`:

```yaml
# LLM Configuration
llm:
  provider: claude              # claude | gemini | openai | ollama
  model: claude-sonnet-4-20250514  # or gemini-2.0-flash-exp

# Background Refresh
refresh:
  interval: 5m

# Logging
logging:
  level: info                   # debug | info | warn | error
  file: ~/.joe/joe.log
```

### Run

```bash
./joe
```

## Architecture

Joe is designed to be:
- **AI-agnostic** - Swappable LLM backends (Claude, OpenAI, Ollama)
- **Single binary** - MVP runs as one process (daemon mode planned for later)
- **Graph-based** - Builds a knowledge graph of your infrastructure
- **Agentic** - LLM reasons → calls tools → processes results → continues

See [docs/joe-architecture.md](docs/joe-architecture.md) for details.

## Development

Build and test:

```bash
# Build all packages
go build ./...

# Run tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Verify code
go vet ./...
gofmt -s -w .
```

## Project Structure

```text
joe/
├── cmd/joe/                  # CLI entry point
├── internal/
│   ├── joe/                  # Core orchestration
│   ├── llm/                  # LLM adapters (claude, openai, ollama)
│   ├── tools/                # Tool execution framework
│   ├── agent/                # Agentic loop
│   ├── graph/                # Graph store (Cayley)
│   ├── store/                # SQL store (SQLite)
│   └── ...
└── docs/                     # Architecture documentation
```

## Documentation

- [CLAUDE.md](CLAUDE.md) - Project context for AI assistants
- [docs/joe-architecture.md](docs/joe-architecture.md) - Full architecture
- [docs/joe-dataflow.md](docs/joe-dataflow.md) - Data flow details
- [docs/go-standards.md](docs/go-standards.md) - Go coding standards

## License

TBD
