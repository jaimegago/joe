# Test Fixtures

This directory contains test data and fixtures for integration and E2E tests.

## Directory Structure

```
fixtures/
├── configs/              # Test configuration files
│   ├── minimal.yaml
│   ├── full.yaml
│   └── invalid.yaml
├── conversations/        # Sample conversation data
│   ├── simple.json
│   ├── multi_turn.json
│   └── with_tools.json
├── llm_responses/        # Canned LLM responses for mocking
│   ├── claude_responses.json
│   └── gemini_responses.json
└── test_repos/          # Sample git repositories
    └── sample-app/
```

## Usage

Load fixtures in tests:

```go
func loadFixture(t *testing.T, path string) []byte {
    data, err := os.ReadFile(filepath.Join("../fixtures", path))
    if err != nil {
        t.Fatalf("failed to load fixture %s: %v", path, err)
    }
    return data
}
```

## Fixtures

### Configurations

- `minimal.yaml`: Bare minimum valid config
- `full.yaml`: All options specified
- `invalid.yaml`: Various invalid configurations for error testing

### Conversations

- `simple.json`: Single question-answer pair
- `multi_turn.json`: Multi-turn conversation with context
- `with_tools.json`: Conversation including tool calls

### LLM Responses

Structured responses for mocking LLM providers:

```json
{
  "scenarios": {
    "greeting": {
      "input": "hello",
      "response": {
        "content": "Hi! How can I help?",
        "model": "test-model"
      }
    }
  }
}
```
