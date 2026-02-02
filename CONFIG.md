# Joe Configuration

Joe can be configured via a YAML configuration file and environment variables.

## Quick Start

1. **Create config file** (optional - Joe uses sensible defaults):
```bash
mkdir -p ~/.joe
cp config.example.yaml ~/.joe/config.yaml
```

2. **Edit config** to set your preferences:
```bash
vi ~/.joe/config.yaml
```

3. **Set API key** (required):
```bash
# For Claude
export ANTHROPIC_API_KEY=your-key-here

# For Gemini
export GEMINI_API_KEY=your-key-here
```

4. **Run Joe**:
```bash
./joe
# Or with custom config path:
./joe -config /path/to/config.yaml
```

## Configuration File

**Default location:** `~/.joe/config.yaml`

**Specify custom path:**
```bash
joe -config /path/to/my-config.yaml
```

### Example Configuration

See [config.example.yaml](config.example.yaml) for a complete example.

```yaml
llm:
  provider: claude
  model: claude-sonnet-4-20250514

refresh:
  interval_minutes: 5
  llm_budget:
    max_calls_per_hour: 100
    batch_threshold: 10
    batch_timeout_sec: 30

notifications:
  desktop:
    enabled: false
    priority_threshold: medium
  slack:
    enabled: false
    priority_threshold: high
  quiet_hours:
    enabled: false
    start: "22:00"
    end: "08:00"
    timezone: Local

logging:
  level: info
  file: ""
```

## Configuration Options

### LLM Settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `llm.provider` | string | `claude` | LLM provider (`claude` or `gemini`) |
| `llm.model` | string | `claude-sonnet-4-20250514` | Model identifier |

**Supported providers:**
- `claude` - Anthropic Claude (requires `ANTHROPIC_API_KEY`)
- `gemini` - Google Gemini (requires `GEMINI_API_KEY` or `GOOGLE_API_KEY`)

### Refresh Settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `refresh.interval_minutes` | int | `5` | Background refresh interval in minutes |
| `refresh.llm_budget.max_calls_per_hour` | int | `100` | Max LLM calls per hour during refresh |
| `refresh.llm_budget.batch_threshold` | int | `10` | Batch threshold for LLM calls |
| `refresh.llm_budget.batch_timeout_sec` | int | `30` | Batch timeout in seconds |

### Logging Settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `logging.level` | string | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `logging.file` | string | `""` | Log file path (empty = stdout) |

### Notification Settings

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `notifications.desktop.enabled` | bool | `false` | Enable desktop notifications |
| `notifications.slack.enabled` | bool | `false` | Enable Slack notifications |

## Environment Variables

Environment variables **override** config file settings:

| Variable | Purpose | Example |
|----------|---------|---------|
| `JOE_LLM_PROVIDER` | Override LLM provider | `export JOE_LLM_PROVIDER=gemini` |
| `JOE_LLM_MODEL` | Override model | `export JOE_LLM_MODEL=gemini-2.0-flash-exp` |
| `ANTHROPIC_API_KEY` | Claude API key | `export ANTHROPIC_API_KEY=sk-...` |
| `GEMINI_API_KEY` | Gemini API key | `export GEMINI_API_KEY=...` |
| `GOOGLE_API_KEY` | Alternative Gemini key | `export GOOGLE_API_KEY=...` |

## Configuration Priority

Settings are applied in this order (later overrides earlier):

1. **Default values** (hardcoded)
2. **Config file** (`~/.joe/config.yaml` or `-config` path)
3. **Environment variables** (e.g., `JOE_LLM_PROVIDER`)

## Security Notes

- **API keys are NEVER stored in config files** - always use environment variables
- Config file permissions: `0644` (readable by owner and group)
- Config directory permissions: `0755`

## Examples

### Using Claude

**Config file** (`~/.joe/config.yaml`):
```yaml
llm:
  provider: claude
  model: claude-sonnet-4-20250514
```

**Run:**
```bash
export ANTHROPIC_API_KEY=your-key
./joe
```

### Using Gemini

**Config file** (`~/.joe/config.yaml`):
```yaml
llm:
  provider: gemini
  model: gemini-1.5-flash  # Fast and stable (recommended)
  # model: gemini-1.5-pro  # More capable
```

**Run:**
```bash
export GEMINI_API_KEY=your-key
./joe
```

### Override with Environment Variables

Even with Claude in config, use Gemini:
```bash
export JOE_LLM_PROVIDER=gemini
export JOE_LLM_MODEL=gemini-1.5-flash
export GEMINI_API_KEY=your-key
./joe
```

### Custom Config Location

```bash
./joe -config /etc/joe/production.yaml
```

### Minimal Config

Joe works without a config file - just set the API key:
```bash
export ANTHROPIC_API_KEY=your-key
./joe
```

## Troubleshooting

**Problem:** "Failed to load config"
- Check file path and permissions
- Ensure YAML is valid (use a YAML validator)
- Check for tabs vs spaces (YAML requires spaces)

**Problem:** "ANTHROPIC_API_KEY is not set"
- Set the appropriate API key for your configured provider
- Check that environment variable is exported: `echo $ANTHROPIC_API_KEY`

**Problem:** Config file not found
- This is OK! Joe uses defaults if no config file exists
- Create `~/.joe/config.yaml` to customize settings

**Problem:** Invalid YAML syntax
- YAML is indentation-sensitive - use spaces, not tabs
- Strings with special characters should be quoted
- Use a YAML validator to check syntax
